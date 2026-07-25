package ui

import (
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/diff"
	"github.com/sebdah/goldie/v2"
)

func TestReviewText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result ReviewResult
	}{
		{
			name: "no_changes",
			result: ReviewResult{
				BaseLabel: "abc1234",
				HeadLabel: "def5678",
				Diffs: []diff.PackageDiff{
					{
						Package:  "internal/core",
						DiffType: diff.Unchanged,
					},
					{
						Package:  "internal/db",
						DiffType: diff.Unchanged,
					},
				},
				PrevAll: []analyzer.Metrics{
					{
						Package: "internal/core",
						Outward: analyzer.PackageCouplingStats{"internal/db": {}},
					},
					{Package: "internal/db"},
				},
				CurrAll: []analyzer.Metrics{
					{
						Package: "internal/core",
						Outward: analyzer.PackageCouplingStats{"internal/db": {}},
					},
					{Package: "internal/db"},
				},
			},
		},
		{
			name: "new_dependency",
			result: ReviewResult{
				BaseLabel: "abc1234",
				HeadLabel: "def5678",
				Diffs: []diff.PackageDiff{
					{
						Package:  "internal/core",
						DiffType: diff.Unchanged,
						OutwardDiff: []diff.DependencyDiff{
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
													File: "core/server.go",
													Line: 42,
												},
												DiffType: diff.Added,
											},
										},
									},
									{
										Name:      "http.Handler",
										DiffType:  diff.Added,
										CurrCount: 1,
										Positions: []diff.PositionDiff{
											{
												Position: analyzer.Position{
													File: "core/server.go",
													Line: 58,
												},
												DiffType: diff.Added,
											},
										},
									},
								},
							},
						},
					},
				},
				PrevAll: []analyzer.Metrics{
					{
						Package: "internal/core",
						Outward: analyzer.PackageCouplingStats{"internal/db": {}},
					},
				},
				CurrAll: []analyzer.Metrics{
					{Package: "internal/core", Outward: analyzer.PackageCouplingStats{
						"internal/db":   {},
						"internal/http": {},
					}},
				},
			},
		},
		{
			name: "removed_dependency",
			result: ReviewResult{
				BaseLabel: "abc1234",
				HeadLabel: "def5678",
				Diffs: []diff.PackageDiff{
					{
						Package:  "internal/api",
						DiffType: diff.Unchanged,
						OutwardDiff: []diff.DependencyDiff{
							{
								Package:  "internal/legacy",
								DiffType: diff.Removed,
								Symbols: []diff.SymbolDiff{
									{
										Name:      "legacy.Transform",
										DiffType:  diff.Removed,
										PrevCount: 1,
										Positions: []diff.PositionDiff{
											{
												Position: analyzer.Position{
													File: "api/handler.go",
													Line: 12,
												},
												DiffType: diff.Removed,
											},
										},
									},
								},
							},
						},
					},
				},
				PrevAll: []analyzer.Metrics{
					{Package: "internal/api", Outward: analyzer.PackageCouplingStats{
						"internal/legacy": {},
						"internal/db":     {},
					}},
				},
				CurrAll: []analyzer.Metrics{
					{
						Package: "internal/api",
						Outward: analyzer.PackageCouplingStats{"internal/db": {}},
					},
				},
			},
		},
		{
			name: "mixed_changes",
			result: ReviewResult{
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
													File: "core/server.go",
													Line: 30,
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
													File: "core/app.go",
													Line: 18,
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
													File: "core/server.go",
													Line: 42,
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
			},
		},
		{
			name: "new_and_removed_packages",
			result: ReviewResult{
				BaseLabel: "abc1234",
				HeadLabel: "def5678",
				Diffs: []diff.PackageDiff{
					{Package: "internal/cache", DiffType: diff.Added},
					{Package: "internal/oldutil", DiffType: diff.Removed},
				},
				PrevAll: []analyzer.Metrics{
					{Package: "internal/oldutil"},
				},
				CurrAll: []analyzer.Metrics{
					{
						Package: "internal/cache",
						Outward: analyzer.PackageCouplingStats{"internal/db": {}},
					},
				},
			},
		},
	}

	g := goldie.New(t,
		goldie.WithFixtureDir(".testdata"),
		goldie.WithNameSuffix(".txt"),
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ReviewText(tt.result)
			g.Assert(t, "review/"+tt.name, []byte(got))
		})
	}
}
