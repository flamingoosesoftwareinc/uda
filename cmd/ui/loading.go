package ui

import (
	"context"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
)

const (
	spinnerFPS           = 6
	sayingRotateInterval = 2 * time.Second

	// Layout: fixed-width columns and their combined widths.
	fixedColumnsWidth   = 42 // INWARD + OUTWARD + INSTABILITY columns (with padding)
	hotspotColumnsWidth = 21 // CHNG FREQ + HOTSPOT columns
	inwardColWidth      = 10
	outwardColWidth     = 11
	instabilityColWidth = 15
	changeFreqColWidth  = 11
	hotspotColWidth     = 10

	// Vertical layout.
	tableChromeHeight = 3 // tab bar + header + border rows reserved above the body
	overlayHeaderRows = 2 // rows consumed by the table header inside the overlay
	minOverlayHeight  = 3 // smallest overlay body height

	// Column-set shapes and bounds.
	minPackageWidth    = 10 // floor for the PACKAGE column width
	baseColumnCount    = 4  // PACKAGE + INWARD + OUTWARD + INSTABILITY
	hotspotColumnCount = 6  // base columns plus CHNG FREQ + HOTSPOT
)

var blockSpinner = spinner.Spinner{
	Frames: []string{"▖", "▘", "▝", "▗"},
	FPS:    time.Second / spinnerFPS,
}

var loadingSayings = []string{
	"Traversing the dependency graph...",
	"Counting coupling connections...",
	"Measuring package instability...",
	"Analyzing import hierarchies...",
	"Mapping inward dependencies...",
	"Resolving outward references...",
	"Calculating structural metrics...",
}

// PostAnalysisFunc is an optional callback invoked after analysis completes
// but before the results are sent to the UI. It enriches the groups in-place
// (e.g. with hotspot data).
type PostAnalysisFunc func(groups []LanguageMetrics, analyzers []analyzer.Analyzer) error

// Messages for the loading flow.
type analysisResult struct {
	groups []LanguageMetrics
}

type analysisError struct {
	err error
}

type rotateSayingMsg struct{}

func rotateSayingCmd() tea.Msg {
	time.Sleep(sayingRotateInterval)

	return rotateSayingMsg{}
}

// runAnalysis returns a tea.Cmd that runs all analyzers, optionally enriches
// the results via enricher, and sends the result.
func runAnalysis(
	ctx context.Context,
	analyzers []analyzer.Analyzer,
	dir fs.FS,
	enricher PostAnalysisFunc,
) tea.Cmd {
	return func() tea.Msg {
		var groups []LanguageMetrics

		for _, langAnalyzer := range analyzers {
			metrics, err := langAnalyzer.Analyze(ctx, dir)
			if err != nil {
				return analysisError{err: err}
			}

			groups = append(groups, LanguageMetrics{
				Language: langAnalyzer.Name(),
				Metrics:  metrics,
			})
		}

		if enricher != nil {
			if err := enricher(groups, analyzers); err != nil {
				return analysisError{err: err}
			}
		}

		return analysisResult{groups: groups}
	}
}

// loadingModel displays a spinner and rotating sayings below the table header.
type loadingModel struct {
	spinner     spinner.Model
	sayingIdx   int
	width       int
	height      int
	languages   []string
	hasHotspots bool
}

func newLoadingModel(languages []string, hasHotspots bool) loadingModel {
	s := spinner.New()
	s.Spinner = blockSpinner
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(colorSpinner))

	return loadingModel{
		spinner:     s,
		width:       defaultTermWidth,
		height:      defaultTermHeight,
		languages:   languages,
		hasHotspots: hasHotspots,
	}
}

func (m loadingModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m loadingModel) Update(msg tea.Msg) (loadingModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd

		m.spinner, cmd = m.spinner.Update(msg)

		return m, cmd
	case rotateSayingMsg:
		m.sayingIdx = (m.sayingIdx + 1) % len(loadingSayings)

		return m, rotateSayingCmd
	}

	return m, nil
}

func (m loadingModel) View() string {
	var b strings.Builder

	// Tab bar (same style as metricsModel).
	if len(m.languages) > 0 {
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
			if i == 0 {
				tabs = append(tabs, activeStyle.Render(lang))
			} else {
				tabs = append(tabs, inactiveStyle.Render(lang))
			}
		}

		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, tabs...))
		b.WriteString("\n")
	}

	// Render the real table header at the top (empty table, header only).
	t := m.buildEmptyTable()
	tableView := t.View()

	// Spinner + saying overlayed in the table body area.
	spinnerLine := m.spinner.View() + "  Analyzing..."
	spinnerStyle := lipgloss.NewStyle().Bold(true)

	// Fixed width prevents layout shift when sayings rotate.
	sayingWidth := 0
	for _, s := range loadingSayings {
		if w := len(fmt.Sprintf("%q", s)); w > sayingWidth {
			sayingWidth = w
		}
	}

	saying := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorPosition)).
		Italic(true).
		Width(sayingWidth).
		Align(lipgloss.Center).
		Render(fmt.Sprintf("%q", loadingSayings[m.sayingIdx]))

	// Height for the overlay area: total height minus header (tab bar + table header + border).
	overlayHeight := max(m.tableHeight()-overlayHeaderRows, minOverlayHeight)

	overlay := lipgloss.Place(m.width, overlayHeight, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Center,
			spinnerStyle.Render(spinnerLine),
			"",
			saying,
		),
	)

	b.WriteString(tableView)
	b.WriteString("\n")
	b.WriteString(overlay)

	return b.String()
}

func (m loadingModel) buildEmptyTable() table.Model {
	pkgWidth := m.width - fixedColumnsWidth

	cols := []table.Column{
		{Title: "PACKAGE", Width: pkgWidth},
		{Title: "INWARD", Width: inwardColWidth},
		{Title: "OUTWARD", Width: outwardColWidth},
		{Title: "INSTABILITY", Width: instabilityColWidth},
	}
	if m.hasHotspots {
		cols[0].Width = m.width - fixedColumnsWidth - hotspotColumnsWidth
		cols = append(cols,
			table.Column{Title: "CHNG FREQ", Width: changeFreqColWidth},
			table.Column{Title: "HOTSPOT", Width: hotspotColWidth},
		)
	}

	s := table.DefaultStyles()
	s.Header = s.Header.
		Bold(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(lipgloss.Color(colorBorder))
	s.Selected = s.Selected.
		Foreground(lipgloss.NoColor{}).
		Background(lipgloss.NoColor{}).
		Bold(false)

	t := table.New(
		table.WithColumns(cols),
		table.WithRows(nil),
		table.WithHeight(1),
		table.WithStyles(s),
	)

	return t
}

// tableHeight mirrors metricsModel.tableHeight for consistent layout.
func (m loadingModel) tableHeight() int {
	h := max(m.height-tableChromeHeight, 1)

	return h
}

// appState tracks which phase the app is in.
type appState int

const (
	stateLoading appState = iota
	stateReady
	stateError
)

// appModel wraps loadingModel and metricsModel, transitioning between them.
type appModel struct {
	state   appState
	loading loadingModel
	metrics metricsModel
	errMsg  string

	// Stored for building metricsModel after analysis completes.
	sortCriteria []SortCriterion
	filter       *regexp.Regexp
	rootDir      string
	hasHotspots  bool
	analysisCmd  tea.Cmd
}

func newAppModel(
	ctx context.Context,
	analyzers []analyzer.Analyzer,
	dir fs.FS,
	criteria []SortCriterion,
	filter *regexp.Regexp,
	rootDir string,
	enricher PostAnalysisFunc,
) appModel {
	hasHotspots := enricher != nil

	languages := make([]string, len(analyzers))
	for i, a := range analyzers {
		languages[i] = a.Name()
	}

	return appModel{
		state:        stateLoading,
		loading:      newLoadingModel(languages, hasHotspots),
		sortCriteria: criteria,
		filter:       filter,
		rootDir:      rootDir,
		hasHotspots:  hasHotspots,
		analysisCmd:  runAnalysis(ctx, analyzers, dir, enricher),
	}
}

func (m appModel) Init() tea.Cmd {
	return tea.Batch(
		m.loading.Init(),
		rotateSayingCmd,
		m.analysisCmd,
	)
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		if m.state == stateError && (msg.String() == "q" || msg.String() == "enter") {
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		switch m.state {
		case stateLoading:
			var cmd tea.Cmd

			m.loading, cmd = m.loading.Update(msg)

			return m, cmd
		case stateReady:
			updated, cmd := m.metrics.Update(msg)
			if mm, ok := updated.(metricsModel); ok {
				m.metrics = mm
			}

			return m, cmd
		case stateError:
			// no resize handling once the error pane is shown
		}

		return m, nil

	case analysisResult:
		m.state = stateReady
		m.metrics = newMetricsModel(msg.groups, m.sortCriteria, m.filter, m.rootDir, m.hasHotspots)
		m.metrics.width = m.loading.width
		m.metrics.height = m.loading.height
		m.metrics = m.metrics.resizeAll()

		return m, m.metrics.Init()

	case analysisError:
		m.state = stateError
		m.errMsg = msg.err.Error()

		return m, nil
	}

	switch m.state {
	case stateLoading:
		var cmd tea.Cmd

		m.loading, cmd = m.loading.Update(msg)

		return m, cmd
	case stateReady:
		updated, cmd := m.metrics.Update(msg)
		if mm, ok := updated.(metricsModel); ok {
			m.metrics = mm
		}

		return m, cmd
	case stateError:
		// error pane is terminal — no further updates routed
	}

	return m, nil
}

func (m appModel) View() tea.View {
	var content string

	switch m.state {
	case stateLoading:
		content = m.loading.View()
	case stateReady:
		// metricsModel.View() returns tea.View; extract content string
		return m.metrics.View()
	case stateError:
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorError)).Bold(true)
		hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorDim))
		c := lipgloss.JoinVertical(lipgloss.Center,
			errStyle.Render("Error: "+m.errMsg),
			"",
			hintStyle.Render("press q to quit"),
		)
		content = lipgloss.Place(
			m.loading.width, m.loading.height,
			lipgloss.Center, lipgloss.Center,
			c,
		)
	}

	v := tea.NewView(content)
	v.AltScreen = true

	return v
}

// RunInteractiveWithLoading launches the interactive TUI with a loading screen
// that displays while the analyzers run. The enricher callback, if non-nil, is
// called after analysis to enrich groups with additional data (e.g. hotspots).
func RunInteractiveWithLoading(
	ctx context.Context,
	analyzers []analyzer.Analyzer,
	dir fs.FS,
	criteria []SortCriterion,
	filter *regexp.Regexp,
	rootDir string,
	enricher PostAnalysisFunc,
) error {
	m := newAppModel(ctx, analyzers, dir, criteria, filter, rootDir, enricher)
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return err
	}

	if app, ok := finalModel.(appModel); ok && app.state == stateError {
		return fmt.Errorf("%s", app.errMsg)
	}

	return nil
}
