package typescript_test

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/typescript"
	"github.com/sebdah/goldie/v2"
	"github.com/stretchr/testify/require"
)

func TestTsAnalyzeMetrics(t *testing.T) {
	tests := map[string]struct {
		strategy typescript.BoundaryStrategy
		dir      string
	}{
		"package/single": {
			strategy: typescript.StrategyPackage,
			dir:      ".testdata/package_single",
		},
		"package/monorepo": {
			strategy: typescript.StrategyPackage,
			dir:      ".testdata/package_monorepo",
		},
		"barrel/feature_folders": {
			strategy: typescript.StrategyBarrel,
			dir:      ".testdata/barrel_feature_folders",
		},
		"directory/flat": {
			strategy: typescript.StrategyDirectory,
			dir:      ".testdata/directory_flat",
		},
		"package/with_excludes": {
			strategy: typescript.StrategyPackage,
			dir:      ".testdata/package_with_excludes",
		},
		"package/subpath_imports": {
			strategy: typescript.StrategyPackage,
			dir:      ".testdata/package_subpath_imports",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g := goldie.New(t,
				goldie.WithFixtureDir(tt.dir),
				goldie.WithNameSuffix(".json"),
			)

			dir := os.DirFS(tt.dir)
			a := typescript.TsAnalyzer(typescript.WithBoundaryStrategy(tt.strategy))
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
	dir := os.DirFS(".testdata/package_single")

	defaultA := typescript.TsAnalyzer()
	invalidA := typescript.TsAnalyzer(typescript.WithBoundaryStrategy("invalid"))

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
