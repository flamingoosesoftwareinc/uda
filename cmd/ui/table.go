package ui

import (
	"fmt"
	"regexp"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/hotspot"
)

func buildTableWithHotspots(
	metrics []analyzer.Metrics,
	width, height int,
	criteria []SortCriterion,
	hotspots *HotspotData,
) table.Model {
	var scores map[string]hotspot.PackageScore
	if hotspots != nil {
		scores = hotspots.Scores
	}

	sorted := SortMetrics(metrics, BuildSortFuncs(criteria, scores)...)

	// Extra columns width: CHNG FREQ (11) + HOTSPOT (10) = 21
	pkgWidth := width - fixedColumnsWidth
	if hotspots != nil {
		pkgWidth = width - fixedColumnsWidth - hotspotColumnsWidth
	}

	if pkgWidth < minPackageWidth {
		pkgWidth = minPackageWidth
	}

	s := table.DefaultStyles()
	s.Header = s.Header.
		Bold(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(lipgloss.Color(colorBorder))
	s.Selected = s.Selected.
		Reverse(true).
		Bold(true)

	return table.New(
		table.WithColumns(metricColumns(criteria, hotspots, pkgWidth)),
		table.WithRows(metricRows(sorted, hotspots, scores)),
		table.WithStyles(s),
		table.WithWidth(width),
		table.WithHeight(height),
		table.WithFocused(true),
	)
}

// metricColumns builds the table columns, adding hotspot columns when present.
func metricColumns(criteria []SortCriterion, hotspots *HotspotData, pkgWidth int) []table.Column {
	cols := []table.Column{
		{Title: "PACKAGE" + SortIndicator(criteria, SortPackage), Width: pkgWidth},
		{Title: "INWARD" + SortIndicator(criteria, SortInward), Width: inwardColWidth},
		{Title: "OUTWARD" + SortIndicator(criteria, SortOutward), Width: outwardColWidth},
		{
			Title: "INSTABILITY" + SortIndicator(criteria, SortInstability),
			Width: instabilityColWidth,
		},
	}
	if hotspots == nil {
		return cols
	}

	return append(cols,
		table.Column{
			Title: "CHNG FREQ" + SortIndicator(criteria, SortChangeFreq),
			Width: changeFreqColWidth,
		},
		table.Column{
			Title: "HOTSPOT" + SortIndicator(criteria, SortHotspot),
			Width: hotspotColWidth,
		},
	)
}

// metricRows builds the table rows, appending hotspot columns when present.
func metricRows(
	sorted []analyzer.Metrics,
	hotspots *HotspotData,
	scores map[string]hotspot.PackageScore,
) []table.Row {
	rows := make([]table.Row, 0, len(sorted))
	for _, metric := range sorted {
		row := table.Row{
			string(metric.Package),
			fmt.Sprintf("%.0f", metric.InwardCoupling()),
			fmt.Sprintf("%.0f", metric.OutwardCoupling()),
			fmt.Sprintf("%.3f", metric.Instability()),
		}
		if hotspots != nil {
			if score, ok := scores[string(metric.Package)]; ok {
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

	return rows
}

// FilterMetrics returns the subset of metrics whose package matches the regex.
// A nil regex passes the slice through unchanged.
func FilterMetrics(metrics []analyzer.Metrics, pattern *regexp.Regexp) []analyzer.Metrics {
	if pattern == nil {
		return metrics
	}

	filtered := make([]analyzer.Metrics, 0, len(metrics))
	for _, metric := range metrics {
		if pattern.MatchString(string(metric.Package)) {
			filtered = append(filtered, metric)
		}
	}

	return filtered
}

func (m metricsModel) rebuildTables() metricsModel {
	tableH := m.tableHeight()
	for i, g := range m.groups {
		filtered := FilterMetrics(g.Metrics, m.filterRegex)
		m.tables[i] = buildTableWithHotspots(filtered, m.width, tableH, m.sortCriteria, g.Hotspots)
	}

	return m
}

func (m metricsModel) resizeAll() metricsModel {
	tableH := m.tableHeight()
	for i := range m.tables {
		m.tables[i].SetWidth(m.width)
		m.tables[i].SetHeight(tableH)
		// Recalculate package column width
		cols := m.tables[i].Columns()
		if len(cols) == baseColumnCount {
			cols[0].Width = m.width - fixedColumnsWidth
			m.tables[i].SetColumns(cols)
		} else if len(cols) == hotspotColumnCount {
			cols[0].Width = m.width - fixedColumnsWidth - hotspotColumnsWidth
			m.tables[i].SetColumns(cols)
		}
	}

	if m.drillDown != nil {
		m.drillDown.viewport.SetWidth(m.width)
		m.drillDown.viewport.SetHeight(tableH)
	}

	return m
}

// tableHeight returns the available height for the table/viewport
// (total height minus tab bar and help bar).
func (m metricsModel) tableHeight() int {
	// 1 for tab bar + 1 newline after table + 1 for help bar
	h := max(m.height-tableChromeHeight, 1)

	return h
}

func isSortKey(key string) bool {
	switch key {
	case "p", "i", "o", "s", "c", "h":
		return true
	}

	return false
}

func sortKeyToField(key string) SortField {
	switch key {
	case "p":
		return SortPackage
	case "i":
		return SortInward
	case "o":
		return SortOutward
	case "s":
		return SortInstability
	case "c":
		return SortChangeFreq
	case "h":
		return SortHotspot
	}

	return SortInstability
}
