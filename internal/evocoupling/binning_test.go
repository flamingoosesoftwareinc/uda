package evocoupling_test

import (
	"testing"
	"time"

	"github.com/flamingoosesoftwareinc/uda/internal/evocoupling"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPresenceMatrix(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	binWidth := 14 * day

	cases := map[string]struct {
		commits     []evocoupling.TimedPackageSet
		windowStart time.Time
		binWidth    time.Duration
		wantBins    int
		wantPkgs    []string
		// wantPresent[binIdx][pkgName] = expected flag. Only checked entries
		// are asserted; missing entries default to false.
		wantPresent map[int]map[string]bool
	}{
		"empty": {
			commits:     nil,
			windowStart: t0,
			binWidth:    binWidth,
			wantBins:    0,
			wantPkgs:    nil,
		},
		"single_commit_single_bin": {
			commits: []evocoupling.TimedPackageSet{
				{Time: t0.Add(2 * day), Packages: map[string]struct{}{"alpha": {}}},
			},
			windowStart: t0,
			binWidth:    binWidth,
			wantBins:    1,
			wantPkgs:    []string{"alpha"},
			wantPresent: map[int]map[string]bool{
				0: {"alpha": true},
			},
		},
		"multi_bin_spread": {
			commits: []evocoupling.TimedPackageSet{
				{Time: t0, Packages: map[string]struct{}{"alpha": {}}},
				{Time: t0.Add(20 * day), Packages: map[string]struct{}{"beta": {}}},
				{Time: t0.Add(35 * day), Packages: map[string]struct{}{"alpha": {}, "beta": {}}},
			},
			windowStart: t0,
			binWidth:    binWidth,
			wantBins:    3, // bin 0: [0,14), bin 1: [14,28), bin 2: [28,42)
			wantPkgs:    []string{"alpha", "beta"},
			wantPresent: map[int]map[string]bool{
				0: {"alpha": true, "beta": false},
				1: {"alpha": false, "beta": true},
				2: {"alpha": true, "beta": true},
			},
		},
		"duplicate_commits_within_bin_collapse": {
			commits: []evocoupling.TimedPackageSet{
				{Time: t0.Add(1 * day), Packages: map[string]struct{}{"alpha": {}}},
				{Time: t0.Add(5 * day), Packages: map[string]struct{}{"alpha": {}}},
				{Time: t0.Add(10 * day), Packages: map[string]struct{}{"alpha": {}}},
			},
			windowStart: t0,
			binWidth:    binWidth,
			wantBins:    1,
			wantPkgs:    []string{"alpha"},
			wantPresent: map[int]map[string]bool{
				0: {"alpha": true},
			},
		},
		"package_at_window_edges": {
			commits: []evocoupling.TimedPackageSet{
				{Time: t0, Packages: map[string]struct{}{"alpha": {}}},
				{Time: t0.Add(28 * day), Packages: map[string]struct{}{"alpha": {}}},
			},
			windowStart: t0,
			binWidth:    binWidth,
			wantBins:    3,
			wantPkgs:    []string{"alpha"},
			wantPresent: map[int]map[string]bool{
				0: {"alpha": true},
				1: {"alpha": false},
				2: {"alpha": true},
			},
		},
		"commit_before_window_start_dropped": {
			commits: []evocoupling.TimedPackageSet{
				{Time: t0.Add(-10 * day), Packages: map[string]struct{}{"alpha": {}}},
				{Time: t0.Add(5 * day), Packages: map[string]struct{}{"beta": {}}},
			},
			windowStart: t0,
			binWidth:    binWidth,
			wantBins:    1,
			wantPkgs: []string{
				"alpha",
				"beta",
			}, // alpha listed (from commit), but never present in window
			wantPresent: map[int]map[string]bool{
				0: {"alpha": false, "beta": true},
			},
		},
		"zero_bin_width_returns_empty": {
			commits: []evocoupling.TimedPackageSet{
				{Time: t0, Packages: map[string]struct{}{"alpha": {}}},
			},
			windowStart: t0,
			binWidth:    0,
			wantBins:    0,
			wantPkgs:    nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			matrix := evocoupling.BuildPresenceMatrix(tc.commits, tc.windowStart, tc.binWidth)

			require.NotNil(t, matrix)
			assert.Len(t, matrix.Bins, tc.wantBins, "bin count")
			assert.Equal(t, tc.wantPkgs, matrix.Packages, "package list")

			if tc.wantBins == 0 {
				assert.Empty(t, matrix.Present)

				return
			}

			require.Len(t, matrix.Present, tc.wantBins)

			pkgIdx := make(map[string]int, len(matrix.Packages))
			for i, pkg := range matrix.Packages {
				pkgIdx[pkg] = i
			}

			for binIdx, byPkg := range tc.wantPresent {
				for pkg, want := range byPkg {
					idx, ok := pkgIdx[pkg]
					require.True(t, ok, "expected package %s in matrix", pkg)
					assert.Equal(t, want, matrix.Present[binIdx][idx],
						"bin %d package %s", binIdx, pkg)
				}
			}
		})
	}
}

func TestBuildPresenceMatrix_BinStartTimes(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	binWidth := 7 * day

	commits := []evocoupling.TimedPackageSet{
		{Time: t0, Packages: map[string]struct{}{"alpha": {}}},
		{Time: t0.Add(20 * day), Packages: map[string]struct{}{"alpha": {}}},
	}

	matrix := evocoupling.BuildPresenceMatrix(commits, t0, binWidth)

	require.Len(t, matrix.Bins, 3)
	assert.True(t, matrix.Bins[0].Equal(t0))
	assert.True(t, matrix.Bins[1].Equal(t0.Add(7*day)))
	assert.True(t, matrix.Bins[2].Equal(t0.Add(14*day)))
}
