package swift_test

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/swift"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", os.Getenv("UPDATE_GOLDEN") == "1", "update golden files")

func TestSwiftAnalyzeMetrics(t *testing.T) {
	tests := map[string]struct {
		dir string
	}{
		"simple_nopackage": {
			dir: ".testdata/simple_nopackage",
		},
		"simple_package": {
			dir: ".testdata/simple_package",
		},
		"project_package": {
			dir: ".testdata/project_package",
		},
		"project_workspace": {
			dir: ".testdata/project_workspace",
		},
		"real_swift_argument_parser": {
			dir: ".testdata/real_swift_argument_parser",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := os.DirFS(tt.dir)
			a := swift.SwiftAnalyzer()
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

			goldenPath := filepath.Join(tt.dir, "golden.json")
			if *update {
				err := os.WriteFile(goldenPath, actual, 0o644)
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

			boundariesPath := filepath.Join(tt.dir, "boundaries.json")
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
