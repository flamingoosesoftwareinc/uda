// Package ui provides the interactive TUI for displaying metrics.
package ui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/hotspot"
)

// Drill-down marker glyphs: ▸ flags a collapsible row, leading-space variants
// are the indented "appended after symbol header" forms (the leading space is
// load-bearing — it visually attaches the chevron to the symbol).
const (
	chevronCollapsed       = "▸"
	chevronCollapsedSuffix = " ▸"
	chevronExpandedSuffix  = " ▾"
)

const (
	ellipsis        = "..."
	firstLineParts  = 2  // SplitN limit that keeps only the first line
	maxCommitMsgLen = 50 // commit-message summary is truncated past this width
)

type drillDownItem struct {
	// Display text for the item line
	header string
	// Non-nil for expandable symbol lines
	stat *analyzer.CouplingStat
	// Whether positions are currently shown
	expanded bool
	// Whether this is a section header (not selectable)
	isSection bool
	// Non-nil for position lines (openable in editor)
	pos *analyzer.Position
	// Non-nil for commit header lines (expandable)
	commitInfo *CommitTouchInfo
	// Non-nil for file change display lines (not selectable)
	fileChange *FileChangeStat
}

type drillDownState struct {
	pkg           analyzer.Metrics
	viewport      viewport.Model
	items         []drillDownItem
	cursor        int
	commitHistory []CommitTouchInfo
}

// headerLines is the number of lines rendered before the item list
// (title + blank + stats + blank).
const headerLines = 4

func (m metricsModel) enterDrillDown() metricsModel {
	if len(m.tables) == 0 {
		return m
	}

	t := m.tables[m.activeTab]

	row := t.SelectedRow()
	if row == nil {
		return m
	}

	g := m.groups[m.activeTab]
	filtered := FilterMetrics(g.Metrics, m.filterRegex)

	var scores map[string]hotspot.PackageScore
	if g.Hotspots != nil {
		scores = g.Hotspots.Scores
	}

	sorted := SortMetrics(filtered, BuildSortFuncs(m.sortCriteria, scores)...)

	cursor := t.Cursor()
	if cursor < 0 || cursor >= len(sorted) {
		return m
	}

	pkg := sorted[cursor]

	// Look up commit history from hotspot data.
	var commitHistory []CommitTouchInfo
	if g.Hotspots != nil && g.Hotspots.Commits != nil {
		commitHistory = g.Hotspots.Commits[string(pkg.Package)]
	}

	items := buildDrillDownItemsWithHistory(pkg, commitHistory)
	vp := viewport.New(viewport.WithWidth(m.width), viewport.WithHeight(m.tableHeight()))

	state := &drillDownState{
		pkg:           pkg,
		viewport:      vp,
		items:         items,
		cursor:        0,
		commitHistory: commitHistory,
	}
	// Find first selectable item
	for i, item := range items {
		if !item.isSection {
			state.cursor = i

			break
		}
	}

	state.viewport.SetContent(state.renderContent())

	m.drillDown = state

	return m
}

//nolint:funlen // sequential build: inward + outward + commit history sections.
func buildDrillDownItemsWithHistory(
	pkg analyzer.Metrics,
	commits []CommitTouchInfo,
) []drillDownItem {
	var items []drillDownItem

	// Inward section
	items = append(items, drillDownItem{header: "Inward Dependencies", isSection: true})
	if len(pkg.Inward) == 0 {
		items = append(items, drillDownItem{header: "  (none)", isSection: true})
	} else {
		for depPkg, stats := range sortedCouplingStats(pkg.Inward) {
			items = append(items, drillDownItem{header: "  " + string(depPkg), isSection: true})

			for sym, stat := range sortedStats(stats) {
				s := stat

				indicator := chevronCollapsed
				if len(s.Positions) == 0 {
					indicator = " "
				}

				items = append(items, drillDownItem{
					header: fmt.Sprintf("    %s (x%d) %s", sym, s.Count, indicator),
					stat:   &s,
				})
			}
		}
	}

	// Blank separator
	items = append(items, drillDownItem{header: "", isSection: true})

	// Outward section
	items = append(items, drillDownItem{header: "Outward Dependencies", isSection: true})
	if len(pkg.Outward) == 0 {
		items = append(items, drillDownItem{header: "  (none)", isSection: true})
	} else {
		for depPkg, stats := range sortedCouplingStats(pkg.Outward) {
			items = append(items, drillDownItem{header: "  " + string(depPkg), isSection: true})

			for sym, stat := range sortedStats(stats) {
				s := stat

				indicator := chevronCollapsed
				if len(s.Positions) == 0 {
					indicator = " "
				}

				items = append(items, drillDownItem{
					header: fmt.Sprintf("    %s (x%d) %s", sym, s.Count, indicator),
					stat:   &s,
				})
			}
		}
	}

	// Commit History section (only when hotspot data is available)
	// Sort reverse-chronologically (newest first).
	slices.SortFunc(commits, func(a, b CommitTouchInfo) int {
		if a.Timestamp.After(b.Timestamp) {
			return -1
		}

		if a.Timestamp.Before(b.Timestamp) {
			return 1
		}

		return 0
	})

	if len(commits) > 0 {
		items = append(items, drillDownItem{header: "", isSection: true})

		items = append(items, drillDownItem{
			header:    fmt.Sprintf("Commit History (%d commits)", len(commits)),
			isSection: true,
		})
		for i := range commits {
			c := commits[i]
			// Truncate message to first line, max 50 chars
			msg := strings.SplitN(c.Message, "\n", firstLineParts)[0]
			if len(msg) > maxCommitMsgLen {
				msg = msg[:maxCommitMsgLen-len(ellipsis)] + ellipsis
			}

			indicator := chevronCollapsed
			if len(c.Files) == 0 {
				indicator = " "
			}

			items = append(items, drillDownItem{
				header: fmt.Sprintf(
					"  %s %-50s  %s  %s",
					c.SHA[:7],
					msg,
					c.Timestamp.Format("2006-01-02"),
					indicator,
				),
				commitInfo: &commits[i],
			})
		}
	}

	return items
}

func (m metricsModel) renderDrillDown() string {
	if m.drillDown == nil {
		return ""
	}

	return m.drillDown.viewport.View()
}

func (dd *drillDownState) renderContent() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorTitle))
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorSection))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorDim))
	selectedStyle := lipgloss.NewStyle().Reverse(true)
	posStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorPosition))

	var b strings.Builder

	b.WriteString(titleStyle.Render(string(dd.pkg.Package)))
	b.WriteString("\n\n")

	fmt.Fprintf(&b, "Inward: %.0f  Outward: %.0f  Instability: %.3f\n\n",
		dd.pkg.InwardCoupling(), dd.pkg.OutwardCoupling(), dd.pkg.Instability())

	addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorAdded))
	removeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorRemoved))
	shaStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorSHA))

	for i, item := range dd.items {
		line := item.header
		switch {
		case item.fileChange != nil:
			if i == dd.cursor {
				line = selectedStyle.Render(fmt.Sprintf("    %-50s  +%d -%d",
					item.fileChange.Path,
					item.fileChange.Additions,
					item.fileChange.Deletions,
				))
			} else {
				// File change lines: color the +N/-N parts
				line = fmt.Sprintf("    %-50s  %s %s",
					item.fileChange.Path,
					addStyle.Render(fmt.Sprintf("+%d", item.fileChange.Additions)),
					removeStyle.Render(fmt.Sprintf("-%d", item.fileChange.Deletions)),
				)
			}
		case item.commitInfo != nil:
			if i == dd.cursor {
				line = selectedStyle.Render(line)
			} else {
				// Style the SHA portion
				sha := item.commitInfo.SHA[:7]
				rest := strings.TrimPrefix(line, "  "+sha)
				line = "  " + shaStyle.Render(sha) + dimStyle.Render(rest)
			}
		case item.isSection:
			if strings.HasPrefix(line, "Inward") || strings.HasPrefix(line, "Outward") ||
				strings.HasPrefix(line, "Commit History") {
				line = sectionStyle.Render(line)
			} else {
				line = dimStyle.Render(line)
			}
		case i == dd.cursor:
			line = selectedStyle.Render(line)
		case item.pos != nil:
			line = posStyle.Render(line)
		default:
			line = dimStyle.Render(line)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

func (dd *drillDownState) moveCursor(delta int) {
	newCursor := max(
		// Clamp to valid range.
		dd.cursor+delta, 0)

	if newCursor >= len(dd.items) {
		newCursor = len(dd.items) - 1
	}

	dir := 1
	if delta < 0 {
		dir = -1
	}
	// Find nearest selectable item in the direction of movement.
	for newCursor >= 0 && newCursor < len(dd.items) {
		if !dd.items[newCursor].isSection {
			dd.cursor = newCursor

			return
		}

		newCursor += dir
	}
}

func (dd *drillDownState) moveCursorToFirst() {
	for i, item := range dd.items {
		if !item.isSection {
			dd.cursor = i

			return
		}
	}
}

func (dd *drillDownState) moveCursorToLast() {
	for i := len(dd.items) - 1; i >= 0; i-- {
		if !dd.items[i].isSection {
			dd.cursor = i

			return
		}
	}
}

func (dd *drillDownState) ensureCursorVisible() {
	// If the cursor is on the first selectable item, scroll to the very top
	// so the title, stats, and section headers above it are visible.
	firstSelectable := -1

	for i, item := range dd.items {
		if !item.isSection {
			firstSelectable = i

			break
		}
	}

	if dd.cursor == firstSelectable {
		dd.viewport.SetYOffset(0)

		return
	}

	// Compute the actual line offset of the cursor item.
	// Each item may span multiple lines (e.g. multi-line coupling text).
	cursorLine := headerLines
	for i := range dd.cursor {
		cursorLine += strings.Count(dd.items[i].header, "\n") + 1
	}

	cursorLines := strings.Count(dd.items[dd.cursor].header, "\n") + 1

	top := dd.viewport.YOffset()

	bottom := top + dd.viewport.Height() - 1
	if cursorLine < top {
		dd.viewport.SetYOffset(cursorLine)
	} else if cursorLine+cursorLines-1 > bottom {
		dd.viewport.SetYOffset(cursorLine + cursorLines - dd.viewport.Height())
	}
}

func (dd *drillDownState) toggleExpand() {
	if dd.cursor < 0 || dd.cursor >= len(dd.items) {
		return
	}

	item := &dd.items[dd.cursor]

	// Handle commit item expansion
	if item.commitInfo != nil {
		dd.toggleExpandCommit()

		return
	}

	if item.stat == nil || len(item.stat.Positions) == 0 {
		return
	}

	// Update the indicator
	sym := item.header
	if strings.HasSuffix(sym, chevronCollapsedSuffix) ||
		strings.HasSuffix(sym, chevronExpandedSuffix) {
		sym = sym[:len(sym)-len(chevronCollapsedSuffix)]
	}

	if item.expanded {
		// Collapse: remove position items that follow
		sym += chevronCollapsedSuffix
		item.header = sym
		item.expanded = false

		removeCount := 0

		for j := dd.cursor + 1; j < len(dd.items); j++ {
			if dd.items[j].pos != nil {
				removeCount++
			} else {
				break
			}
		}

		dd.items = append(dd.items[:dd.cursor+1], dd.items[dd.cursor+1+removeCount:]...)
	} else {
		// Expand: insert position items after the current symbol
		sym += chevronExpandedSuffix
		item.header = sym
		item.expanded = true

		posItems := make([]drillDownItem, 0, len(item.stat.Positions))
		for i, p := range item.stat.Positions {
			posItems = append(posItems, drillDownItem{
				header: fmt.Sprintf("      %s:%d:%d-%d", p.File, p.Line, p.ColStart, p.ColEnd),
				pos:    &item.stat.Positions[i],
			})
		}

		dd.items = slices.Concat(dd.items[:dd.cursor+1], posItems, dd.items[dd.cursor+1:])
	}
}

func (dd *drillDownState) toggleExpandCommit() {
	item := &dd.items[dd.cursor]
	if item.commitInfo == nil {
		return
	}

	// Update the indicator
	hdr := item.header
	if strings.HasSuffix(hdr, chevronCollapsedSuffix) ||
		strings.HasSuffix(hdr, chevronExpandedSuffix) {
		hdr = hdr[:len(hdr)-len(chevronCollapsedSuffix)]
	}

	if item.expanded {
		// Collapse: remove file change items that follow
		hdr += chevronCollapsedSuffix
		item.header = hdr
		item.expanded = false

		removeCount := 0

		for j := dd.cursor + 1; j < len(dd.items); j++ {
			if dd.items[j].fileChange != nil {
				removeCount++
			} else {
				break
			}
		}

		dd.items = append(dd.items[:dd.cursor+1], dd.items[dd.cursor+1+removeCount:]...)
	} else {
		// Expand: insert file change items after the commit header
		hdr += chevronExpandedSuffix
		item.header = hdr
		item.expanded = true

		expandItems := make([]drillDownItem, 0, len(item.commitInfo.Files))

		for i := range item.commitInfo.Files {
			fc := item.commitInfo.Files[i]
			expandItems = append(expandItems, drillDownItem{
				header:     fmt.Sprintf("    %-50s  +%d -%d", fc.Path, fc.Additions, fc.Deletions),
				fileChange: &item.commitInfo.Files[i],
			})
		}

		dd.items = slices.Concat(dd.items[:dd.cursor+1], expandItems, dd.items[dd.cursor+1:])
	}
}

// sortedCouplingStats iterates over PackageCouplingStats in sorted key order.
func sortedCouplingStats(
	pcs analyzer.PackageCouplingStats,
) func(func(analyzer.Package, analyzer.CouplingStats) bool) {
	keys := make([]analyzer.Package, 0, len(pcs))
	for k := range pcs {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return func(yield func(analyzer.Package, analyzer.CouplingStats) bool) {
		for _, k := range keys {
			if !yield(k, pcs[k]) {
				return
			}
		}
	}
}

func sortedStats(stats analyzer.CouplingStats) func(func(string, analyzer.CouplingStat) bool) {
	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return func(yield func(string, analyzer.CouplingStat) bool) {
		for _, k := range keys {
			if !yield(k, stats[k]) {
				return
			}
		}
	}
}
