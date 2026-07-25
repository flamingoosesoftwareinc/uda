package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/flamingoosesoftwareinc/uda/internal/evocoupling"
)

// Sort key strings for --sort. Stable identifiers — appear on the CLI
// surface and are matched as-is.
const (
	SortByCorrelation = "correlation"
	SortByDivergence  = "divergence"
	SortByNMI         = "nmi"
)

// SigmaMode distinguishes user-supplied sigma from picker-derived sigma in
// JSON output. Stable strings — downstream consumers may branch on them.
const (
	SigmaModeManual = "manual"
	SigmaModeAuto   = "auto"
)

// SigmaSelection describes how the kernel bandwidth was chosen. Populated
// for both manual (--sigma) and auto modes — the auto-derivation fields are
// always present so a reviewer reading the JSON can see what the picker
// would have chosen even when overridden.
type SigmaSelection struct {
	Mode              string  `json:"mode"`
	SigmaSeconds      float64 `json:"sigma_seconds"`
	SigmaHuman        string  `json:"sigma_human"`
	TargetStddev      float64 `json:"target_stddev"`
	RequiredESS       float64 `json:"required_ess"`
	AchievedESSMedian float64 `json:"achieved_ess_median"`
	AchievedESSP25    float64 `json:"achieved_ess_p25"`
	LowConfidence     bool    `json:"low_confidence"`
	Reason            string  `json:"reason,omitempty"`
	DensityCapSeconds float64 `json:"density_cap_seconds,omitempty"`
	DensityCapHuman   string  `json:"density_cap_human,omitempty"`
}

// MIBiasCorrection labels the bias-correction strategy used for the NMI
// metric. Stable string — downstream consumers branch on it. v1 ships
// "miller-madow" only.
const MIBiasCorrection = "miller-madow"

// MISelection describes the MI pass parameters. Populated only when MI is
// enabled (omitempty drops the block from the JSON for the v1 default-off
// path). Mirrors the sigma_selection shape so reviewers reading the JSON
// see one consistent metadata surface.
type MISelection struct {
	Enabled         bool    `json:"enabled"`
	BinWidthSeconds float64 `json:"bin_width_seconds"`
	NBins           int     `json:"n_bins"`
	MinSupport      int     `json:"min_support"`
	BiasCorrection  string  `json:"bias_correction"`
	// LowConfidence is true when the bin count is too small for MI to be
	// meaningful — design doc open question 4, floor at 8 bins.
	LowConfidence bool `json:"low_confidence,omitempty"`
}

// CouplingReport is the top-level coupling output surface: sigma metadata
// plus the ranked pairs.
type CouplingReport struct {
	SigmaSelection SigmaSelection             `json:"sigma_selection"`
	MISelection    *MISelection               `json:"mi_selection,omitempty"`
	Pairs          []evocoupling.CouplingPair `json:"pairs"`
}

// SortCouplingPairs returns pairs re-sorted by the requested key. NMI /
// divergence sort puts nil-valued pairs (NaN-suppressed, MI-not-computed)
// at the bottom — they carry no signal for the requested axis. Unknown
// sort keys are treated as correlation (Analyze's default order, already
// applied upstream).
func SortCouplingPairs(pairs []evocoupling.CouplingPair, key string) []evocoupling.CouplingPair {
	switch key {
	case SortByDivergence:
		sort.SliceStable(pairs, func(i, j int) bool {
			return greaterByMetric(
				pairs[i].Divergence,
				pairs[j].Divergence,
				pairs[i].PackageA,
				pairs[j].PackageA,
			)
		})
	case SortByNMI:
		sort.SliceStable(pairs, func(i, j int) bool {
			return greaterByMetric(pairs[i].NMI, pairs[j].NMI, pairs[i].PackageA, pairs[j].PackageA)
		})
	}

	return pairs
}

// greaterByMetric ranks finite values above nil and falls back to
// package-name lex order for stable output when both sides are nil or
// equal. Returns true when i should sort before j.
func greaterByMetric(left, right *float64, leftName, rightName string) bool {
	switch {
	case left != nil && right == nil:
		return true
	case left == nil && right != nil:
		return false
	case left == nil && right == nil:
		return leftName < rightName
	}

	if *left != *right {
		return *left > *right
	}

	return leftName < rightName
}

// FilterCouplingPairs returns pairs where at least one package matches pattern.
func FilterCouplingPairs(
	pairs []evocoupling.CouplingPair,
	pattern *regexp.Regexp,
) []evocoupling.CouplingPair {
	if pattern == nil {
		return pairs
	}

	filtered := make([]evocoupling.CouplingPair, 0, len(pairs))
	for _, p := range pairs {
		if pattern.MatchString(p.PackageA) || pattern.MatchString(p.PackageB) {
			filtered = append(filtered, p)
		}
	}

	return filtered
}

// CouplingTable renders the coupling report as a single-line sigma header
// followed by a lipgloss table of pairs.
func CouplingTable(report CouplingReport, filter *regexp.Regexp) string {
	pairs := FilterCouplingPairs(report.Pairs, filter)
	miEnabled := report.MISelection != nil && report.MISelection.Enabled

	headers, rows := couplingTableRows(pairs, miEnabled)

	lightDark := lipgloss.LightDark(lipgloss.HasDarkBackground(os.Stdin, os.Stdout))
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lightDark(lipgloss.Color("0"), lipgloss.Color("15")))
	headerStyle := lipgloss.NewStyle().Bold(true).Align(lipgloss.Center)
	cellStyle := lipgloss.NewStyle().Padding(0, 1)

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle.Padding(0, 1)
			}

			return cellStyle
		}).
		Headers(headers...).
		Rows(rows...)

	out := titleStyle.Render("Evolutionary Coupling") + "\n" +
		SigmaHeaderLine(report.SigmaSelection) + "\n"
	if miEnabled {
		out += MIHeaderLine(*report.MISelection) + "\n"
	}

	return out + t.Render()
}

// missingMetric is the placeholder rendered in the text format when a
// metric is nil — either because MI was suppressed (NaN) or because the
// pair predates the MI pass. Single character keeps the column tight.
const missingMetric = "—"

func couplingTableRows(pairs []evocoupling.CouplingPair, miEnabled bool) ([]string, [][]string) {
	// Truncate Correlation precision to 3 decimals when MI columns join
	// it; keeps the row under the 100-char target. 4-decimal Correlation
	// stays for the default (MI-off) path so existing tooling is
	// byte-stable.
	if !miEnabled {
		headers := []string{"PACKAGE A", "PACKAGE B", "CORRELATION"}
		rows := make([][]string, 0, len(pairs))

		for _, p := range pairs {
			rows = append(rows, []string{
				p.PackageA,
				p.PackageB,
				fmt.Sprintf("%.4f", p.Correlation),
			})
		}

		return headers, rows
	}

	headers := []string{"PACKAGE A", "PACKAGE B", "CORR", "NMI", "DIVERG", "SUPPORT"}
	rows := make([][]string, 0, len(pairs))

	for _, p := range pairs {
		rows = append(rows, []string{
			p.PackageA,
			p.PackageB,
			fmt.Sprintf("%.3f", p.Correlation),
			renderMetric(p.NMI),
			renderMetric(p.Divergence),
			renderSupport(p.LowSupport, p.NMI),
		})
	}

	return headers, rows
}

func renderMetric(v *float64) string {
	if v == nil {
		return missingMetric
	}

	return fmt.Sprintf("%.3f", *v)
}

func renderSupport(lowSupport bool, nmi *float64) string {
	if nmi == nil {
		return missingMetric
	}

	if lowSupport {
		return "low"
	}

	return "ok"
}

// MIHeaderLine renders a one-line MI summary for the text format header.
// Example: "MI: 12 bins of 14d, min support 3 (miller-madow)".
func MIHeaderLine(sel MISelection) string {
	binDays := sel.BinWidthSeconds / float64(secondsPerDay)
	out := fmt.Sprintf("MI: %d bins of %.0fd, min support %d (%s)",
		sel.NBins, binDays, sel.MinSupport, sel.BiasCorrection)

	if sel.LowConfidence {
		out += ", low confidence"
	}

	return out
}

// secondsPerDay is the divisor used to render bin width as days in the MI
// header line. Pulled out to a named constant so the conversion is
// explicit at the call site.
const secondsPerDay = 24 * 60 * 60

// SigmaHeaderLine renders a one-line sigma summary for the text/table
// format header. Examples:
//
//	Sigma: 14d 0h (auto, target ±0.05, ESS=412)
//	Sigma: 14d (manual)
//	Sigma: 30d (auto, target ±0.05, ESS=87, low confidence: insufficient_commits)
func SigmaHeaderLine(sel SigmaSelection) string {
	if sel.Mode == SigmaModeManual {
		return fmt.Sprintf("Sigma: %s (manual)", sel.SigmaHuman)
	}

	suffix := fmt.Sprintf("auto, target ±%g, ESS=%.0f",
		sel.TargetStddev, sel.AchievedESSMedian)
	if sel.LowConfidence {
		suffix += ", low confidence: " + sel.Reason
	}

	return fmt.Sprintf("Sigma: %s (%s)", sel.SigmaHuman, suffix)
}

// CouplingJSON returns the coupling report as indented JSON.
func CouplingJSON(report CouplingReport, filter *regexp.Regexp) (string, error) {
	report.Pairs = FilterCouplingPairs(report.Pairs, filter)

	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}

	return string(b), nil
}
