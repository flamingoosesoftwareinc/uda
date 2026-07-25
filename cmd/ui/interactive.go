package ui

import (
	"regexp"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// Canonical bubbletea key strings reused across switch arms in the metrics
// model. Kept here so a misspelling at one site can't drift the others.
const (
	keyCtrlC = "ctrl+c"
	keyEnter = "enter"
	keyEsc   = "esc"
	keyDown  = "down"
)

const (
	filterInputWidth     = 40  // regex-filter text-input width in cells
	filterInputCharLimit = 256 // regex-filter text-input maximum length
	initialTableHeight   = 20  // table body height before the first WindowSizeMsg
	halfPageDivisor      = 2   // ctrl+d/ctrl+u scroll half a viewport
)

// RunInteractive launches the interactive TUI for metrics display.
func RunInteractive(
	groups []LanguageMetrics,
	criteria []SortCriterion,
	filter *regexp.Regexp,
) error {
	m := newMetricsModel(groups, criteria, filter, "", false)
	p := tea.NewProgram(m)
	_, err := p.Run()

	return err
}

type metricsModel struct {
	groups       []LanguageMetrics
	activeTab    int
	tables       []table.Model
	width        int
	height       int
	drillDown    *drillDownState
	sortCriteria []SortCriterion
	filterMode   bool
	filterInput  textinput.Model
	filterRegex  *regexp.Regexp
	filterErr    string
	rootDir      string
	showHelp     bool
	hasHotspots  bool
}

func newMetricsModel(
	groups []LanguageMetrics,
	criteria []SortCriterion,
	filter *regexp.Regexp,
	rootDir string,
	hasHotspots bool,
) metricsModel {
	filterInput := textinput.New()
	filterInput.Placeholder = "regex filter..."
	filterInput.CharLimit = filterInputCharLimit
	filterInput.SetWidth(filterInputWidth)

	model := metricsModel{
		groups:       groups,
		activeTab:    0,
		width:        defaultTermWidth,
		height:       defaultTermHeight,
		sortCriteria: criteria,
		filterInput:  filterInput,
		rootDir:      rootDir,
		hasHotspots:  hasHotspots,
	}

	if filter != nil {
		model.filterInput.SetValue(filter.String())
		model.filterRegex = filter
	}

	tables := make([]table.Model, len(groups))
	for i, g := range groups {
		filtered := FilterMetrics(g.Metrics, model.filterRegex)
		tables[i] = buildTableWithHotspots(
			filtered,
			defaultTermWidth,
			initialTableHeight,
			criteria,
			g.Hotspots,
		)
	}

	model.tables = tables

	return model
}

func (m metricsModel) Init() tea.Cmd {
	return nil
}

func (m metricsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.resizeAll()

		return m, nil

	case editorFinishedMsg, editorOpenedMsg:
		return m, nil

	case tea.KeyPressMsg:
		if model, cmd, handled := m.handleKeyPress(msg); handled {
			return model, cmd
		}
	}

	if m.drillDown != nil {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			m = m.handleDrillDownNav(key)
		}
		// Don't delegate key/mouse events to the viewport — all scrolling
		// is driven by cursor movement to keep the highlight visible.
		return m, nil
	}

	if len(m.tables) > 0 {
		var cmd tea.Cmd

		m.tables[m.activeTab], cmd = m.tables[m.activeTab].Update(msg)

		return m, cmd
	}

	return m, nil
}

func (m metricsModel) View() tea.View {
	var b strings.Builder

	// Tab bar
	b.WriteString(m.renderTabBar())
	b.WriteString("\n")

	if m.drillDown != nil {
		b.WriteString(m.renderDrillDown())
	} else if len(m.tables) > 0 {
		b.WriteString(m.tables[m.activeTab].View())
	}

	b.WriteString("\n")
	b.WriteString(m.renderHelp())

	view := b.String()

	if m.showHelp {
		view = overlayCenter(view, renderHelpBox(), m.width, m.height)
	}

	v := tea.NewView(view)
	v.AltScreen = true

	return v
}

// handleKeyPress dispatches a key in help/filter/table context. The bool is
// true when the key was fully handled and the returned model/cmd should be used.
func (m metricsModel) handleKeyPress(msg tea.KeyPressMsg) (metricsModel, tea.Cmd, bool) {
	if m.showHelp {
		m.showHelp = false

		return m, nil, true
	}

	if m.filterMode {
		model, cmd := m.handleFilterKey(msg)

		return model, cmd, true
	}

	return m.handleTableKey(msg)
}

// handleFilterKey processes a key while the regex filter input is focused.
func (m metricsModel) handleFilterKey(msg tea.KeyPressMsg) (metricsModel, tea.Cmd) {
	switch msg.String() {
	case keyCtrlC:
		return m, tea.Quit
	case keyEnter:
		m.filterMode = false
		m.filterInput.Blur()

		return m, nil
	case keyEsc:
		m.filterMode = false
		m.filterInput.SetValue("")
		m.filterInput.Blur()
		m.filterRegex = nil
		m.filterErr = ""

		return m.rebuildTables(), nil
	default:
		var cmd tea.Cmd

		m.filterInput, cmd = m.filterInput.Update(msg)

		return m.applyFilterInput(), cmd
	}
}

// applyFilterInput recompiles the regex from the filter input and rebuilds tables.
func (m metricsModel) applyFilterInput() metricsModel {
	val := m.filterInput.Value()
	if val == "" {
		m.filterRegex = nil
		m.filterErr = ""

		return m.rebuildTables()
	}

	compiled, err := regexp.Compile(val)
	if err != nil {
		m.filterErr = "invalid regex"
		m.filterRegex = nil
	} else {
		m.filterErr = ""
		m.filterRegex = compiled
	}

	return m.rebuildTables()
}

// handleTableKey processes a key in the main table view. The bool is false when
// the key is not consumed here and should fall through to drill-down handling.
func (m metricsModel) handleTableKey(msg tea.KeyPressMsg) (metricsModel, tea.Cmd, bool) {
	switch msg.String() {
	case keyCtrlC:
		return m, tea.Quit, true
	case "q":
		if m.drillDown != nil {
			m.drillDown = nil

			return m, nil, true
		}

		return m, tea.Quit, true
	case keyEsc, "backspace":
		if m.drillDown != nil {
			m.drillDown = nil
		}

		return m, nil, true
	case "tab":
		if m.drillDown == nil && len(m.groups) > 1 {
			m.activeTab = (m.activeTab + 1) % len(m.groups)
		}

		return m, nil, true
	case "shift+tab":
		if m.drillDown == nil && len(m.groups) > 1 {
			m.activeTab = (m.activeTab - 1 + len(m.groups)) % len(m.groups)
		}

		return m, nil, true
	case keyEnter:
		return m.handleEnterKey()
	case "/":
		if m.drillDown == nil {
			m.filterMode = true
			m.filterInput.Focus()

			return m, textinput.Blink, true
		}
	case "?":
		if m.drillDown == nil {
			m.showHelp = true

			return m, nil, true
		}
	default:
		if m.drillDown == nil && isSortKey(msg.String()) {
			field := sortKeyToField(msg.String())
			m.sortCriteria = ToggleSort(m.sortCriteria, field)

			return m.rebuildTables(), nil, true
		}
	}

	return m, nil, false
}

// handleEnterKey opens the drill-down, or acts on the selected drill-down item.
func (m metricsModel) handleEnterKey() (metricsModel, tea.Cmd, bool) {
	if m.drillDown == nil {
		return m.enterDrillDown(), nil, true
	}

	state := m.drillDown
	if state.cursor < 0 || state.cursor >= len(state.items) {
		return m, nil, true
	}

	item := state.items[state.cursor]
	if item.pos != nil {
		return m, openEditor(*item.pos, m.rootDir), true
	}

	if item.stat != nil && len(item.stat.Positions) > 0 {
		state.toggleExpand()
		state.viewport.SetContent(state.renderContent())
		state.ensureCursorVisible()
	}

	if item.commitInfo != nil && len(item.commitInfo.Files) > 0 {
		state.toggleExpand()
		state.viewport.SetContent(state.renderContent())
		state.ensureCursorVisible()
	}

	return m, nil, true
}

// handleDrillDownNav applies cursor navigation and expansion inside the drill-down view.
func (m metricsModel) handleDrillDownNav(key tea.KeyPressMsg) metricsModel {
	switch key.String() {
	case "j", keyDown:
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
		return m
	}

	m.drillDown.viewport.SetContent(m.drillDown.renderContent())
	m.drillDown.ensureCursorVisible()

	return m
}
