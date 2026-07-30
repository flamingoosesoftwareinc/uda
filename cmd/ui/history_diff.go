package ui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/diff"
)

const (
	instabilityEpsilon = 0.001 // instability deltas below this are treated as no change
	initialDiffItems   = 10    // initial capacity for a simple-diff item slice
)

// historyDrillDownState manages the diff view between two commits.
type historyDrillDownState struct {
	pkgName         string
	current         *PackageDataPoint
	previous        *PackageDataPoint
	currentMetrics  *analyzer.Metrics
	previousMetrics *analyzer.Metrics
	viewport        viewport.Model
	items           []historyDiffItem
	cursor          int
	changeFreq      float64
	hotspotScore    float64
}

type historyDiffItem struct {
	text      string
	isSection bool
	// diffType indicates if this is added/removed/unchanged
	diffType diffType
	// stat for expandable symbol lines (positions) - current commit's stat
	stat *analyzer.CouplingStat
	// prevStat holds the previous commit's stat for comparison (when symbol exists in both)
	prevStat *analyzer.CouplingStat
	// expanded shows if positions are visible
	expanded bool
	// pos for position lines (openable in editor)
	pos *analyzer.Position
	// sha is the commit SHA this item's positions belong to
	sha string
	// prevSHA is the previous commit SHA (for removed positions)
	prevSHA string
}

type diffType int

const (
	diffUnchanged diffType = iota
	diffAdded
	diffRemoved
)

// mapDiffType converts from the domain diff type to the UI diff type.
func mapDiffType(dt diff.Type) diffType {
	switch dt {
	case diff.Added:
		return diffAdded
	case diff.Removed:
		return diffRemoved
	case diff.Unchanged:
		return diffUnchanged
	default:
		return diffUnchanged
	}
}

func newHistoryDrillDownState(
	pkgName string,
	current, previous *PackageDataPoint,
	currentMetrics, previousMetrics *analyzer.Metrics,
	width, height int,
) *historyDrillDownState {
	state := &historyDrillDownState{
		pkgName:         pkgName,
		current:         current,
		previous:        previous,
		currentMetrics:  currentMetrics,
		previousMetrics: previousMetrics,
		viewport:        viewport.New(viewport.WithWidth(width), viewport.WithHeight(height)),
	}

	state.buildItems()

	// Release the heavy metrics now that items are built.
	// The items list contains all data needed for rendering and interaction.
	state.currentMetrics = nil
	state.previousMetrics = nil

	state.viewport.SetContent(state.renderContent())

	return state
}

func (dd *historyDrillDownState) buildItems() {
	var items []historyDiffItem

	// Header
	items = append(items, historyDiffItem{
		text:      "Package: " + dd.pkgName,
		isSection: true,
	})
	items = append(items, historyDiffItem{text: "", isSection: true})

	// Commit info
	items = append(items, dd.currentCommitItems()...)

	items = append(items, historyDiffItem{text: "", isSection: true})

	// If we have full metrics, show dependency diff
	switch {
	case dd.currentMetrics != nil:
		items = append(items, dd.buildDependencyDiff()...)
	case dd.previous != nil:
		// Fallback to simple summary diff
		items = append(items, dd.buildSimpleDiff()...)
	default:
		items = append(items, historyDiffItem{
			text:      "No previous commit for comparison",
			isSection: true,
		})
	}

	dd.items = items

	// Find first selectable item
	for i, item := range items {
		if !item.isSection {
			dd.cursor = i

			break
		}
	}
}

// currentCommitItems builds the "Current Commit" section, or nil when there is
// no current commit.
func (dd *historyDrillDownState) currentCommitItems() []historyDiffItem {
	if dd.current == nil {
		return nil
	}

	items := []historyDiffItem{
		{text: "Current Commit", isSection: true},
		{text: "  SHA: " + dd.current.SHA[:7]},
		{text: "  Message: " + truncateMessage(dd.current.Message, maxCommitMsgLen)},
		{text: "  Date: " + dd.current.Timestamp.Format("2006-01-02 15:04")},
	}

	return append(items, historyDiffItem{text: dd.currentStatsLine()})
}

// currentStatsLine formats the coupling/instability stats line for the current
// commit, annotating deltas against the previous commit when available.
func (dd *historyDrillDownState) currentStatsLine() string {
	var inSuffix, outSuffix, instSuffix string

	if dd.previous != nil {
		if d := dd.current.Inward - dd.previous.Inward; d != 0 {
			inSuffix = fmt.Sprintf(" (%s%d)", signPrefix(d), d)
		}

		if d := dd.current.Outward - dd.previous.Outward; d != 0 {
			outSuffix = fmt.Sprintf(" (%s%d)", signPrefix(d), d)
		}

		if d := dd.current.Instability - dd.previous.Instability; d > instabilityEpsilon ||
			d < -instabilityEpsilon {
			instSuffix = fmt.Sprintf(" (%s%.2f)", signPrefixFloat(d), d)
		}
	}

	statsLine := fmt.Sprintf("  Inward: %d%s  Outward: %d%s  Instability: %.2f%s",
		dd.current.Inward, inSuffix,
		dd.current.Outward, outSuffix,
		dd.current.Instability, instSuffix)
	if dd.changeFreq > 0 || dd.hotspotScore > 0 {
		statsLine += fmt.Sprintf("  ChngFreq: %.3f  Hotspot: %.3f",
			dd.changeFreq, dd.hotspotScore)
	}

	return statsLine
}

func (dd *historyDrillDownState) buildSimpleDiff() []historyDiffItem {
	items := make([]historyDiffItem, 0, initialDiffItems)

	items = append(items, historyDiffItem{
		text:      "Previous Commit",
		isSection: true,
	})
	items = append(items, historyDiffItem{
		text: "  SHA: " + dd.previous.SHA[:7],
	})
	items = append(items, historyDiffItem{
		text: "  Message: " + truncateMessage(dd.previous.Message, maxCommitMsgLen),
	})
	items = append(items, historyDiffItem{
		text: "  Date: " + dd.previous.Timestamp.Format("2006-01-02 15:04"),
	})
	items = append(items, historyDiffItem{
		text: fmt.Sprintf("  Inward: %d  Outward: %d  Instability: %.2f",
			dd.previous.Inward, dd.previous.Outward, dd.previous.Instability),
	})

	items = append(items, historyDiffItem{text: "", isSection: true})

	// Changes section
	items = append(items, historyDiffItem{
		text:      "Summary Changes",
		isSection: true,
	})

	inwardDelta := dd.current.Inward - dd.previous.Inward
	outwardDelta := dd.current.Outward - dd.previous.Outward
	instabilityDelta := dd.current.Instability - dd.previous.Instability

	items = append(items, historyDiffItem{
		text: formatDelta("Inward", inwardDelta),
	})
	items = append(items, historyDiffItem{
		text: formatDelta("Outward", outwardDelta),
	})
	items = append(items, historyDiffItem{
		text: formatDeltaFloat("Instability", instabilityDelta),
	})

	return items
}

func (dd *historyDrillDownState) buildDependencyDiff() []historyDiffItem {
	//nolint:prealloc // grown across many heterogeneous append blocks; final size is data-dependent.
	var items []historyDiffItem

	var (
		prevInward, prevOutward analyzer.PackageCouplingStats
		prevSHA                 string
	)

	if dd.previousMetrics != nil {
		prevInward = dd.previousMetrics.Inward
		prevOutward = dd.previousMetrics.Outward
	}

	if dd.previous != nil {
		prevSHA = dd.previous.SHA
	}

	currSHA := ""
	if dd.current != nil {
		currSHA = dd.current.SHA
	}

	// Inward dependencies diff
	items = append(items, historyDiffItem{
		text:      "Inward Dependencies (who depends on this package)",
		isSection: true,
	})
	items = append(items, couplingDiffToItems(
		diff.CouplingStats(prevInward, dd.currentMetrics.Inward),
		prevSHA, currSHA,
		prevInward, dd.currentMetrics.Inward,
	)...)

	items = append(items, historyDiffItem{text: "", isSection: true})

	// Outward dependencies diff
	items = append(items, historyDiffItem{
		text:      "Outward Dependencies (what this package depends on)",
		isSection: true,
	})
	items = append(items, couplingDiffToItems(
		diff.CouplingStats(prevOutward, dd.currentMetrics.Outward),
		prevSHA, currSHA,
		prevOutward, dd.currentMetrics.Outward,
	)...)

	return items
}

// couplingDiffToItems maps domain DependencyDiff results into TUI historyDiffItems.
// It needs the original coupling stats to preserve CouplingStat references for
// the expand/collapse position drill-down in the TUI.
func couplingDiffToItems(
	diffs []diff.DependencyDiff,
	prevSHA, currSHA string,
	prev, curr analyzer.PackageCouplingStats,
) []historyDiffItem {
	if len(diffs) == 0 {
		return []historyDiffItem{{
			text:      "  (none)",
			isSection: true,
		}}
	}

	if prev == nil {
		prev = make(analyzer.PackageCouplingStats)
	}

	if curr == nil {
		curr = make(analyzer.PackageCouplingStats)
	}

	var items []historyDiffItem
	for _, dep := range diffs {
		items = append(items, historyDiffItem{
			text:      "  " + dep.Package,
			isSection: true,
			diffType:  mapDiffType(dep.DiffType),
		})

		pkg := analyzer.Package(dep.Package)
		prevStats := prev[pkg]
		currStats := curr[pkg]

		items = append(items, symbolDiffToItems(
			dep.Symbols, prevSHA, currSHA, prevStats, currStats,
		)...)
	}

	return items
}

// symbolDiffToItems maps domain SymbolDiff results into TUI historyDiffItems.
func symbolDiffToItems(
	diffs []diff.SymbolDiff,
	prevSHA, currSHA string,
	prevStats, currStats analyzer.CouplingStats,
) []historyDiffItem {
	if prevStats == nil {
		prevStats = make(analyzer.CouplingStats)
	}

	if currStats == nil {
		currStats = make(analyzer.CouplingStats)
	}

	items := make([]historyDiffItem, 0, len(diffs))

	for _, symbolDiff := range diffs {
		var (
			stat     analyzer.CouplingStat
			pStat    *analyzer.CouplingStat
			countStr string
			sha      string
			pSHA     string
		)

		switch symbolDiff.DiffType {
		case diff.Added:
			stat = currStats[symbolDiff.Name]
			countStr = fmt.Sprintf("x%d", symbolDiff.CurrCount)
			sha = currSHA
		case diff.Removed:
			stat = prevStats[symbolDiff.Name]
			countStr = fmt.Sprintf("x%d", symbolDiff.PrevCount)
			sha = prevSHA
		case diff.Unchanged:
			stat = currStats[symbolDiff.Name]
			ps := prevStats[symbolDiff.Name]
			pStat = &ps
			sha = currSHA
			pSHA = prevSHA

			if symbolDiff.PrevCount != symbolDiff.CurrCount {
				//nolint:gosec // symbol occurrence counts; uint→int cannot overflow at realistic counts.
				delta := int(symbolDiff.CurrCount) - int(symbolDiff.PrevCount)
				if delta > 0 {
					countStr = fmt.Sprintf("x%d (+%d)", symbolDiff.CurrCount, delta)
				} else {
					countStr = fmt.Sprintf("x%d (%d)", symbolDiff.CurrCount, delta)
				}
			} else {
				countStr = fmt.Sprintf("x%d", symbolDiff.CurrCount)
			}
		}

		indicator := " "
		if len(stat.Positions) > 0 || (pStat != nil && len(pStat.Positions) > 0) {
			indicator = "▸"
		}

		s := stat
		items = append(items, historyDiffItem{
			text:     fmt.Sprintf("    %s (%s) %s", symbolDiff.Name, countStr, indicator),
			diffType: mapDiffType(symbolDiff.DiffType),
			stat:     &s,
			prevStat: pStat,
			sha:      sha,
			prevSHA:  pSHA,
		})
	}

	return items
}

func (dd *historyDrillDownState) renderContent() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorTitle))
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorSection))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorDim))
	selectedStyle := lipgloss.NewStyle().Reverse(true)
	posStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorPosition))
	addedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorAdded))
	removedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorRemoved))
	changedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorChanged))

	var b strings.Builder

	for i, item := range dd.items {
		line := item.text

		// Apply base styling
		switch {
		case item.isSection:
			switch {
			case strings.HasPrefix(line, "Package:"):
				line = titleStyle.Render(line)
			case strings.HasPrefix(line, "Inward Dep"),
				strings.HasPrefix(line, "Outward Dep"),
				line == "Current Commit",
				line == "Previous Commit",
				line == "Summary Changes":
				line = sectionStyle.Render(line)
			case item.diffType == diffAdded:
				line = addedStyle.Render("+ " + strings.TrimPrefix(line, "  "))
			case item.diffType == diffRemoved:
				line = removedStyle.Render("- " + strings.TrimPrefix(line, "  "))
			default:
				line = dimStyle.Render(line)
			}
		case i == dd.cursor:
			line = selectedStyle.Render(line)
		case item.pos != nil:
			line = posStyle.Render(line)
		case item.diffType == diffAdded:
			line = addedStyle.Render(line)
		case item.diffType == diffRemoved:
			line = removedStyle.Render(line)
		case strings.Contains(line, "(+") || strings.Contains(line, "(-"):
			// Count changed
			line = changedStyle.Render(line)
		default:
			line = dimStyle.Render(line)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

func (dd *historyDrillDownState) moveCursor(delta int) {
	newCursor := dd.cursor + delta
	for newCursor >= 0 && newCursor < len(dd.items) {
		if !dd.items[newCursor].isSection {
			dd.cursor = newCursor

			break
		}

		newCursor += delta
	}

	dd.ensureCursorVisible()
}

func (dd *historyDrillDownState) moveCursorToFirst() {
	for i, item := range dd.items {
		if !item.isSection {
			dd.cursor = i

			break
		}
	}

	dd.ensureCursorVisible()
}

func (dd *historyDrillDownState) moveCursorToLast() {
	for i := len(dd.items) - 1; i >= 0; i-- {
		if !dd.items[i].isSection {
			dd.cursor = i

			break
		}
	}

	dd.ensureCursorVisible()
}

func (dd *historyDrillDownState) ensureCursorVisible() {
	// If the cursor is on the first selectable item, scroll to the very top
	// so the package heading and commit info above it are visible.
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

	// Compute actual line offset accounting for multi-line items.
	cursorLine := 0
	for i := range dd.cursor {
		cursorLine += strings.Count(dd.items[i].text, "\n") + 1
	}

	cursorLines := strings.Count(dd.items[dd.cursor].text, "\n") + 1

	top := dd.viewport.YOffset()

	bottom := top + dd.viewport.Height() - 1
	if cursorLine < top {
		dd.viewport.SetYOffset(cursorLine)
	} else if cursorLine+cursorLines-1 > bottom {
		dd.viewport.SetYOffset(cursorLine + cursorLines - dd.viewport.Height())
	}
}

func (dd *historyDrillDownState) toggleExpand() {
	if dd.cursor < 0 || dd.cursor >= len(dd.items) {
		return
	}

	item := &dd.items[dd.cursor]
	if item.stat == nil || len(item.stat.Positions) == 0 {
		return
	}

	// Update the indicator
	sym := item.text
	if strings.HasSuffix(sym, " ▸") || strings.HasSuffix(sym, " ▾") {
		sym = sym[:len(sym)-len(" ▸")]
	}

	if item.expanded {
		dd.collapseSymbol(item, sym)
	} else {
		dd.expandSymbol(item, sym)
	}
}

// collapseSymbol collapses an expanded symbol, removing the position items that follow it.
func (dd *historyDrillDownState) collapseSymbol(item *historyDiffItem, sym string) {
	item.text = sym + " ▸"
	item.expanded = false

	removeCount := 0

	for j := dd.cursor + 1; j < len(dd.items); j++ {
		if dd.items[j].pos == nil {
			break
		}

		removeCount++
	}

	dd.items = append(dd.items[:dd.cursor+1], dd.items[dd.cursor+1+removeCount:]...)
}

// expandSymbol expands a symbol, inserting its position items after it.
func (dd *historyDrillDownState) expandSymbol(item *historyDiffItem, sym string) {
	item.text = sym + " ▾"
	item.expanded = true

	posItems := dd.symbolPositionItems(item)
	dd.items = slices.Concat(dd.items[:dd.cursor+1], posItems, dd.items[dd.cursor+1:])
}

// symbolPositionItems builds the position items for an expanded symbol.
func (dd *historyDrillDownState) symbolPositionItems(item *historyDiffItem) []historyDiffItem {
	if item.prevStat != nil {
		// Symbol exists in both commits — show position diff.
		posDiffs := diff.Positions(item.prevStat.Positions, item.stat.Positions)

		return positionDiffToItems(posDiffs, item.prevSHA, item.sha)
	}

	// Symbol only in one commit — all positions have the same diff type.
	posItems := make([]historyDiffItem, 0, len(item.stat.Positions))
	for i, p := range item.stat.Positions {
		posItems = append(posItems, historyDiffItem{
			text: fmt.Sprintf(
				"      %s:%d:%d-%d",
				p.File,
				p.Line,
				p.ColStart,
				p.ColEnd,
			),
			pos:      &item.stat.Positions[i],
			diffType: item.diffType,
			sha:      item.sha,
		})
	}

	return posItems
}

// positionDiffToItems maps domain PositionDiff results into TUI historyDiffItems.
func positionDiffToItems(diffs []diff.PositionDiff, prevSHA, currSHA string) []historyDiffItem {
	items := make([]historyDiffItem, 0, len(diffs))

	for _, positionDiff := range diffs {
		p := positionDiff.Position

		sha := currSHA
		if positionDiff.DiffType == diff.Removed {
			sha = prevSHA
		}

		pos := p // copy for pointer stability
		items = append(items, historyDiffItem{
			text:     fmt.Sprintf("      %s:%d:%d-%d", p.File, p.Line, p.ColStart, p.ColEnd),
			pos:      &pos,
			diffType: mapDiffType(positionDiff.DiffType),
			sha:      sha,
		})
	}

	return items
}

func formatDelta(label string, delta int) string {
	if delta > 0 {
		return fmt.Sprintf("  %s: +%d", label, delta)
	} else if delta < 0 {
		return fmt.Sprintf("  %s: %d", label, delta)
	}

	return fmt.Sprintf("  %s: no change", label)
}

func formatDeltaFloat(label string, delta float64) string {
	if delta > instabilityEpsilon {
		return fmt.Sprintf("  %s: +%.2f", label, delta)
	} else if delta < -instabilityEpsilon {
		return fmt.Sprintf("  %s: %.2f", label, delta)
	}

	return fmt.Sprintf("  %s: no change", label)
}

func signPrefix(v int) string {
	if v > 0 {
		return "+"
	}

	return ""
}

func signPrefixFloat(v float64) string {
	if v > 0 {
		return "+"
	}

	return ""
}

func truncateMessage(msg string, maxLen int) string {
	if len(msg) <= maxLen {
		return msg
	}

	return msg[:maxLen-3] + "..."
}
