package golang_test

import (
	"context"
	"log/slog"
	"os"
	"slices"
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/golang"
	"github.com/stretchr/testify/require"
)

type packageImports struct {
	Pkg     analyzer.Package
	Imports []analyzer.Import
}

func toSortedSlice(pi analyzer.PackageImports) []packageImports {
	result := make([]packageImports, 0, len(pi))
	for pkg, imports := range pi {
		sorted := make([]analyzer.Import, len(imports))
		copy(sorted, imports)
		slices.Sort(sorted)
		result = append(result, packageImports{Pkg: pkg, Imports: sorted})
	}
	return result
}

func TestGoAnalyze(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	tests := map[string]struct {
		dir  string
		want analyzer.PackageImports
	}{
		"simple_nomod": {
			dir: "testdata/simple_nomod",
			want: analyzer.PackageImports{
				"main": []analyzer.Import{
					`"fmt"`,
				},
			},
		},
		"simple_gomod": {
			dir: "testdata/simple_gomod",
			want: analyzer.PackageImports{
				"example.com/simple_gomod/main": []analyzer.Import{
					`"fmt"`,
				},
			},
		},
		"project_gomod": {
			dir: "testdata/project_gomod",
			want: analyzer.PackageImports{
				"example.com/project_gomod/main": []analyzer.Import{
					`"example.com/project_gomod/cmd"`,
				},
				"example.com/project_gomod/cmd": []analyzer.Import{
					`"fmt"`,
				},
				"example.com/project_gomod/internal/foo": []analyzer.Import{
					`"fmt"`,
				},
				"example.com/project_gomod/internal/bar": []analyzer.Import{
					`"fmt"`,
					`"example.com/project_gomod/internal/bar/baz"`,
				},
				"example.com/project_gomod/internal/bar/baz": []analyzer.Import{
					`"fmt"`,
				},
			},
		},
		"project_goworkspace": {
			dir: "testdata/project_goworkspace",
			want: analyzer.PackageImports{
				"example.com/cowsay/main": []analyzer.Import{
					`"fmt"`,
					`"os"`,
					`"example.com/cowsay/cmd"`,
					`"example.com/cowsay/moo"`,
				},
				"example.com/foobarbaz/internal/greet": []analyzer.Import{
					`"fmt"`,
				},
				"example.com/cowsay/cmd": []analyzer.Import{
					`"fmt"`,
					`"example.com/cowsay/moo"`,
				},
				"example.com/cowsay/moo": []analyzer.Import{
					`"fmt"`,
				},
				"example.com/foobarbaz/main": []analyzer.Import{
					`"fmt"`,
					`"example.com/foobarbaz/internal/greet"`,
				},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := os.DirFS(tt.dir)
			got, err := golang.GoAnalyzer().Analyze(context.Background(), dir)
			require.NoError(t, err)
			require.ElementsMatch(t, toSortedSlice(tt.want), toSortedSlice(got))
		})
	}
}
