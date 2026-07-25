package python_test

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/python"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "update golden files")

func TestPythonAnalyzeMetrics(t *testing.T) {
	tests := map[string]struct {
		strategy   python.BoundaryStrategy
		dir        string
		fixtureDir string
	}{
		"module/simple_nosetup": {
			strategy: python.StrategyModule,
			dir:      ".testdata/simple_nosetup",
		},
		"module/simple_nomanifest": {
			strategy: python.StrategyModule,
			dir:      ".testdata/simple_nomanifest",
		},
		"module/simple_package": {
			strategy: python.StrategyModule,
			dir:      ".testdata/simple_package",
		},
		"module/simple_namespace": {
			strategy: python.StrategyModule,
			dir:      ".testdata/simple_namespace",
		},
		"module/project_package": {
			strategy: python.StrategyModule,
			dir:      ".testdata/project_package",
		},
		"module/project_monorepo": {
			strategy: python.StrategyModule,
			dir:      ".testdata/project_monorepo",
		},
		"module/complex_imports": {
			strategy: python.StrategyModule,
			dir:      ".testdata/complex_imports",
		},
		"module/namespace_cross_deps": {
			strategy: python.StrategyModule,
			dir:      ".testdata/namespace_cross_deps",
		},
		"module/decorators_and_types": {
			strategy: python.StrategyModule,
			dir:      ".testdata/decorators_and_types",
		},
		"module/real_python_dotenv": {
			strategy: python.StrategyModule,
			dir:      ".testdata/real_python_dotenv",
		},
		"package/simple_package": {
			strategy:   python.StrategyPackage,
			dir:        ".testdata/simple_package",
			fixtureDir: ".testdata/package_simple_package",
		},
		"package/project_package": {
			strategy:   python.StrategyPackage,
			dir:        ".testdata/project_package",
			fixtureDir: ".testdata/package_project_package",
		},
		"package/project_monorepo": {
			strategy:   python.StrategyPackage,
			dir:        ".testdata/project_monorepo",
			fixtureDir: ".testdata/package_project_monorepo",
		},
		"subpackage/project_package": {
			strategy:   python.StrategySubpackage,
			dir:        ".testdata/project_package",
			fixtureDir: ".testdata/subpackage_project_package",
		},
		"subpackage/project_monorepo": {
			strategy:   python.StrategySubpackage,
			dir:        ".testdata/project_monorepo",
			fixtureDir: ".testdata/subpackage_project_monorepo",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixtureDir := tt.dir
			if tt.fixtureDir != "" {
				fixtureDir = tt.fixtureDir
			}

			dir := os.DirFS(tt.dir)
			a := python.PythonAnalyzer(python.WithBoundaryStrategy(tt.strategy))
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

			actual, err := json.MarshalIndent(got, "", "  ")
			require.NoError(t, err)

			goldenPath := filepath.Join(fixtureDir, "golden.json")
			if *update {
				err := os.MkdirAll(fixtureDir, 0o755)
				require.NoError(t, err)
				err = os.WriteFile(goldenPath, actual, 0o644)
				require.NoError(t, err)
			} else {
				expected, err := os.ReadFile(goldenPath)
				require.NoError(t, err, "golden file not found, run with -update to generate")
				require.JSONEq(t, string(expected), string(actual))
			}

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

			boundariesJSON, err := json.MarshalIndent(boundaries, "", "  ")
			require.NoError(t, err)

			boundariesPath := filepath.Join(fixtureDir, "boundaries.json")
			if *update {
				err := os.WriteFile(boundariesPath, boundariesJSON, 0o644)
				require.NoError(t, err)

				return
			}

			expectedBoundaries, err := os.ReadFile(boundariesPath)
			require.NoError(
				t,
				err,
				"boundaries golden file not found, run with -update to generate",
			)
			require.JSONEq(t, string(expectedBoundaries), string(boundariesJSON))
		})
	}
}

func TestPythonAnalyzerBackwardsCompatibility(t *testing.T) {
	// PythonAnalyzer() with no options should produce identical results
	// to PythonAnalyzer(WithBoundaryStrategy(StrategyModule)).
	dir := os.DirFS(".testdata/simple_package")

	noOpts := python.PythonAnalyzer()
	explicit := python.PythonAnalyzer(python.WithBoundaryStrategy(python.StrategyModule))

	gotNoOpts, err := noOpts.Analyze(context.Background(), dir)
	require.NoError(t, err)

	gotExplicit, err := explicit.Analyze(context.Background(), dir)
	require.NoError(t, err)

	sortMetrics := func(m []analyzer.Metrics) {
		slices.SortFunc(m, func(a, b analyzer.Metrics) int {
			if a.Package < b.Package {
				return -1
			}

			if a.Package > b.Package {
				return 1
			}

			return 0
		})
	}
	sortMetrics(gotNoOpts)
	sortMetrics(gotExplicit)

	noOptsJSON, err := json.MarshalIndent(gotNoOpts, "", "  ")
	require.NoError(t, err)
	explicitJSON, err := json.MarshalIndent(gotExplicit, "", "  ")
	require.NoError(t, err)

	require.JSONEq(
		t,
		string(noOptsJSON),
		string(explicitJSON),
		"PythonAnalyzer() should produce the same results as PythonAnalyzer(WithBoundaryStrategy(StrategyModule))",
	)
}

func TestWithBoundaryStrategyInvalidFallsBackToDefault(t *testing.T) {
	// An unrecognised strategy must be silently ignored; the analyser falls back
	// to the default (StrategyModule) and produces the same output as no option.
	dir := os.DirFS(".testdata/simple_nosetup")

	defaultA := python.PythonAnalyzer()
	invalidA := python.PythonAnalyzer(python.WithBoundaryStrategy("invalid"))

	gotDefault, err := defaultA.Analyze(context.Background(), dir)
	require.NoError(t, err)

	gotInvalid, err := invalidA.Analyze(context.Background(), dir)
	require.NoError(t, err)

	sortMetrics := func(m []analyzer.Metrics) {
		slices.SortFunc(m, func(a, b analyzer.Metrics) int {
			if a.Package < b.Package {
				return -1
			}

			if a.Package > b.Package {
				return 1
			}

			return 0
		})
	}
	sortMetrics(gotDefault)
	sortMetrics(gotInvalid)

	defaultJSON, err := json.MarshalIndent(gotDefault, "", "  ")
	require.NoError(t, err)
	invalidJSON, err := json.MarshalIndent(gotInvalid, "", "  ")
	require.NoError(t, err)

	require.JSONEq(t, string(defaultJSON), string(invalidJSON),
		"invalid strategy should fall back to the default (StrategyModule)")
}
