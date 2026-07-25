package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/diff"
	"github.com/sebdah/goldie/v2"
)

// reviewTestResult returns a ReviewResult with mixed changes
// (new dependency, removed dependency, changed dependency, new/removed packages).
func reviewTestResult() ReviewResult {
	return ReviewResult{
		BaseLabel: "abc1234",
		HeadLabel: "def5678",
		Diffs: []diff.PackageDiff{
			{
				Package:  "internal/cache",
				DiffType: diff.Added,
				OutwardDiff: []diff.DependencyDiff{
					{
						Package:  "internal/db",
						DiffType: diff.Added,
						Symbols: []diff.SymbolDiff{
							{Name: "db.Pool", DiffType: diff.Added, CurrCount: 3},
						},
					},
				},
			},
			{
				Package:  "internal/core",
				DiffType: diff.Unchanged,
				OutwardDiff: []diff.DependencyDiff{
					{
						Package:  "internal/config",
						DiffType: diff.Unchanged,
						Symbols: []diff.SymbolDiff{
							{
								Name:      "config.Timeout",
								DiffType:  diff.Added,
								CurrCount: 1,
								Positions: []diff.PositionDiff{
									{
										Position: analyzer.Position{
											File: "core/server.go", Line: 30,
											ColStart: 5, ColEnd: 20,
										},
										DiffType: diff.Added,
									},
								},
							},
							{
								Name:      "config.Debug",
								DiffType:  diff.Removed,
								PrevCount: 1,
								Positions: []diff.PositionDiff{
									{
										Position: analyzer.Position{
											File: "core/app.go", Line: 18,
											ColStart: 2, ColEnd: 15,
										},
										DiffType: diff.Removed,
									},
								},
							},
						},
					},
					{
						Package:  "internal/http",
						DiffType: diff.Added,
						Symbols: []diff.SymbolDiff{
							{
								Name:      "http.Client",
								DiffType:  diff.Added,
								CurrCount: 1,
								Positions: []diff.PositionDiff{
									{
										Position: analyzer.Position{
											File: "core/server.go", Line: 42,
											ColStart: 8, ColEnd: 22,
										},
										DiffType: diff.Added,
									},
								},
							},
						},
					},
				},
			},
			{
				Package:  "internal/oldutil",
				DiffType: diff.Removed,
			},
		},
		PrevAll: []analyzer.Metrics{
			{
				Package: "internal/core",
				Inward: analyzer.PackageCouplingStats{
					"internal/api":     {},
					"internal/svc":     {},
					"internal/handler": {},
					"internal/job":     {},
					"internal/worker":  {},
				},
				Outward: analyzer.PackageCouplingStats{
					"internal/config": {},
					"internal/db":     {},
				},
			},
			{
				Package: "internal/oldutil",
				Outward: analyzer.PackageCouplingStats{"internal/core": {}},
			},
		},
		CurrAll: []analyzer.Metrics{
			{
				Package: "internal/cache",
				Outward: analyzer.PackageCouplingStats{
					"internal/db":     {},
					"internal/config": {},
				},
			},
			{
				Package: "internal/core",
				Inward: analyzer.PackageCouplingStats{
					"internal/api":     {},
					"internal/svc":     {},
					"internal/handler": {},
					"internal/job":     {},
					"internal/worker":  {},
				},
				Outward: analyzer.PackageCouplingStats{
					"internal/config": {},
					"internal/db":     {},
					"internal/http":   {},
					"internal/log":    {},
				},
			},
		},
	}
}

// reviewTestResultNoChanges returns a result with no architectural changes.
func reviewTestResultNoChanges() ReviewResult {
	return ReviewResult{
		BaseLabel: "abc1234",
		HeadLabel: "def5678",
		Diffs: []diff.PackageDiff{
			{Package: "internal/core", DiffType: diff.Unchanged},
			{Package: "internal/db", DiffType: diff.Unchanged},
		},
		PrevAll: []analyzer.Metrics{
			{Package: "internal/core", Outward: analyzer.PackageCouplingStats{"internal/db": {}}},
			{Package: "internal/db"},
		},
		CurrAll: []analyzer.Metrics{
			{Package: "internal/core", Outward: analyzer.PackageCouplingStats{"internal/db": {}}},
			{Package: "internal/db"},
		},
	}
}

func TestReviewInteractive_List(t *testing.T) {
	m := newReviewModel(reviewTestResult())

	var model tea.Model = m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestReviewInteractive_NoChanges(t *testing.T) {
	m := newReviewModel(reviewTestResultNoChanges())

	var model tea.Model = m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestReviewInteractive_Navigate(t *testing.T) {
	m := newReviewModel(reviewTestResult())

	var model tea.Model = m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model, _ = model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestReviewInteractive_DrillDown(t *testing.T) {
	m := newReviewModel(reviewTestResult())

	var model tea.Model = m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	// Navigate to internal/core (second row) which has dependency changes
	model, _ = model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	// Enter drill-down
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestReviewInteractive_DrillDownExpand(t *testing.T) {
	m := newReviewModel(reviewTestResult())

	var model tea.Model = m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	// Navigate to internal/core (second row)
	model, _ = model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	// Enter drill-down
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// Expand first symbol (space)
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestReviewInteractive_DrillDownBack(t *testing.T) {
	m := newReviewModel(reviewTestResult())

	var model tea.Model = m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	// Enter drill-down
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// Back to list
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}
