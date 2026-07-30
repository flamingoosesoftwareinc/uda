package rust_test

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/rust"
	"github.com/sebdah/goldie/v2"
	"github.com/stretchr/testify/require"
)

func TestRustAnalyzeMetrics(t *testing.T) {
	tests := map[string]struct {
		strategy   rust.BoundaryStrategy
		dir        string
		fixtureDir string
	}{
		"module/simple_crate": {
			strategy: rust.StrategyModule,
			dir:      ".testdata/simple_crate",
		},
		"module/simple_nocargo": {
			strategy: rust.StrategyModule,
			dir:      ".testdata/simple_nocargo",
		},
		"module/project_crate": {
			strategy: rust.StrategyModule,
			dir:      ".testdata/project_crate",
		},
		"module/project_crate_grouped_imports": {
			strategy: rust.StrategyModule,
			dir:      ".testdata/project_crate_grouped_imports",
		},
		"module/project_crate_lib": {
			strategy: rust.StrategyModule,
			dir:      ".testdata/project_crate_lib",
		},
		"module/project_workspace": {
			strategy: rust.StrategyModule,
			dir:      ".testdata/project_workspace",
		},
		"module/project_workspace_three_crates": {
			strategy: rust.StrategyModule,
			dir:      ".testdata/project_workspace_three_crates",
		},
		"module/project_nested_use": {
			strategy: rust.StrategyModule,
			dir:      ".testdata/project_nested_use",
		},
		"module/project_workspace_hyphenated": {
			strategy: rust.StrategyModule,
			dir:      ".testdata/project_workspace_hyphenated",
		},
		"package/project_workspace_hyphenated": {
			strategy:   rust.StrategyPackage,
			dir:        ".testdata/project_workspace_hyphenated",
			fixtureDir: ".testdata/package_workspace_hyphenated",
		},
		"package/project_workspace": {
			strategy:   rust.StrategyPackage,
			dir:        ".testdata/project_workspace",
			fixtureDir: ".testdata/package_workspace",
		},
		"package/project_workspace_three_crates": {
			strategy:   rust.StrategyPackage,
			dir:        ".testdata/project_workspace_three_crates",
			fixtureDir: ".testdata/package_workspace_three_crates",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixtureDir := tt.dir
			if tt.fixtureDir != "" {
				fixtureDir = tt.fixtureDir
			}

			g := goldie.New(t,
				goldie.WithFixtureDir(fixtureDir),
				goldie.WithNameSuffix(".json"),
			)

			dir := os.DirFS(tt.dir)
			a := rust.RustAnalyzer(rust.WithBoundaryStrategy(tt.strategy))
			got, err := a.Analyze(context.Background(), dir)
			require.NoError(t, err)

			slices.SortFunc(got, func(a, b analyzer.Metrics) int {
				if a.Package < b.Package {
					return -1
				}

				if a.Package > b.Package {
					return 1
				}

				return 0
			})

			g.AssertJson(t, "golden", got)

			boundaries, err := a.Boundaries(context.Background(), dir)
			require.NoError(t, err)

			slices.SortFunc(boundaries, func(a, b analyzer.PackageBoundary) int {
				if a.Name < b.Name {
					return -1
				}

				if a.Name > b.Name {
					return 1
				}

				return 0
			})

			for i := range boundaries {
				slices.Sort(boundaries[i].Dirs)
			}

			g.AssertJson(t, "boundaries", boundaries)
		})
	}
}

func TestWithBoundaryStrategyInvalidFallsBackToDefault(t *testing.T) {
	// An unrecognised strategy must be silently ignored; the analyser falls back
	// to the default (StrategyPackage) and produces the same output as no option.
	dir := os.DirFS(".testdata/simple_crate")

	defaultA := rust.RustAnalyzer()
	invalidA := rust.RustAnalyzer(rust.WithBoundaryStrategy("invalid"))

	gotDefault, err := defaultA.Analyze(context.Background(), dir)
	require.NoError(t, err)

	gotInvalid, err := invalidA.Analyze(context.Background(), dir)
	require.NoError(t, err)

	slices.SortFunc(gotDefault, func(a, b analyzer.Metrics) int {
		if a.Package < b.Package {
			return -1
		}

		if a.Package > b.Package {
			return 1
		}

		return 0
	})
	slices.SortFunc(gotInvalid, func(a, b analyzer.Metrics) int {
		if a.Package < b.Package {
			return -1
		}

		if a.Package > b.Package {
			return 1
		}

		return 0
	})

	require.Equal(t, gotDefault, gotInvalid,
		"invalid strategy should fall back to the default (StrategyPackage)")
}
