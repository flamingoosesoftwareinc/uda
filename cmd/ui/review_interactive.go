package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/diff"
)

// RunReviewInteractive launches the interactive TUI for the review command.
func RunReviewInteractive(r ReviewResult) error {
	m := newReviewModel(r)
	p := tea.NewProgram(m)
	_, err := p.Run()

	return err
}

type reviewModel struct {
	result    ReviewResult
	width     int
	height    int
	table     table.Model
	changed   []reviewRow // only packages with changes (for the list)
	drillDown *reviewDrillDownState
}

type reviewRow struct {
	pkg      string
	diffType diff.Type
	pd       diff.PackageDiff
	prev     *analyzer.Metrics
	curr     *analyzer.Metrics
}

func newReviewModel(r ReviewResult) reviewModel {
	var changed []reviewRow

	for _, pkgDiff := range r.Diffs {
		if pkgDiff.DiffType == diff.Unchanged && !packageHasChanges(pkgDiff) {
			continue
		}

		changed = append(changed, reviewRow{
			pkg:      pkgDiff.Package,
			diffType: pkgDiff.DiffType,
			pd:       pkgDiff,
			prev:     findMetrics(r.PrevAll, pkgDiff.Package),
			curr:     findMetrics(r.CurrAll, pkgDiff.Package),
		})
	}

	model := reviewModel{
		result:  r,
		width:   defaultTermWidth,
		height:  defaultTermHeight,
		changed: changed,
	}
	model.table = model.buildTable(defaultTermWidth, initialTableHeight)

	return model
}

func (m reviewModel) Init() tea.Cmd {
	return nil
}

func (m reviewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.resizeAll()

		return m, nil

	case editorFinishedMsg, editorOpenedMsg:
		return m, nil

	case tea.KeyPressMsg:
		if model, cmd, handled := m.handleReviewKey(msg); handled {
			return model, cmd
		}
	}

	// Delegate to table for list navigation (j/k/arrow keys).
	if m.drillDown == nil {
		var cmd tea.Cmd

		m.table, cmd = m.table.Update(msg)

		return m, cmd
	}

	return m, nil
}

func (m reviewModel) View() tea.View {
	var b strings.Builder

	// Tab bar — same style as metrics
	b.WriteString(m.renderTabBar())
	b.WriteString("\n")

	if m.drillDown != nil {
		b.WriteString(m.drillDown.viewport.View())
	} else {
		b.WriteString(m.table.View())
	}

	b.WriteString("\n")
	b.WriteString(m.renderHelp())

	v := tea.NewView(b.String())
	v.AltScreen = true

	return v
}

// handleReviewKey dispatches a key press. The bool is false when the key is not
// consumed here and should fall through to table navigation.
func (m reviewModel) handleReviewKey(msg tea.KeyPressMsg) (reviewModel, tea.Cmd, bool) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit, true
	case "q":
		if m.drillDown != nil {
			m.drillDown = nil

			return m, nil, true
		}

		return m, tea.Quit, true
	case "esc", "backspace":
		if m.drillDown != nil {
			m.drillDown = nil
		}

		return m, nil, true
	case "enter":
		model, cmd := m.handleReviewEnter()

		return model, cmd, true
	}

	if m.drillDown != nil {
		m.handleReviewDrillDownNav(msg)

		return m, nil, true
	}

	return m, nil, false
}

// handleReviewEnter opens a package drill-down, or acts on the selected item.
func (m reviewModel) handleReviewEnter() (reviewModel, tea.Cmd) {
	if m.drillDown == nil {
		return m.enterDrillDown(), nil
	}

	state := m.drillDown
	if state.cursor < 0 || state.cursor >= len(state.items) {
		return m, nil
	}

	item := state.items[state.cursor]
	if item.pos != nil {
		return m, openEditor(*item.pos, "")
	}

	if item.expandable {
		state.toggleExpand()
		state.viewport.SetContent(state.renderContent())
		state.ensureCursorVisible()
	}

	return m, nil
}

// handleReviewDrillDownNav applies cursor navigation inside the drill-down view.
func (m reviewModel) handleReviewDrillDownNav(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "j", "down":
		m.drillDown.moveCursor(1)
	case "k", "up":
		m.drillDown.moveCursor(-1)
	case "g":
		m.drillDown.moveCursorToFirst()
	case "G":
		m.drillDown.moveCursorToLast()
	case "space":
		m.drillDown.toggleExpand()
	case "pgdown", "f":
		m.drillDown.moveCursor(m.drillDown.viewport.Height())
	case "pgup", "b":
		m.drillDown.moveCursor(-m.drillDown.viewport.Height())
	case "ctrl+d":
		m.drillDown.moveCursor(m.drillDown.viewport.Height() / halfPageDivisor)
	case "ctrl+u":
		m.drillDown.moveCursor(-m.drillDown.viewport.Height() / halfPageDivisor)
	default:
		return
	}

	m.drillDown.viewport.SetContent(m.drillDown.renderContent())
	m.drillDown.ensureCursorVisible()
}

func (m reviewModel) buildTable(width, height int) table.Model {
	pkgWidth := max(width-fixedColumnsWidth, minPackageWidth)

	cols := []table.Column{
		{Title: "PACKAGE", Width: pkgWidth},
		{Title: "INSTABILITY", Width: instabilityColWidth},
		{Title: "INWARD", Width: inwardColWidth},
		{Title: "OUTWARD", Width: outwardColWidth},
	}

	rows := make([]table.Row, 0, len(m.changed))
	for _, row := range m.changed {
		var indicator string

		switch row.diffType {
		case diff.Added:
			indicator = "+ "
		case diff.Removed:
			indicator = "- "
		case diff.Unchanged:
			indicator = "~ "
		}

		var instStr, inStr, outStr string

		switch row.diffType {
		case diff.Added:
			instStr = fmt.Sprintf("%.3f", metricsInstability(row.curr))
			inStr = strconv.Itoa(metricsInward(row.curr))
			outStr = strconv.Itoa(metricsOutward(row.curr))
		case diff.Removed:
			instStr = "(removed)"
			inStr = ""
			outStr = ""
		case diff.Unchanged:
			prevI := metricsInstability(row.prev)
			currI := metricsInstability(row.curr)
			instStr = fmt.Sprintf("%.3f → %.3f", prevI, currI)
			inStr = fmt.Sprintf("%d → %d", metricsInward(row.prev), metricsInward(row.curr))
			outStr = fmt.Sprintf("%d → %d", metricsOutward(row.prev), metricsOutward(row.curr))
		}

		rows = append(rows, table.Row{
			indicator + row.pkg,
			instStr,
			inStr,
			outStr,
		})
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
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithStyles(s),
		table.WithWidth(width),
		table.WithHeight(height),
		table.WithFocused(true),
	)
}

func (m reviewModel) renderTabBar() string {
	style := lipgloss.NewStyle().
		Bold(true).
		Reverse(true).
		Padding(0, tabHorizontalPadding)
	label := fmt.Sprintf("%s → %s", m.result.BaseLabel, m.result.HeadLabel)

	return style.Render(label)
}

func (m reviewModel) enterDrillDown() reviewModel {
	row := m.table.SelectedRow()
	if row == nil {
		return m
	}

	cursor := m.table.Cursor()
	if cursor < 0 || cursor >= len(m.changed) {
		return m
	}

	m.drillDown = newReviewDrillDown(
		m.changed[cursor], m.width, m.tableHeight(),
	)

	return m
}

func (m reviewModel) resizeAll() reviewModel {
	tableH := m.tableHeight()
	m.table.SetWidth(m.width)
	m.table.SetHeight(tableH)
	// Recalculate package column width
	cols := m.table.Columns()
	if len(cols) == baseColumnCount {
		pkgWidth := max(m.width-fixedColumnsWidth, minPackageWidth)

		cols[0].Width = pkgWidth
		m.table.SetColumns(cols)
	}

	if m.drillDown != nil {
		m.drillDown.viewport.SetWidth(m.width)
		m.drillDown.viewport.SetHeight(tableH)
	}

	return m
}

func (m reviewModel) renderHelp() string {
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorDim))

	if m.drillDown != nil {
		help := "esc/backspace: back | j/k: navigate | space: expand"

		dd := m.drillDown
		if dd.cursor >= 0 && dd.cursor < len(dd.items) && dd.items[dd.cursor].pos != nil {
			help += " | enter: open in editor"
		}

		help += " | q: quit"

		return helpStyle.Render(help)
	}

	return helpStyle.Render("j/k: navigate | enter: details | q: quit")
}

// tableHeight returns the available height for the table/viewport
// (total height minus tab bar and help bar).
func (m reviewModel) tableHeight() int {
	// 1 for tab bar + 1 newline after table + 1 for help bar
	h := max(m.height-tableChromeHeight, 1)

	return h
}

// --- Review Drill-Down ---

type reviewDrillDownItem struct {
	text       string
	isSection  bool
	diffType   diff.Type
	expandable bool
	expanded   bool
	pos        *analyzer.Position
	// symbolDiff holds the symbol data for expand/collapse of positions
	symbolDiff *diff.SymbolDiff
}

type reviewDrillDownState struct {
	row      reviewRow
	viewport viewport.Model
	items    []reviewDrillDownItem
	cursor   int
}

func newReviewDrillDown(row reviewRow, width, height int) *reviewDrillDownState {
	state := &reviewDrillDownState{
		row:      row,
		viewport: viewport.New(viewport.WithWidth(width), viewport.WithHeight(height)),
	}
	state.buildItems()
	state.viewport.SetContent(state.renderContent())

	return state
}

func (dd *reviewDrillDownState) buildItems() {
	items := dd.headerItems()
	items = append(items, dd.metricsItems()...)
	items = append(items, reviewDrillDownItem{text: "", isSection: true})

	newDeps, removedDeps, changedDeps := categorizeOutwardDeps(dd.row.pd)

	items = dd.appendDependencySection(items, "New Dependencies", newDeps, diff.Added)
	items = dd.appendDependencySection(items, "Removed Dependencies", removedDeps, diff.Removed)
	items = dd.appendDependencySection(items, "Changed Dependencies", changedDeps, diff.Unchanged)

	if len(newDeps) == 0 && len(removedDeps) == 0 && len(changedDeps) == 0 {
		items = append(items, reviewDrillDownItem{
			text:      "No outward dependency changes",
			isSection: true,
		})
	}

	dd.items = items
	dd.selectFirstItem()
}

// headerItems builds the package title (with add/remove status) and a blank line.
func (dd *reviewDrillDownState) headerItems() []reviewDrillDownItem {
	var statusStr string

	switch dd.row.diffType {
	case diff.Added:
		statusStr = " (new)"
	case diff.Removed:
		statusStr = " (removed)"
	case diff.Unchanged:
		// no status suffix for unchanged packages
	}

	return []reviewDrillDownItem{
		{text: dd.row.pkg + statusStr, isSection: true},
		{text: "", isSection: true},
	}
}

// metricsItems builds the coupling/instability summary line, or nil when the
// current metrics are unavailable.
func (dd *reviewDrillDownState) metricsItems() []reviewDrillDownItem {
	if dd.row.curr == nil {
		return nil
	}

	var metricsLine string
	if dd.row.prev != nil {
		metricsLine = fmt.Sprintf("Inward: %d → %d  Outward: %d → %d  Instability: %.2f → %.2f",
			metricsInward(dd.row.prev), metricsInward(dd.row.curr),
			metricsOutward(dd.row.prev), metricsOutward(dd.row.curr),
			metricsInstability(dd.row.prev), metricsInstability(dd.row.curr))
	} else {
		metricsLine = fmt.Sprintf("Inward: %d  Outward: %d  Instability: %.2f",
			metricsInward(dd.row.curr), metricsOutward(dd.row.curr),
			metricsInstability(dd.row.curr))
	}

	return []reviewDrillDownItem{{text: metricsLine, isSection: true}}
}

// categorizeOutwardDeps splits a package's outward dependency diffs into
// added, removed, and changed (symbol-level) groups.
func categorizeOutwardDeps(
	pkgDiff diff.PackageDiff,
) ([]diff.DependencyDiff, []diff.DependencyDiff, []diff.DependencyDiff) {
	var newDeps, removedDeps, changedDeps []diff.DependencyDiff

	for _, dep := range pkgDiff.OutwardDiff {
		switch dep.DiffType {
		case diff.Added:
			newDeps = append(newDeps, dep)
		case diff.Removed:
			removedDeps = append(removedDeps, dep)
		case diff.Unchanged:
			if hasSymbolChanges(dep.Symbols) {
				changedDeps = append(changedDeps, dep)
			}
		}
	}

	return newDeps, removedDeps, changedDeps
}

// appendDependencySection appends a titled section of dependency edges (with
// their symbol items) to items, doing nothing when deps is empty.
func (dd *reviewDrillDownState) appendDependencySection(
	items []reviewDrillDownItem,
	title string,
	deps []diff.DependencyDiff,
	depDiffType diff.Type,
) []reviewDrillDownItem {
	if len(deps) == 0 {
		return items
	}

	items = append(items, reviewDrillDownItem{text: title, isSection: true})

	for _, dep := range deps {
		items = append(items, reviewDrillDownItem{
			text:      "  → " + dep.Package,
			isSection: true,
			diffType:  depDiffType,
		})
		items = append(items, dd.symbolItems(dep.Symbols)...)
	}

	return append(items, reviewDrillDownItem{text: "", isSection: true})
}

// selectFirstItem places the cursor on the first non-section item.
func (dd *reviewDrillDownState) selectFirstItem() {
	for i, item := range dd.items {
		if !item.isSection {
			dd.cursor = i

			return
		}
	}
}

func (dd *reviewDrillDownState) symbolItems(syms []diff.SymbolDiff) []reviewDrillDownItem {
	var items []reviewDrillDownItem

	for i := range syms {
		s := syms[i]
		if s.DiffType == diff.Unchanged && s.PrevCount == s.CurrCount {
			continue
		}

		indicator := " "

		hasPositions := len(s.Positions) > 0
		if hasPositions {
			indicator = "▸"
		}

		var countStr string

		switch s.DiffType {
		case diff.Added:
			countStr = fmt.Sprintf("x%d", s.CurrCount)
		case diff.Removed:
			countStr = fmt.Sprintf("x%d", s.PrevCount)
		case diff.Unchanged:
			if s.PrevCount != s.CurrCount {
				//nolint:gosec // symbol occurrence counts; uint→int cannot overflow at realistic counts.
				delta := int(s.CurrCount) - int(s.PrevCount)
				if delta > 0 {
					countStr = fmt.Sprintf("x%d (+%d)", s.CurrCount, delta)
				} else {
					countStr = fmt.Sprintf("x%d (%d)", s.CurrCount, delta)
				}
			} else {
				countStr = fmt.Sprintf("x%d", s.CurrCount)
			}
		}

		sd := s
		items = append(items, reviewDrillDownItem{
			text:       fmt.Sprintf("    %s (%s) %s", s.Name, countStr, indicator),
			diffType:   s.DiffType,
			expandable: hasPositions,
			symbolDiff: &sd,
		})
	}

	return items
}

func (dd *reviewDrillDownState) renderContent() string {
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

		switch {
		case item.isSection:
			switch {
			case i == 0:
				line = titleStyle.Render(line)
			case item.text == "New Dependencies",
				item.text == "Removed Dependencies",
				item.text == "Changed Dependencies":
				line = sectionStyle.Render(line)
			case item.diffType == diff.Added:
				line = addedStyle.Render(line)
			case item.diffType == diff.Removed:
				line = removedStyle.Render(line)
			default:
				line = dimStyle.Render(line)
			}
		case i == dd.cursor:
			line = selectedStyle.Render(line)
		case item.pos != nil:
			line = posStyle.Render(line)
		case item.diffType == diff.Added:
			line = addedStyle.Render(line)
		case item.diffType == diff.Removed:
			line = removedStyle.Render(line)
		case strings.Contains(line, "(+") || strings.Contains(line, "(-"):
			line = changedStyle.Render(line)
		default:
			line = dimStyle.Render(line)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

func (dd *reviewDrillDownState) moveCursor(delta int) {
	newCursor := max(dd.cursor+delta, 0)

	if newCursor >= len(dd.items) {
		newCursor = len(dd.items) - 1
	}

	dir := 1
	if delta < 0 {
		dir = -1
	}

	for newCursor >= 0 && newCursor < len(dd.items) {
		if !dd.items[newCursor].isSection {
			dd.cursor = newCursor

			return
		}

		newCursor += dir
	}
}

func (dd *reviewDrillDownState) moveCursorToFirst() {
	for i, item := range dd.items {
		if !item.isSection {
			dd.cursor = i

			return
		}
	}
}

func (dd *reviewDrillDownState) moveCursorToLast() {
	for i := len(dd.items) - 1; i >= 0; i-- {
		if !dd.items[i].isSection {
			dd.cursor = i

			return
		}
	}
}

func (dd *reviewDrillDownState) ensureCursorVisible() {
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

func (dd *reviewDrillDownState) toggleExpand() {
	if dd.cursor < 0 || dd.cursor >= len(dd.items) {
		return
	}

	item := &dd.items[dd.cursor]
	if !item.expandable || item.symbolDiff == nil {
		return
	}

	// Update indicator
	text := item.text
	if strings.HasSuffix(text, " ▸") || strings.HasSuffix(text, " ▾") {
		text = text[:len(text)-len(" ▸")]
	}

	if item.expanded {
		// Collapse: remove position items
		text += " ▸"
		item.text = text
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
		// Expand: insert position items
		text += " ▾"
		item.text = text
		item.expanded = true

		var posItems []reviewDrillDownItem

		for _, positionDiff := range item.symbolDiff.Positions {
			p := positionDiff.Position
			pos := p // copy for pointer stability
			prefix := " "

			diffKind := positionDiff.DiffType
			switch positionDiff.DiffType {
			case diff.Added:
				prefix = "+"
			case diff.Removed:
				prefix = "-"
			case diff.Unchanged:
				diffKind = item.symbolDiff.DiffType
			}

			posItems = append(posItems, reviewDrillDownItem{
				text: fmt.Sprintf(
					"      %s %s:%d:%d-%d",
					prefix,
					p.File,
					p.Line,
					p.ColStart,
					p.ColEnd,
				),
				pos:      &pos,
				diffType: diffKind,
			})
		}

		dd.items = slices.Concat(dd.items[:dd.cursor+1], posItems, dd.items[dd.cursor+1:])
	}
}
