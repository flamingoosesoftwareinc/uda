package ui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/history"
)

// MetricsFetcher fetches full metrics for a commit SHA.
// Returns metrics grouped by language, or an error.
type MetricsFetcher func(ctx context.Context, sha string) ([]history.LanguageMetrics, error)

// WorkspaceCheckout checks out a commit in the workspace and returns the workspace path.
// This is used to open files from a specific commit's snapshot in the editor.
type WorkspaceCheckout func(sha string) (workspacePath string, err error)

// RunHistoryInteractive launches the interactive TUI for history metrics.
func RunHistoryInteractive(
	commits []history.CommitMetrics,
	fetcher MetricsFetcher,
	workspaceCheckout WorkspaceCheckout,
) error {
	m := newHistoryModel(commits, fetcher, workspaceCheckout)
	p := tea.NewProgram(&m)
	_, err := p.Run()

	return err
}

// focusLevel represents the current navigation focus level.
type focusLevel int

const (
	focusList  focusLevel = iota // Scrolling through cards, Tab/arrows work on selected card
	focusDrill                   // In drill-down diff view
)

const (
	chartCardHMargin    = 4 // horizontal cells reserved around a chart card
	historyChromeHeight = 4 // vertical rows reserved for tab bar + help + borders
	cardScrollBuffer    = 2 // extra cards rendered above/below the viewport for smooth scrolling
)

type historyModel struct {
	timeSeries        []LanguageTimeSeries
	languages         []string
	activeTab         int
	cards             []chartCard
	selectedCard      int
	focus             focusLevel
	viewport          viewport.Model
	drillDown         *historyDrillDownState
	metricsFetcher    MetricsFetcher
	workspaceCheckout WorkspaceCheckout
	width             int
	height            int
	showHelp          bool
}

func newHistoryModel(
	commits []history.CommitMetrics,
	fetcher MetricsFetcher,
	workspaceCheckout WorkspaceCheckout,
) historyModel {
	timeSeries := TransformToTimeSeries(commits)
	languages := GetLanguages(commits)

	model := historyModel{
		timeSeries:        timeSeries,
		languages:         languages,
		activeTab:         0,
		width:             defaultTermWidth,
		height:            defaultTermHeight,
		focus:             focusList,
		metricsFetcher:    fetcher,
		workspaceCheckout: workspaceCheckout,
	}

	model.rebuildCards()

	return model
}

func (m *historyModel) Init() tea.Cmd {
	return nil
}

func (m *historyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport = viewport.New(
			viewport.WithWidth(m.width),
			viewport.WithHeight(m.contentHeight()),
		)
		m.resizeCards()
		m.viewport.SetContent(m.renderCards())

		return m, nil

	case tea.KeyPressMsg:
		// Help overlay: dismiss on any key
		if m.showHelp {
			m.showHelp = false

			return m, nil
		}

		// Global keys
		switch msg.String() {
		case keyCtrlC:
			return m, tea.Quit
		case "?":
			if m.drillDown == nil {
				m.showHelp = true

				return m, nil
			}
		case "q":
			if m.drillDown != nil {
				m.drillDown = nil
				m.focus = focusList

				return m, nil
			}

			return m, tea.Quit
		case "esc":
			if m.drillDown != nil {
				m.drillDown = nil
				m.focus = focusList

				return m, nil
			}

			return m, nil
		}

		// Drill-down mode
		if m.drillDown != nil {
			return m.updateDrillDown(msg)
		}

		// Main list mode - selected card is auto-focused
		return m.updateFocusList(msg)
	}

	return m, nil
}

func (m *historyModel) View() tea.View {
	var b strings.Builder

	b.WriteString(m.renderTabBar())
	b.WriteString("\n")

	if m.drillDown != nil {
		b.WriteString(m.drillDown.viewport.View())
	} else {
		b.WriteString(m.viewport.View())
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

func (m *historyModel) rebuildCards() {
	var series []PackageTimeSeries
	if m.activeTab < len(m.timeSeries) {
		series = m.timeSeries[m.activeTab].Series
	}

	cards := make([]chartCard, len(series))
	for i, s := range series {
		cards[i] = newChartCard(s.Package, s.DataPoints, m.width-chartCardHMargin)
		cards[i].changeFreq = s.ChangeFreq
		cards[i].hotspotScore = s.HotspotScore
	}

	m.cards = cards

	m.selectedCard = 0
	if len(cards) > 0 {
		m.cards[0].Focus()
	}

	m.viewport = viewport.New(viewport.WithWidth(m.width), viewport.WithHeight(m.contentHeight()))
	m.viewport.SetContent(m.renderCards())
}

func (m *historyModel) contentHeight() int {
	return m.height - historyChromeHeight
}

func (m *historyModel) updateFocusList(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", keyDown:
		m.selectCardDelta(1)
	case "k", "up":
		m.selectCardDelta(-1)
	case "shift+tab", "[":
		m.switchLanguageTab(-1)
	case "]":
		m.switchLanguageTab(1)
	case "left", "h":
		m.moveSelectedMarker(-1)
	case "right", "l":
		m.moveSelectedMarker(1)
	case "+", "=":
		m.zoomSelected(true)
	case "-", "_":
		m.zoomSelected(false)
	case keyEnter:
		m.enterDrillDown()
	case "g":
		m.selectFirstCard()
	case "G":
		m.selectLastCard()
	}

	return m, nil
}

// selectCardDelta moves the card selection by delta (bounded), refreshing the view.
func (m *historyModel) selectCardDelta(delta int) {
	next := m.selectedCard + delta
	if next < 0 || next >= len(m.cards) {
		return
	}

	m.cards[m.selectedCard].Blur()
	m.selectedCard = next
	m.cards[m.selectedCard].Focus()
	m.viewport.SetContent(m.renderCards())
	m.ensureCardVisible()
}

// switchLanguageTab moves the active language tab by delta (wrapping) and rebuilds cards.
func (m *historyModel) switchLanguageTab(delta int) {
	if len(m.languages) <= 1 {
		return
	}

	m.activeTab = (m.activeTab + delta + len(m.languages)) % len(m.languages)
	m.rebuildCards()
}

// moveSelectedMarker shifts the selected card's marker by delta and rebuilds its charts.
func (m *historyModel) moveSelectedMarker(delta int) {
	if len(m.cards) == 0 {
		return
	}

	card := &m.cards[m.selectedCard]
	card.moveMarker(delta)
	card.rebuildCharts()
	m.viewport.SetContent(m.renderCards())
}

// zoomSelected zooms the selected card in or out and refreshes the view.
func (m *historyModel) zoomSelected(zoomingIn bool) {
	if len(m.cards) == 0 {
		return
	}

	card := &m.cards[m.selectedCard]
	if zoomingIn {
		card.zoomIn()
	} else {
		card.zoomOut()
	}

	m.viewport.SetContent(m.renderCards())
}

// selectFirstCard focuses the first card and scrolls to the top.
func (m *historyModel) selectFirstCard() {
	if len(m.cards) == 0 {
		return
	}

	m.cards[m.selectedCard].Blur()
	m.selectedCard = 0
	m.cards[m.selectedCard].Focus()
	m.viewport.SetContent(m.renderCards())
	m.viewport.GotoTop()
}

// selectLastCard focuses the last card and scrolls to the bottom.
func (m *historyModel) selectLastCard() {
	if len(m.cards) == 0 {
		return
	}

	m.cards[m.selectedCard].Blur()
	m.selectedCard = len(m.cards) - 1
	m.cards[m.selectedCard].Focus()
	m.viewport.SetContent(m.renderCards())
	m.viewport.GotoBottom()
}

func (m *historyModel) updateDrillDown(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.drillDown == nil {
		return m, nil
	}

	state := m.drillDown

	switch msg.String() {
	case "j", keyDown:
		state.moveCursor(1)
		state.viewport.SetContent(state.renderContent())
	case "k", "up":
		state.moveCursor(-1)
		state.viewport.SetContent(state.renderContent())
	case "g":
		state.moveCursorToFirst()
		state.viewport.SetContent(state.renderContent())
	case "G":
		state.moveCursorToLast()
		state.viewport.SetContent(state.renderContent())
	case keyEnter:
		// If cursor is on a position item, open in editor
		if state.cursor >= 0 && state.cursor < len(state.items) {
			item := state.items[state.cursor]
			if item.pos != nil && item.sha != "" && m.workspaceCheckout != nil {
				// Checkout the workspace to the correct commit, then open editor
				workspacePath, err := m.workspaceCheckout(item.sha)
				if err == nil {
					return m, openEditor(*item.pos, workspacePath)
				}
				// If checkout fails, fall through (no editor opened)
			}
			// Otherwise toggle expand if it's an expandable symbol
			if item.stat != nil && len(item.stat.Positions) > 0 {
				state.toggleExpand()
				state.viewport.SetContent(state.renderContent())
			}
		}
	}

	return m, nil
}

func (m *historyModel) enterDrillDown() {
	if len(m.cards) == 0 {
		return
	}

	card := &m.cards[m.selectedCard]
	current := card.MarkerDataPoint()
	previous := card.MarkerPreviousDataPoint()

	if current == nil {
		return
	}

	// Get current language for filtering metrics
	var lang string
	if m.activeTab < len(m.languages) {
		lang = m.languages[m.activeTab]
	}

	// Fetch full metrics for current and previous commits
	var currentMetrics, previousMetrics *analyzer.Metrics

	if m.metricsFetcher != nil {
		ctx := context.Background()

		// Fetch current commit metrics
		currentLangMetrics, err := m.metricsFetcher(ctx, current.SHA)
		if err == nil {
			currentMetrics = findPackageMetrics(currentLangMetrics, lang, card.pkgName)
		}

		// Fetch previous commit metrics if available
		if previous != nil {
			prevLangMetrics, err := m.metricsFetcher(ctx, previous.SHA)
			if err == nil {
				previousMetrics = findPackageMetrics(prevLangMetrics, lang, card.pkgName)
			}
		}
	}

	state := newHistoryDrillDownState(
		card.pkgName,
		current,
		previous,
		currentMetrics,
		previousMetrics,
		m.width,
		m.contentHeight(),
	)
	state.changeFreq = card.changeFreq
	state.hotspotScore = card.hotspotScore
	m.drillDown = state
	m.focus = focusDrill
}

// findPackageMetrics finds the full metrics for a specific package from language metrics.
func findPackageMetrics(
	langMetrics []history.LanguageMetrics,
	lang, pkgName string,
) *analyzer.Metrics {
	for _, langMetric := range langMetrics {
		if langMetric.Language != lang {
			continue
		}

		for i := range langMetric.Metrics {
			if string(langMetric.Metrics[i].Package) == pkgName {
				return &langMetric.Metrics[i]
			}
		}
	}

	return nil
}

func (m *historyModel) resizeCards() {
	for i := range m.cards {
		m.cards[i].Resize(m.width - chartCardHMargin)
	}
}

func (m *historyModel) ensureCardVisible() {
	if len(m.cards) == 0 {
		return
	}

	cardHeight := m.cards[0].height + 1

	cardTop := m.selectedCard * cardHeight
	cardBottom := cardTop + cardHeight

	if cardTop < m.viewport.YOffset() {
		m.viewport.SetYOffset(cardTop)
	} else if cardBottom > m.viewport.YOffset()+m.viewport.Height() {
		m.viewport.SetYOffset(cardBottom - m.viewport.Height())
	}
}

// visibleCardRange returns the start (inclusive) and end (exclusive) indices
// of cards that are currently visible in the viewport (plus a small buffer).
func (m *historyModel) visibleCardRange() (int, int) {
	if len(m.cards) == 0 {
		return 0, 0
	}

	cardHeight := m.cards[0].height + 1 // +1 for separator
	if cardHeight <= 0 {
		cardHeight = 1
	}

	vpTop := m.viewport.YOffset()
	vpBottom := vpTop + m.viewport.Height()

	first := vpTop / cardHeight
	last := vpBottom/cardHeight + 1

	// Add buffer of cards above and below for smooth scrolling
	first = max(0, first-cardScrollBuffer)
	last = min(len(m.cards), last+cardScrollBuffer)

	return first, last
}

func (m *historyModel) renderCards() string {
	first, last := m.visibleCardRange()

	var b strings.Builder

	for i := range m.cards {
		if i >= first && i < last {
			// Visible: ensure charts are built and render fully
			m.cards[i].ensureCharts()
			b.WriteString(m.cards[i].View())
		} else {
			// Off-screen: release heavy chart models, render lightweight placeholder
			m.cards[i].releaseCharts()
			b.WriteString(m.cards[i].ViewPlaceholder())
		}

		if i < len(m.cards)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m *historyModel) renderTabBar() string {
	if len(m.languages) == 0 {
		return ""
	}

	activeStyle := lipgloss.NewStyle().
		Bold(true).
		Reverse(true).
		Padding(0, tabHorizontalPadding)

	inactiveStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorInactiveFg)).
		Background(lipgloss.Color(colorInactiveBg)).
		Padding(0, tabHorizontalPadding)

	var tabs []string

	for i, lang := range m.languages {
		if i == m.activeTab {
			tabs = append(tabs, activeStyle.Render(lang))
		} else {
			tabs = append(tabs, inactiveStyle.Render(lang))
		}
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}

func (m *historyModel) renderHelp() string {
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorDim))

	var help string

	if m.drillDown != nil {
		help = "j/k: nav | enter: expand | esc: back | q: quit"
	} else {
		help = "j/k: scroll | h/l: marker | +/-: zoom | enter: details"
		if len(m.languages) > 1 {
			help += " | [/]: lang"
		}

		help += " | ?: help | q: quit"
	}

	// Truncate if too long
	if len(help) > m.width-2 {
		help = help[:m.width-5] + "..."
	}

	return helpStyle.Render(help)
}
