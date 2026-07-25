package ui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/sebdah/goldie/v2"
)

func singleLanguageGroups() []LanguageMetrics {
	return []LanguageMetrics{
		{
			Language: "Go",
			Metrics: []analyzer.Metrics{
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
					Outward: analyzer.PackageCouplingStats{
						"example.com/project/pkg/foo": {
							"foo.DoFoo": {Count: 2},
						},
						"example.com/project/pkg/bar": {
							"bar.DoBar": {Count: 1},
						},
					},
				},
				{
					Package: "example.com/project/pkg/foo",
					Inward: analyzer.PackageCouplingStats{
						"example.com/project/cmd": {
							"foo.DoFoo": {Count: 2},
						},
					},
					Outward: analyzer.PackageCouplingStats{},
				},
				{
					Package: "example.com/project/pkg/bar",
					Inward: analyzer.PackageCouplingStats{
						"example.com/project/cmd": {
							"bar.DoBar": {Count: 1},
						},
					},
					Outward: analyzer.PackageCouplingStats{},
				},
			},
		},
	}
}

// singleLanguageGroupsWithPositions returns test data that includes position
// information so the drill-down expand feature can be exercised.
func singleLanguageGroupsWithPositions() []LanguageMetrics {
	return []LanguageMetrics{
		{
			Language: "Go",
			Metrics: []analyzer.Metrics{
				{
					Package: "example.com/project/main",
					Inward:  analyzer.PackageCouplingStats{},
					Outward: analyzer.PackageCouplingStats{
						"example.com/project/cmd": {
							"cmd.Execute": {
								Count: 2,
								Positions: []analyzer.Position{
									{File: "main.go", Line: 10, ColStart: 2, ColEnd: 15},
									{File: "main.go", Line: 22, ColStart: 3, ColEnd: 16},
								},
							},
						},
						"fmt": {
							"fmt.Println": {
								Count: 3,
								Positions: []analyzer.Position{
									{File: "main.go", Line: 8, ColStart: 2, ColEnd: 13},
									{File: "main.go", Line: 14, ColStart: 2, ColEnd: 13},
									{File: "main.go", Line: 20, ColStart: 2, ColEnd: 13},
								},
							},
						},
					},
				},
				{
					Package: "example.com/project/cmd",
					Inward: analyzer.PackageCouplingStats{
						"example.com/project/main": {
							"cmd.Execute": {
								Count: 2,
								Positions: []analyzer.Position{
									{File: "main.go", Line: 10, ColStart: 2, ColEnd: 15},
									{File: "main.go", Line: 22, ColStart: 3, ColEnd: 16},
								},
							},
						},
					},
					Outward: analyzer.PackageCouplingStats{},
				},
			},
		},
	}
}

func multiLanguageGroups() []LanguageMetrics {
	return []LanguageMetrics{
		{
			Language: "Go",
			Metrics: []analyzer.Metrics{
				{
					Package: "example.com/project/main",
					Inward:  analyzer.PackageCouplingStats{},
					Outward: analyzer.PackageCouplingStats{
						"example.com/project/cmd": {
							"cmd.Execute": {Count: 1},
						},
					},
				},
			},
		},
		{
			Language: "TypeScript",
			Metrics: []analyzer.Metrics{
				{
					Package: "my-app/src/App",
					Inward:  analyzer.PackageCouplingStats{},
					Outward: analyzer.PackageCouplingStats{
						"my-app/src/components/Button": {
							"Button.Button": {Count: 1},
						},
					},
				},
				{
					Package: "my-app/src/components/Button",
					Inward: analyzer.PackageCouplingStats{
						"my-app/src/App": {
							"Button.Button": {Count: 1},
						},
					},
					Outward: analyzer.PackageCouplingStats{},
				},
			},
		},
	}
}

// viewString extracts the string content from a tea.View.
func viewString(v tea.View) string {
	if v.Content == nil {
		return ""
	}

	if s, ok := v.Content.(fmt.Stringer); ok {
		return s.String()
	}

	return ""
}

func TestInteractive_SingleLanguage(t *testing.T) {
	m := newMetricsModel(singleLanguageGroups(), DefaultSort(), nil, "", false)

	var model tea.Model = m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestInteractive_MultiLanguage(t *testing.T) {
	m := newMetricsModel(multiLanguageGroups(), DefaultSort(), nil, "", false)

	var model tea.Model = m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestInteractive_TabSwitch(t *testing.T) {
	m := newMetricsModel(multiLanguageGroups(), DefaultSort(), nil, "", false)

	var model tea.Model = m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestInteractive_DrillDown(t *testing.T) {
	m := newMetricsModel(singleLanguageGroups(), DefaultSort(), nil, "", false)

	var model tea.Model = m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestInteractive_Sort(t *testing.T) {
	m := newMetricsModel(singleLanguageGroups(), DefaultSort(), nil, "", false)

	var model tea.Model = m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model, _ = model.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestInteractive_SortInstability(t *testing.T) {
	m := newMetricsModel(singleLanguageGroups(), DefaultSort(), nil, "", false)

	var model tea.Model = m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model, _ = model.Update(tea.KeyPressMsg{Code: 's', Text: "s"})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestInteractive_SortToggle(t *testing.T) {
	m := newMetricsModel(singleLanguageGroups(), DefaultSort(), nil, "", false)

	var model tea.Model = m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model, _ = model.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	model, _ = model.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

// enterPkgFilter builds the metrics model, enters filter mode, and types "pkg".
func enterPkgFilter(t *testing.T) tea.Model {
	t.Helper()

	m := newMetricsModel(singleLanguageGroups(), DefaultSort(), nil, "", false)

	var model tea.Model = m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	// Enter filter mode
	model, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	// Type "pkg"
	model, _ = model.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	model, _ = model.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	model, _ = model.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})

	return model
}

func TestInteractive_Filter(t *testing.T) {
	model := enterPkgFilter(t)

	// Confirm
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestInteractive_FilterClear(t *testing.T) {
	model := enterPkgFilter(t)

	// Escape clears
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestInteractive_DrillDownExpand(t *testing.T) {
	m := newMetricsModel(singleLanguageGroupsWithPositions(), DefaultSort(), nil, "", false)

	var model tea.Model = m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	// Enter drill-down
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// Expand first symbol (space)
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}

func TestInteractive_DrillDownPositionNav(t *testing.T) {
	m := newMetricsModel(singleLanguageGroupsWithPositions(), DefaultSort(), nil, "", false)

	var model tea.Model = m

	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	// Enter drill-down
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// Expand first symbol
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	// Navigate down
	model, _ = model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(viewString(model.View())))
}
