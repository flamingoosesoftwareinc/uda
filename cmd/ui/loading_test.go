package ui

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/sebdah/goldie/v2"
)

type mockAnalyzer struct {
	name    string
	metrics []analyzer.Metrics
	err     error
	delay   time.Duration
}

func (m mockAnalyzer) Name() string { return m.name }

func (m mockAnalyzer) Analyze(_ context.Context, _ fs.FS) ([]analyzer.Metrics, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}

	return m.metrics, m.err
}

func testAnalyzers() []analyzer.Analyzer {
	return []analyzer.Analyzer{
		mockAnalyzer{
			name: "Go",
			metrics: []analyzer.Metrics{
				{
					Package: "example.com/project/main",
					Inward:  analyzer.PackageCouplingStats{},
					Outward: analyzer.PackageCouplingStats{
						"example.com/project/cmd": {
							"cmd.Execute": {Count: 1},
						},
					},
				},
				{
					Package: "example.com/project/cmd",
					Inward: analyzer.PackageCouplingStats{
						"example.com/project/main": {
							"cmd.Execute": {Count: 1},
						},
					},
					Outward: analyzer.PackageCouplingStats{},
				},
			},
		},
	}
}

func TestLoading_TransitionsToReady(t *testing.T) {
	m := newAppModel(
		context.Background(),
		testAnalyzers(),
		nil,
		DefaultSort(),
		nil,
		"",
		nil,
	)

	// Run Init to get initial commands
	var model tea.Model = m

	cmd := model.Init()

	// Execute the analysis command directly to simulate the transition
	// The batch returns multiple commands; execute them to find analysisResult
	if cmd != nil {
		msgs := executeBatchCmd(cmd)
		for _, msg := range msgs {
			model, _ = model.Update(msg)
		}
	}

	// Send window size
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestLoading_ShowsError(t *testing.T) {
	errAnalyzer := []analyzer.Analyzer{
		mockAnalyzer{
			name: "Go",
			err:  errors.New("failed to parse source files"),
		},
	}
	m := newAppModel(
		context.Background(),
		errAnalyzer,
		nil,
		DefaultSort(),
		nil,
		"",
		nil,
	)

	// Run Init and execute commands
	var model tea.Model = m

	cmd := model.Init()

	if cmd != nil {
		msgs := executeBatchCmd(cmd)
		for _, msg := range msgs {
			model, _ = model.Update(msg)
		}
	}

	// Send window size
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

// sortTestAnalyzers returns analyzers with enough packages to exercise sort order.
func sortTestAnalyzers() []analyzer.Analyzer {
	return []analyzer.Analyzer{
		mockAnalyzer{
			name: "Go",
			metrics: []analyzer.Metrics{
				{
					Package: "example.com/project/main",
					Inward:  analyzer.PackageCouplingStats{},
					Outward: analyzer.PackageCouplingStats{
						"example.com/project/cmd": {"cmd.Execute": {Count: 1}},
					},
				},
				{
					Package: "example.com/project/cmd",
					Inward: analyzer.PackageCouplingStats{
						"example.com/project/main": {"cmd.Execute": {Count: 1}},
					},
					Outward: analyzer.PackageCouplingStats{
						"example.com/project/pkg/foo": {"foo.DoFoo": {Count: 2}},
						"example.com/project/pkg/bar": {"bar.DoBar": {Count: 1}},
					},
				},
				{
					Package: "example.com/project/pkg/foo",
					Inward: analyzer.PackageCouplingStats{
						"example.com/project/cmd": {"foo.DoFoo": {Count: 2}},
					},
					Outward: analyzer.PackageCouplingStats{},
				},
				{
					Package: "example.com/project/pkg/bar",
					Inward: analyzer.PackageCouplingStats{
						"example.com/project/cmd": {"bar.DoBar": {Count: 1}},
					},
					Outward: analyzer.PackageCouplingStats{},
				},
			},
		},
	}
}

// loadAndReady creates an appModel, runs Init commands to transition to ready state,
// and sends a WindowSizeMsg.
func loadAndReady(t *testing.T, analyzers []analyzer.Analyzer) tea.Model {
	t.Helper()

	m := newAppModel(
		context.Background(),
		analyzers,
		nil,
		DefaultSort(),
		nil,
		"",
		nil,
	)

	var model tea.Model = m

	cmd := model.Init()
	if cmd != nil {
		for _, msg := range executeBatchCmd(cmd) {
			model, _ = model.Update(msg)
		}
	}

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	return model
}

func TestLoading_SortPackageAsc(t *testing.T) {
	model := loadAndReady(t, sortTestAnalyzers())
	// Press 'p' to sort by package ascending
	model, _ = model.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestLoading_SortPackageDesc(t *testing.T) {
	model := loadAndReady(t, sortTestAnalyzers())
	// Press 'p' twice: first → asc, second → desc
	model, _ = model.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	model, _ = model.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

// executeBatchCmd executes a tea.Cmd and collects all resulting messages.
// It handles batch commands by recursively executing sub-commands.
func executeBatchCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}

	msg := cmd()
	if msg == nil {
		return nil
	}

	// Check if it's a batch message (tea.BatchMsg)
	if batch, ok := msg.(tea.BatchMsg); ok {
		var msgs []tea.Msg
		for _, subCmd := range batch {
			msgs = append(msgs, executeBatchCmd(subCmd)...)
		}

		return msgs
	}

	return []tea.Msg{msg}
}
