package ui_test

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/flamingoosesoftwareinc/uda/cmd/ui"
	"github.com/flamingoosesoftwareinc/uda/internal/evocoupling"
	"github.com/stretchr/testify/require"
)

func testCouplingPairs() []evocoupling.CouplingPair {
	return []evocoupling.CouplingPair{
		{PackageA: "pkg/auth", PackageB: "pkg/user", Correlation: 0.85},
		{PackageA: "pkg/billing", PackageB: "pkg/invoice", Correlation: 0.72},
		{PackageA: "pkg/auth", PackageB: "pkg/billing", Correlation: 0.45},
		{PackageA: "pkg/store", PackageB: "pkg/inventory", Correlation: 0.30},
	}
}

func TestSigmaHeaderLine(t *testing.T) {
	cases := map[string]struct {
		sel  ui.SigmaSelection
		want string
	}{
		"manual_mode": {
			sel:  ui.SigmaSelection{Mode: ui.SigmaModeManual, SigmaHuman: "14d"},
			want: "Sigma: 14d (manual)",
		},
		"auto_mode_ok": {
			sel: ui.SigmaSelection{
				Mode:              ui.SigmaModeAuto,
				SigmaHuman:        "14d",
				TargetStddev:      0.05,
				AchievedESSMedian: 412,
			},
			want: "Sigma: 14d (auto, target ±0.05, ESS=412)",
		},
		"auto_mode_low_confidence": {
			sel: ui.SigmaSelection{
				Mode:              ui.SigmaModeAuto,
				SigmaHuman:        "30d",
				TargetStddev:      0.05,
				AchievedESSMedian: 87,
				LowConfidence:     true,
				Reason:            "insufficient_commits",
			},
			want: "Sigma: 30d (auto, target ±0.05, ESS=87, low confidence: insufficient_commits)",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, ui.SigmaHeaderLine(tc.sel))
		})
	}
}

func TestCouplingJSONRoundTrip(t *testing.T) {
	// The sigma_selection block is the new contract; verify it survives
	// a JSON marshal-unmarshal cycle with the field names the design doc
	// promises.
	report := ui.CouplingReport{
		SigmaSelection: ui.SigmaSelection{
			Mode:              ui.SigmaModeAuto,
			SigmaSeconds:      1209600,
			SigmaHuman:        "14d",
			TargetStddev:      0.05,
			RequiredESS:       400,
			AchievedESSMedian: 412,
			AchievedESSP25:    287,
		},
		Pairs: testCouplingPairs(),
	}

	out, err := ui.CouplingJSON(report, nil)
	require.NoError(t, err)
	require.Contains(t, out, `"sigma_selection"`)
	require.Contains(t, out, `"sigma_human": "14d"`)
	require.Contains(t, out, `"pairs"`)

	var decoded ui.CouplingReport
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
	require.Equal(t, "14d", decoded.SigmaSelection.SigmaHuman)
	require.InDelta(t, 0.05, decoded.SigmaSelection.TargetStddev, 1e-9)
	require.Len(t, decoded.Pairs, 4)
}

func TestCouplingTableContainsSigmaLine(t *testing.T) {
	report := ui.CouplingReport{
		SigmaSelection: ui.SigmaSelection{
			Mode:              ui.SigmaModeAuto,
			SigmaHuman:        "14d",
			TargetStddev:      0.05,
			AchievedESSMedian: 412,
		},
		Pairs: testCouplingPairs(),
	}

	out := ui.CouplingTable(report, nil)
	require.Contains(t, out, "Sigma: 14d",
		"table output must include sigma header line, got: %q", out)
}

func TestMIHeaderLine(t *testing.T) {
	const fourteenDaysSec = 14 * 24 * 60 * 60

	cases := map[string]struct {
		sel  ui.MISelection
		want string
	}{
		"normal": {
			sel: ui.MISelection{
				Enabled:         true,
				BinWidthSeconds: fourteenDaysSec,
				NBins:           12,
				MinSupport:      3,
				BiasCorrection:  ui.MIBiasCorrection,
			},
			want: "MI: 12 bins of 14d, min support 3 (miller-madow)",
		},
		"low_confidence": {
			sel: ui.MISelection{
				Enabled:         true,
				BinWidthSeconds: fourteenDaysSec,
				NBins:           4,
				MinSupport:      3,
				BiasCorrection:  ui.MIBiasCorrection,
				LowConfidence:   true,
			},
			want: "MI: 4 bins of 14d, min support 3 (miller-madow), low confidence",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, ui.MIHeaderLine(tc.sel))
		})
	}
}

func TestSortCouplingPairs_Divergence(t *testing.T) {
	mk := func(v float64) *float64 { return &v }

	pairs := []evocoupling.CouplingPair{
		{PackageA: "a", PackageB: "b", Divergence: mk(0.1), NMI: mk(0.5)},
		{PackageA: "c", PackageB: "d", Divergence: mk(0.7), NMI: mk(0.8)},
		{PackageA: "e", PackageB: "f", Divergence: nil, NMI: nil},
		{PackageA: "g", PackageB: "h", Divergence: mk(0.3), NMI: mk(0.4)},
	}

	sorted := ui.SortCouplingPairs(pairs, ui.SortByDivergence)

	require.Equal(t, "c", sorted[0].PackageA, "highest divergence first")
	require.Equal(t, "g", sorted[1].PackageA)
	require.Equal(t, "a", sorted[2].PackageA)
	require.Equal(t, "e", sorted[3].PackageA, "nil divergence sinks to bottom")
}

func TestSortCouplingPairs_UnknownKeyPreserves(t *testing.T) {
	pairs := []evocoupling.CouplingPair{
		{PackageA: "first", Correlation: 0.5},
		{PackageA: "second", Correlation: 0.3},
	}

	sorted := ui.SortCouplingPairs(pairs, "bogus")

	require.Equal(t, "first", sorted[0].PackageA)
	require.Equal(t, "second", sorted[1].PackageA)
}

func TestCouplingJSON_MIBlockOmittedWhenDisabled(t *testing.T) {
	report := ui.CouplingReport{
		SigmaSelection: ui.SigmaSelection{Mode: ui.SigmaModeAuto, SigmaHuman: "14d"},
		Pairs:          testCouplingPairs(),
	}

	out, err := ui.CouplingJSON(report, nil)
	require.NoError(t, err)
	require.NotContains(t, out, `"mi_selection"`,
		"mi_selection must be absent when MI disabled — byte-stable contract")
}

func TestCouplingJSON_MIBlockPresentWhenEnabled(t *testing.T) {
	const fourteenDaysSec = 14 * 24 * 60 * 60

	report := ui.CouplingReport{
		SigmaSelection: ui.SigmaSelection{Mode: ui.SigmaModeAuto, SigmaHuman: "14d"},
		MISelection: &ui.MISelection{
			Enabled:         true,
			BinWidthSeconds: fourteenDaysSec,
			NBins:           12,
			MinSupport:      3,
			BiasCorrection:  ui.MIBiasCorrection,
		},
		Pairs: testCouplingPairs(),
	}

	out, err := ui.CouplingJSON(report, nil)
	require.NoError(t, err)
	require.Contains(t, out, `"mi_selection"`)
	require.Contains(t, out, `"bias_correction": "miller-madow"`)
	require.Contains(t, out, `"min_support": 3`)
}

func TestFilterCouplingPairs(t *testing.T) {
	pairs := testCouplingPairs()

	tests := []struct {
		name    string
		filter  string
		wantLen int
		check   func(t *testing.T, got []evocoupling.CouplingPair)
	}{
		{
			name:    "nil filter returns all",
			wantLen: 4,
		},
		{
			name:    "matches package A only",
			filter:  "store",
			wantLen: 1,
			check: func(t *testing.T, got []evocoupling.CouplingPair) {
				require.Equal(t, "pkg/store", got[0].PackageA)
			},
		},
		{
			name:    "matches package B only",
			filter:  "invoice",
			wantLen: 1,
			check: func(t *testing.T, got []evocoupling.CouplingPair) {
				require.Equal(t, "pkg/invoice", got[0].PackageB)
			},
		},
		{
			name:    "matches both sides keeps pair once",
			filter:  "auth",
			wantLen: 2,
		},
		{
			name:    "matches neither",
			filter:  "nonexistent",
			wantLen: 0,
		},
		{
			name:    "regex pattern",
			filter:  "bill|invoice",
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var re *regexp.Regexp
			if tt.filter != "" {
				re = regexp.MustCompile(tt.filter)
			}

			got := ui.FilterCouplingPairs(pairs, re)
			require.Len(t, got, tt.wantLen)

			if tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}
