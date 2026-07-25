package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/history"
	"github.com/sebdah/goldie/v2"
)

// pkgSummary builds a single-package metrics summary for test fixtures.
func pkgSummary(pkg string, inward, outward int, instability float64) analyzer.MetricsSummary {
	return analyzer.MetricsSummary{
		Package:     pkg,
		Inward:      inward,
		Outward:     outward,
		Instability: instability,
	}
}

// goCommit builds a Go-only commit metrics fixture.
func goCommit(
	sha, msg string,
	timestamp time.Time,
	pkgs ...analyzer.MetricsSummary,
) history.CommitMetrics {
	return history.CommitMetrics{
		SHA:       sha,
		Timestamp: timestamp,
		Message:   msg,
		Metrics: []analyzer.LanguageMetricsSummary{
			{Language: "Go", Metrics: pkgs},
		},
	}
}

// goTSCommit builds a Go + TypeScript commit metrics fixture.
func goTSCommit(
	sha, msg string,
	timestamp time.Time,
	goPkgs, tsPkgs []analyzer.MetricsSummary,
) history.CommitMetrics {
	return history.CommitMetrics{
		SHA:       sha,
		Timestamp: timestamp,
		Message:   msg,
		Metrics: []analyzer.LanguageMetricsSummary{
			{Language: "Go", Metrics: goPkgs},
			{Language: "TypeScript", Metrics: tsPkgs},
		},
	}
}

func historyTestCommits() []history.CommitMetrics {
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	return []history.CommitMetrics{
		goCommit("abc1234abc1234abc1234abc1234abc1234abc12", "Initial commit", baseTime,
			pkgSummary("example.com/project/main", 0, 2, 1.0),
			pkgSummary("example.com/project/pkg/foo", 1, 0, 0.0),
		),
		goCommit(
			"def5678def5678def5678def5678def5678def56",
			"Add feature",
			baseTime.Add(24*time.Hour),
			pkgSummary("example.com/project/main", 0, 3, 1.0),
			pkgSummary("example.com/project/pkg/foo", 2, 1, 0.333),
			pkgSummary("example.com/project/pkg/bar", 1, 0, 0.0),
		),
		goCommit("ghi9012ghi9012ghi9012ghi9012ghi9012ghi90", "Refactor", baseTime.Add(48*time.Hour),
			pkgSummary("example.com/project/main", 1, 2, 0.667),
			pkgSummary("example.com/project/pkg/foo", 2, 2, 0.5),
			pkgSummary("example.com/project/pkg/bar", 1, 1, 0.5),
		),
	}
}

func multiLangHistoryCommits() []history.CommitMetrics {
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	return []history.CommitMetrics{
		goTSCommit("abc1234abc1234abc1234abc1234abc1234abc12", "Initial commit", baseTime,
			[]analyzer.MetricsSummary{pkgSummary("example.com/project/main", 0, 2, 1.0)},
			[]analyzer.MetricsSummary{pkgSummary("my-app/src/App", 0, 1, 1.0)},
		),
		goTSCommit(
			"def5678def5678def5678def5678def5678def56",
			"Add feature",
			baseTime.Add(24*time.Hour),
			[]analyzer.MetricsSummary{pkgSummary("example.com/project/main", 1, 2, 0.667)},
			[]analyzer.MetricsSummary{pkgSummary("my-app/src/App", 1, 2, 0.667)},
		),
	}
}

func TestHistory_Render(t *testing.T) {
	m := newHistoryModel(historyTestCommits(), nil, nil)

	var model tea.Model = &m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestHistory_Navigate(t *testing.T) {
	m := newHistoryModel(historyTestCommits(), nil, nil)

	var model tea.Model = &m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	model, _ = model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestHistory_MarkerMove(t *testing.T) {
	m := newHistoryModel(historyTestCommits(), nil, nil)

	var model tea.Model = &m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	model, _ = model.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestHistory_DrillDown(t *testing.T) {
	m := newHistoryModel(historyTestCommits(), nil, nil)

	var model tea.Model = &m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	model, _ = model.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestHistory_LangSwitch(t *testing.T) {
	m := newHistoryModel(multiLangHistoryCommits(), nil, nil)

	var model tea.Model = &m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	model, _ = model.Update(tea.KeyPressMsg{Code: ']', Text: "]"})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestHistory_EscFromDrillDown(t *testing.T) {
	m := newHistoryModel(historyTestCommits(), nil, nil)

	var model tea.Model = &m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestHistory_Zoom(t *testing.T) {
	m := newHistoryModel(historyTestCommits(), nil, nil)

	var model tea.Model = &m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	model, _ = model.Update(tea.KeyPressMsg{Code: '+', Text: "+"})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}
