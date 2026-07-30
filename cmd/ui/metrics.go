package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/hotspot"
)

// LanguageMetrics pairs a language name with its metrics.
type LanguageMetrics struct {
	Language string             `json:"language"`
	Metrics  []analyzer.Metrics `json:"metrics"`
	Hotspots *HotspotData       `json:"hotspots,omitempty"`
}

// HotspotData carries hotspot analysis results for a language group.
type HotspotData struct {
	Scores  map[string]hotspot.PackageScore `json:"scores"`
	Commits map[string][]CommitTouchInfo    `json:"commits,omitempty"`
}

// CommitTouchInfo holds info about a commit that touched a package.
type CommitTouchInfo struct {
	SHA       string           `json:"sha"`
	Message   string           `json:"message"`
	Timestamp time.Time        `json:"timestamp"`
	Files     []FileChangeStat `json:"files"`
}

// FileChangeStat holds change stats for a single file.
type FileChangeStat struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// MetricsSummary is the simplified per-package metrics record (re-export from analyzer).
type MetricsSummary = analyzer.MetricsSummary

// LanguageMetricsSummary pairs a language with its MetricsSummary records (re-export from analyzer).
type LanguageMetricsSummary = analyzer.LanguageMetricsSummary

// HotspotMetricsSummary extends MetricsSummary with hotspot fields.
// Returned alongside the table format (package, inward count, outward count,
// instability) when hotspot analysis is enabled.
type HotspotMetricsSummary struct {
	Package     string  `json:"package"`
	Inward      int     `json:"inward"`
	Outward     int     `json:"outward"`
	Instability float64 `json:"instability"`
	ChangeFreq  float64 `json:"change_freq,omitempty"`
	Hotspot     float64 `json:"hotspot,omitempty"`
}

// HotspotLanguageMetricsSummary pairs a language with its HotspotMetricsSummary records.
type HotspotLanguageMetricsSummary struct {
	Language string                  `json:"language"`
	Metrics  []HotspotMetricsSummary `json:"metrics"`
}

// MetricsJSON renders the per-language metrics groups as indented JSON in the
// simplified table-friendly schema (package + inward/outward counts + instability).
func MetricsJSON(
	groups []LanguageMetrics,
	criteria []SortCriterion,
	filter *regexp.Regexp,
) (string, error) {
	// Check if any group has hotspot data
	hasHotspots := false

	for _, g := range groups {
		if g.Hotspots != nil {
			hasHotspots = true

			break
		}
	}

	if hasHotspots {
		summaries := make([]HotspotLanguageMetricsSummary, 0, len(groups))
		for _, g := range groups {
			filtered := FilterMetrics(g.Metrics, filter)
			sorted := SortMetrics(filtered, BuildSortFuncs(criteria, nil)...)

			metricsSummary := make([]HotspotMetricsSummary, 0, len(sorted))
			for _, metric := range sorted {
				s := HotspotMetricsSummary{
					Package:     string(metric.Package),
					Inward:      int(metric.InwardCoupling()),
					Outward:     int(metric.OutwardCoupling()),
					Instability: metric.Instability(),
				}
				if g.Hotspots != nil {
					if score, ok := g.Hotspots.Scores[string(metric.Package)]; ok {
						s.ChangeFreq = score.ChangeFreq
						s.Hotspot = score.HotspotScore
					}
				}

				metricsSummary = append(metricsSummary, s)
			}

			summaries = append(summaries, HotspotLanguageMetricsSummary{
				Language: g.Language,
				Metrics:  metricsSummary,
			})
		}

		b, err := json.MarshalIndent(summaries, "", "  ")
		if err != nil {
			return "", err
		}

		return string(b), nil
	}

	summaries := make([]LanguageMetricsSummary, 0, len(groups))
	for _, g := range groups {
		filtered := FilterMetrics(g.Metrics, filter)
		sorted := SortMetrics(filtered, BuildSortFuncs(criteria, nil)...)

		metricsSummary := make([]MetricsSummary, 0, len(sorted))
		for _, m := range sorted {
			metricsSummary = append(metricsSummary, MetricsSummary{
				Package:     string(m.Package),
				Inward:      int(m.InwardCoupling()),
				Outward:     int(m.OutwardCoupling()),
				Instability: m.Instability(),
			})
		}

		summaries = append(summaries, LanguageMetricsSummary{
			Language: g.Language,
			Metrics:  metricsSummary,
		})
	}

	b, err := json.MarshalIndent(summaries, "", "  ")
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// MetricsJSONExtended returns the full metrics data including detailed
// coupling information (which packages, what symbols, usage counts, and positions).
func MetricsJSONExtended(groups []LanguageMetrics, filter *regexp.Regexp) (string, error) {
	filtered := applyFilter(groups, filter)

	b, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// MetricsTable returns the metrics as styled terminal tables,
// one per language, sorted by the given criteria.
func MetricsTable(
	groups []LanguageMetrics,
	criteria []SortCriterion,
	filter *regexp.Regexp,
) string {
	sections := make([]string, 0, len(groups))

	for _, g := range groups {
		metrics := FilterMetrics(g.Metrics, filter)
		sections = append(
			sections,
			renderLanguageTableWithHotspots(g.Language, metrics, criteria, nil),
		)
	}

	return strings.Join(sections, "\n\n")
}

func applyFilter(groups []LanguageMetrics, filter *regexp.Regexp) []LanguageMetrics {
	if filter == nil {
		return groups
	}

	filtered := make([]LanguageMetrics, len(groups))
	for i, g := range groups {
		filtered[i] = LanguageMetrics{
			Language: g.Language,
			Metrics:  FilterMetrics(g.Metrics, filter),
		}
	}

	return filtered
}

func renderLanguageTableWithHotspots(
	language string,
	metrics []analyzer.Metrics,
	criteria []SortCriterion,
	hotspots *HotspotData,
) string {
	var scores map[string]hotspot.PackageScore
	if hotspots != nil {
		scores = hotspots.Scores
	}

	sorted := SortMetrics(metrics, BuildSortFuncs(criteria, scores)...)

	rows := make([][]string, 0, len(sorted))
	for _, metric := range sorted {
		row := []string{
			string(metric.Package),
			fmt.Sprintf("%.0f", metric.InwardCoupling()),
			fmt.Sprintf("%.0f", metric.OutwardCoupling()),
			fmt.Sprintf("%.3f", metric.Instability()),
		}
		if hotspots != nil {
			if score, ok := hotspots.Scores[string(metric.Package)]; ok {
				row = append(row,
					fmt.Sprintf("%.1f%%", score.ChangeFreq*percentMultiplier),
					fmt.Sprintf("%.3f", score.HotspotScore),
				)
			} else {
				row = append(row, "0.0%", "0.000")
			}
		}

		rows = append(rows, row)
	}

	lightDark := lipgloss.LightDark(lipgloss.HasDarkBackground(os.Stdin, os.Stdout))
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lightDark(lipgloss.Color("0"), lipgloss.Color("15")))
	headerStyle := lipgloss.NewStyle().Bold(true).Align(lipgloss.Center)
	cellStyle := lipgloss.NewStyle().Padding(0, 1)

	headers := []string{"PACKAGE", "INWARD", "OUTWARD", "INSTABILITY"}
	if hotspots != nil {
		headers = append(headers, "CHNG FREQ", "HOTSPOT")
	}

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

	return titleStyle.Render(language) + "\n" + t.Render()
}

// MetricsTableWithHotspots returns tables including hotspot columns.
func MetricsTableWithHotspots(
	groups []LanguageMetrics,
	criteria []SortCriterion,
	filter *regexp.Regexp,
) string {
	sections := make([]string, 0, len(groups))

	for _, g := range groups {
		metrics := FilterMetrics(g.Metrics, filter)
		sections = append(
			sections,
			renderLanguageTableWithHotspots(g.Language, metrics, criteria, g.Hotspots),
		)
	}

	return strings.Join(sections, "\n\n")
}
