package golang_test

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/golang"
	"github.com/sebdah/goldie/v2"
	"github.com/stretchr/testify/require"
)

func TestGoAnalyzeMetrics(t *testing.T) {
	tests := map[string]struct {
		dir string
	}{
		"simple_nomod": {
			dir: ".testdata/simple_nomod",
		},
		"simple_gomod": {
			dir: ".testdata/simple_gomod",
		},
		"project_gomod": {
			dir: ".testdata/project_gomod",
		},
		"project_goworkspace": {
			dir: ".testdata/project_goworkspace",
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
			a := golang.GoAnalyzer()
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
