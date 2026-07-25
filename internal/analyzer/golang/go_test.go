package golang

import (
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/stretchr/testify/require"
)

func TestGetPkgPathPrefix(t *testing.T) {
	tests := map[string]struct {
		goFilepath string
		gomodPaths map[directory]modulePath
		want       modulePath
	}{
		"should return module path with directory when go.mod found in parent": {
			goFilepath: "src/cmd/main.go",
			gomodPaths: map[directory]modulePath{
				"src": "github.com/example/project",
			},
			want: "github.com/example/project/cmd",
		},
		"should return module path with directory when go.mod found in grandparent": {
			goFilepath: "src/pkg/handler/handler.go",
			gomodPaths: map[directory]modulePath{
				"src": "github.com/example/project",
			},
			want: "github.com/example/project/pkg/handler",
		},
		"should return module path for root go.mod": {
			goFilepath: "cmd/main.go",
			gomodPaths: map[directory]modulePath{
				".": "github.com/example/project",
			},
			want: "github.com/example/project/cmd",
		},
		"should return module path when go.mod is in nested src/go/src": {
			goFilepath: "src/go/src/cmd/main.go",
			gomodPaths: map[directory]modulePath{
				"src/go/src": "github.com/example/nested",
			},
			want: "github.com/example/nested/cmd",
		},
		"should return module path when file is deeply nested under src/go/src": {
			goFilepath: "src/go/src/pkg/handler/handler.go",
			gomodPaths: map[directory]modulePath{
				"src/go/src": "github.com/example/nested",
			},
			want: "github.com/example/nested/pkg/handler",
		},
		"should return file dir when file is directly in src/go/src": {
			goFilepath: "src/go/src/main.go",
			gomodPaths: map[directory]modulePath{
				"src/go/src": "github.com/example/nested",
			},
			want: "github.com/example/nested",
		},
		"should return file dir when no go.mod found": {
			goFilepath: "src/cmd/main.go",
			gomodPaths: map[directory]modulePath{},
			want:       "src/cmd",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := getPkgPathPrefix(tt.goFilepath, tt.gomodPaths)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestOrInitPkgNode(t *testing.T) {
	tests := map[string]struct {
		c      captures
		setup  func(g *analyzer.PackageAnalysisTree)
		assert func(t *testing.T, g *analyzer.PackageAnalysisTree)
	}{
		"should create pkg node with in, alias, and out children on empty graph": {
			c: captures{p: "github.com/example/project/cmd"},
			assert: func(t *testing.T, g *analyzer.PackageAnalysisTree) {
				pkgNode, exists := g.Get("github.com/example/project/cmd")
				require.True(t, exists, "expected pkg node to be created")
				require.NotNil(t, pkgNode.Out, "expected Out to be initialized")
				require.NotNil(t, pkgNode.In, "expected In to be initialized")
				require.NotNil(t, pkgNode.Aliases, "expected Aliases to be initialized")
			},
		},
		"should be idempotent when root nodes already exist": {
			c: captures{p: "github.com/example/project/cmd"},
			setup: func(g *analyzer.PackageAnalysisTree) {
				g.Add("github.com/example/project/cmd")
			},
			assert: func(t *testing.T, g *analyzer.PackageAnalysisTree) {
				pkgNode, exists := g.Get("github.com/example/project/cmd")
				require.True(t, exists, "expected pkg node to exist")
				require.NotNil(t, pkgNode.Out, "expected Out to be initialized")
				require.NotNil(t, pkgNode.In, "expected In to be initialized")
				require.NotNil(t, pkgNode.Aliases, "expected Aliases to be initialized")
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g := analyzer.NewPackageAnalysisTree()
			if tt.setup != nil {
				tt.setup(g)
			}

			getOrInitPkgNode(t.Context(), g, tt.c)
			tt.assert(t, g)
		})
	}
}

func TestHydrateOutNode(t *testing.T) {
	tests := map[string]struct {
		imports []analyzer.Import
		setup   func() *analyzer.PackageImportInfo
		assert  func(t *testing.T, outNode *analyzer.PackageImportInfo)
	}{
		"should add imports as children of outNode": {
			imports: []analyzer.Import{
				"github.com/asdflka/blah/http",
				"github.com/asdflka/blah/models",
			},
			setup: func() *analyzer.PackageImportInfo {
				tree := analyzer.NewPackageAnalysisTree()
				pa := tree.Add("github.com/example/project/cmd")

				return pa.Out
			},
			assert: func(t *testing.T, outNode *analyzer.PackageImportInfo) {
				_, httpExists := outNode.Get("github.com/asdflka/blah/http")
				require.True(t, httpExists)

				_, modelsExists := outNode.Get("github.com/asdflka/blah/models")
				require.True(t, modelsExists)

				require.Len(t, outNode.GetChildren(), 2)
			},
		},
		"should not duplicate imports that already exist": {
			imports: []analyzer.Import{
				"github.com/asdflka/blah/http",
				"github.com/asdflka/blah/models",
			},
			setup: func() *analyzer.PackageImportInfo {
				tree := analyzer.NewPackageAnalysisTree()
				pa := tree.Add("github.com/example/project/cmd")
				pa.Out.Add("github.com/asdflka/blah/http")

				return pa.Out
			},
			assert: func(t *testing.T, outNode *analyzer.PackageImportInfo) {
				require.Len(t, outNode.GetChildren(), 2)

				_, httpExists := outNode.Get("github.com/asdflka/blah/http")
				require.True(t, httpExists)

				_, modelsExists := outNode.Get("github.com/asdflka/blah/models")
				require.True(t, modelsExists)
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			outNode := tt.setup()
			hydrateOutNode(tt.imports, outNode)
			tt.assert(t, outNode)
		})
	}
}

func TestHydrateAliases(t *testing.T) {
	tests := map[string]struct {
		aliases []capturedAlias
		setup   func() (aliasMap map[string]string, outNode *analyzer.PackageImportInfo)
		assert  func(t *testing.T, aliasMap map[string]string, outNode *analyzer.PackageImportInfo)
	}{
		"should add alias to aliasMap and its package to outNode": {
			aliases: []capturedAlias{{name: "h", pkg: "github.com/asdflka/blah/http"}},
			setup: func() (map[string]string, *analyzer.PackageImportInfo) {
				tree := analyzer.NewPackageAnalysisTree()
				pa := tree.Add("github.com/example/project/cmd")

				return pa.Aliases, pa.Out
			},
			assert: func(t *testing.T, aliasMap map[string]string, outNode *analyzer.PackageImportInfo) {
				pkg, exists := aliasMap["h"]
				require.True(t, exists, "expected alias 'h' to be created")
				require.Equal(t, "github.com/asdflka/blah/http", pkg)

				_, outExists := outNode.Get("github.com/asdflka/blah/http")
				require.True(t, outExists, "expected aliased package added to outNode")
			},
		},
		"should not duplicate alias or import that already exist": {
			aliases: []capturedAlias{{name: "h", pkg: "github.com/asdflka/blah/http"}},
			setup: func() (map[string]string, *analyzer.PackageImportInfo) {
				tree := analyzer.NewPackageAnalysisTree()
				pa := tree.Add("github.com/example/project/cmd")
				pa.Aliases["h"] = "github.com/asdflka/blah/http"
				pa.Out.Add("github.com/asdflka/blah/http")

				return pa.Aliases, pa.Out
			},
			assert: func(t *testing.T, aliasMap map[string]string, outNode *analyzer.PackageImportInfo) {
				require.Len(t, aliasMap, 1)
				require.Len(t, outNode.GetChildren(), 1)
			},
		},
		"should handle multiple aliases": {
			aliases: []capturedAlias{
				{name: "h", pkg: "github.com/asdflka/blah/http"},
				{name: "m", pkg: "github.com/asdflka/blah/models"},
			},
			setup: func() (map[string]string, *analyzer.PackageImportInfo) {
				tree := analyzer.NewPackageAnalysisTree()
				pa := tree.Add("github.com/example/project/cmd")

				return pa.Aliases, pa.Out
			},
			assert: func(t *testing.T, aliasMap map[string]string, outNode *analyzer.PackageImportInfo) {
				require.Len(t, aliasMap, 2)
				require.Len(t, outNode.GetChildren(), 2)

				require.Equal(t, "github.com/asdflka/blah/http", aliasMap["h"])
				require.Equal(t, "github.com/asdflka/blah/models", aliasMap["m"])
			},
		},
		"should populate outNode once when same package is aliased twice": {
			aliases: []capturedAlias{
				{name: "h", pkg: "github.com/asdflka/blah/http"},
				{name: "hh", pkg: "github.com/asdflka/blah/http"},
			},
			setup: func() (map[string]string, *analyzer.PackageImportInfo) {
				tree := analyzer.NewPackageAnalysisTree()
				pa := tree.Add("github.com/example/project/cmd")

				return pa.Aliases, pa.Out
			},
			assert: func(t *testing.T, aliasMap map[string]string, outNode *analyzer.PackageImportInfo) {
				require.Len(t, aliasMap, 2, "expected both alias names to be kept")
				require.Len(
					t,
					outNode.GetChildren(),
					1,
					"expected package added to outNode only once",
				)
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			aliasMap, outNode := tt.setup()
			hydrateAliases(t.Context(), tt.aliases, aliasMap, outNode)
			tt.assert(t, aliasMap, outNode)
		})
	}
}

func TestHydrateQualifiedExpressions(t *testing.T) {
	tests := map[string]struct {
		expr   []capturedExpr
		setup  func() (aliasMap map[string]string, outNode *analyzer.PackageImportInfo)
		assert func(t *testing.T, outNode *analyzer.PackageImportInfo)
	}{
		"should add qualified type as child of its imported package node": {
			expr: []capturedExpr{{expr: "context.Context"}},
			setup: func() (map[string]string, *analyzer.PackageImportInfo) {
				tree := analyzer.NewPackageAnalysisTree()
				pa := tree.Add("github.com/example/project/cmd")
				pa.Out.Add("context")

				return pa.Aliases, pa.Out
			},
			assert: func(t *testing.T, outNode *analyzer.PackageImportInfo) {
				ctxInfo, exists := outNode.Get("context")
				require.True(t, exists)

				stats := ctxInfo.CouplingStats()
				require.Contains(t, stats, "context.Context")
				require.Equal(t, uint(1), stats["context.Context"].Count)
			},
		},
		"should resolve qualified expression through alias": {
			expr: []capturedExpr{{expr: "h.Server"}},
			setup: func() (map[string]string, *analyzer.PackageImportInfo) {
				tree := analyzer.NewPackageAnalysisTree()
				pa := tree.Add("github.com/example/project/cmd")
				pa.Aliases["h"] = "net/http"
				pa.Out.Add("net/http")

				return pa.Aliases, pa.Out
			},
			assert: func(t *testing.T, outNode *analyzer.PackageImportInfo) {
				httpInfo, exists := outNode.Get("net/http")
				require.True(t, exists)

				stats := httpInfo.CouplingStats()
				require.Contains(t, stats, "http.Server")
				require.Equal(t, uint(1), stats["http.Server"].Count)
			},
		},
		"should add package scoped function as child of its imported package node": {
			expr: []capturedExpr{{expr: "io.ReadAll"}},
			setup: func() (map[string]string, *analyzer.PackageImportInfo) {
				tree := analyzer.NewPackageAnalysisTree()
				pa := tree.Add("github.com/example/project/cmd")
				pa.Out.Add("io")

				return pa.Aliases, pa.Out
			},
			assert: func(t *testing.T, outNode *analyzer.PackageImportInfo) {
				ioInfo, exists := outNode.Get("io")
				require.True(t, exists)

				stats := ioInfo.CouplingStats()
				require.Contains(t, stats, "io.ReadAll")
				require.Equal(t, uint(1), stats["io.ReadAll"].Count)
			},
		},
		"should skip expressions where qualifier is not an imported package": {
			expr: []capturedExpr{{expr: "myStruct.Field"}, {expr: "cfg.Timeout"}},
			setup: func() (map[string]string, *analyzer.PackageImportInfo) {
				tree := analyzer.NewPackageAnalysisTree()
				pa := tree.Add("github.com/example/project/cmd")
				pa.Out.Add("context")

				return pa.Aliases, pa.Out
			},
			assert: func(t *testing.T, outNode *analyzer.PackageImportInfo) {
				ctxInfo, exists := outNode.Get("context")
				require.True(t, exists)

				stats := ctxInfo.CouplingStats()
				require.Empty(
					t,
					stats,
					"expected no expressions added for non-package qualifiers",
				)
			},
		},
		"should increment occurrences for duplicate qualified expressions": {
			expr: []capturedExpr{{expr: "http.Server"}, {expr: "http.Server"}},
			setup: func() (map[string]string, *analyzer.PackageImportInfo) {
				tree := analyzer.NewPackageAnalysisTree()
				pa := tree.Add("github.com/example/project/cmd")
				pa.Out.Add("net/http")

				return pa.Aliases, pa.Out
			},
			assert: func(t *testing.T, outNode *analyzer.PackageImportInfo) {
				httpInfo, exists := outNode.Get("net/http")
				require.True(t, exists)

				stats := httpInfo.CouplingStats()
				require.Len(t, stats, 1)
				require.Equal(t, uint(2), stats["http.Server"].Count)
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			aliasMap, outNode := tt.setup()
			hydrateQualifiedExpressions(t.Context(), tt.expr, aliasMap, outNode)
			tt.assert(t, outNode)
		})
	}
}

func TestResolveInwardDependencies(t *testing.T) {
	tests := map[string]struct {
		setup  func(g *analyzer.PackageAnalysisTree)
		assert func(t *testing.T, g *analyzer.PackageAnalysisTree)
	}{
		"should add to in node when imported package exists in graph": {
			setup: func(g *analyzer.PackageAnalysisTree) {
				// pkg A imports pkg B
				pkgA := g.Add("github.com/example/project/a")
				pkgA.Out.Add("github.com/example/project/b")

				g.Add("github.com/example/project/b")
			},
			assert: func(t *testing.T, g *analyzer.PackageAnalysisTree) {
				pkgB, _ := g.Get("github.com/example/project/b")

				_, depExists := pkgB.In.Get("github.com/example/project/a")
				require.True(t, depExists, "expected pkg A to appear in package B's in node")
			},
		},
		"should skip imports that do not exist in the graph": {
			setup: func(g *analyzer.PackageAnalysisTree) {
				pkgA := g.Add("github.com/example/project/a")
				// imports an external package not in the graph
				pkgA.Out.Add("github.com/external/lib")
			},
			assert: func(t *testing.T, g *analyzer.PackageAnalysisTree) {
				pkgA, _ := g.Get("github.com/example/project/a")

				require.Empty(
					t,
					pkgA.In.GetChildren(),
					"expected no inward deps for external imports",
				)
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g := analyzer.NewPackageAnalysisTree()
			tt.setup(g)

			analyzer.ResolveInwardDependencies(g)
			tt.assert(t, g)
		})
	}
}

func TestGetCouplingStats(t *testing.T) {
	tests := map[string]struct {
		setup  func() *analyzer.PackageImportInfo
		assert func(t *testing.T, stats analyzer.PackageCouplingStats)
	}{
		"should return empty stats when inOrOutNode is nil": {
			setup: func() *analyzer.PackageImportInfo {
				return nil
			},
			assert: func(t *testing.T, stats analyzer.PackageCouplingStats) {
				require.NotNil(t, stats)
				require.Empty(t, stats)
			},
		},
		"should return coupling stats for multiple imported packages": {
			setup: func() *analyzer.PackageImportInfo {
				tree := analyzer.NewPackageAnalysisTree()
				pa := tree.Add("pkg")
				ctxInfo := pa.Out.Add("context")
				ctxInfo.AddWithCount("context.Context", "context.Context", 2, nil)
				ctxInfo.AddWithCount("context.Background", "context.Background", 3, nil)

				httpInfo := pa.Out.Add("net/http")
				httpInfo.AddWithCount("http.Server", "http.Server", 1, nil)

				return pa.Out
			},
			assert: func(t *testing.T, stats analyzer.PackageCouplingStats) {
				require.Len(t, stats, 2)

				contextCouplingStats, exists := stats["context"]
				require.True(t, exists)
				require.Len(t, contextCouplingStats, 2)
				require.Equal(t, uint(2), contextCouplingStats["context.Context"].Count)
				require.Equal(t, uint(3), contextCouplingStats["context.Background"].Count)

				httpCouplingStats, exists := stats["net/http"]
				require.True(t, exists)
				require.Len(t, httpCouplingStats, 1)
				require.Equal(t, uint(1), httpCouplingStats["http.Server"].Count)
			},
		},
		"should return empty stats when node has no children": {
			setup: func() *analyzer.PackageImportInfo {
				tree := analyzer.NewPackageAnalysisTree()
				pa := tree.Add("pkg")

				return pa.Out
			},
			assert: func(t *testing.T, stats analyzer.PackageCouplingStats) {
				require.NotNil(t, stats)
				require.Empty(t, stats)
			},
		},
		"should return empty coupling stats for import with no qualified expressions": {
			setup: func() *analyzer.PackageImportInfo {
				tree := analyzer.NewPackageAnalysisTree()
				pa := tree.Add("pkg")
				pa.Out.Add("context")

				return pa.Out
			},
			assert: func(t *testing.T, stats analyzer.PackageCouplingStats) {
				require.Len(t, stats, 1)

				couplingStats, exists := stats["context"]
				require.True(t, exists)
				require.Empty(t, couplingStats)
			},
		},
		"should return coupling stats for single import with expressions": {
			setup: func() *analyzer.PackageImportInfo {
				tree := analyzer.NewPackageAnalysisTree()
				pa := tree.Add("pkg")
				ctxInfo := pa.Out.Add("context")
				ctxInfo.AddWithCount("context.Context", "context.Context", 7, nil)

				return pa.Out
			},
			assert: func(t *testing.T, stats analyzer.PackageCouplingStats) {
				require.Len(t, stats, 1)

				couplingStats, exists := stats["context"]
				require.True(t, exists)
				require.Len(t, couplingStats, 1)
				require.Equal(t, uint(7), couplingStats["context.Context"].Count)
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pii := tt.setup()
			stats := analyzer.GetCouplingStats(t.Context(), pii)
			tt.assert(t, stats)
		})
	}
}
