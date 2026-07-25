package rust

import (
	"testing"
	"testing/fstest"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/stretchr/testify/require"
)

func TestCrateImportName(t *testing.T) {
	tests := map[string]struct {
		crate crateInfo
		want  string
	}{
		"plain name unchanged": {
			crate: crateInfo{name: "casesplit"},
			want:  "casesplit",
		},
		"hyphens normalize to underscores": {
			crate: crateInfo{name: "iso-model"},
			want:  "iso_model",
		},
		"explicit lib name wins over normalization": {
			crate: crateInfo{name: "renamed-lib", libName: "renamed"},
			want:  "renamed",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, tt.crate.importName())
		})
	}
}

func TestExtractCrateInfoCapturesLibName(t *testing.T) {
	dir := fstest.MapFS{
		"renamed-lib/Cargo.toml": &fstest.MapFile{Data: []byte(
			"[package]\nname = \"renamed-lib\"\n\n[lib]\nname = \"renamed\"\n",
		)},
		"iso-model/Cargo.toml": &fstest.MapFile{Data: []byte(
			"[package]\nname = \"iso-model\"\n",
		)},
	}

	crateMap, err := extractCrateInfo(
		t.Context(),
		dir,
		[]string{"renamed-lib/Cargo.toml", "iso-model/Cargo.toml"},
	)
	require.NoError(t, err)

	require.Equal(t, "renamed", crateMap["renamed-lib"].libName)
	require.Equal(t, "renamed", crateMap["renamed-lib"].importName())
	require.Empty(t, crateMap["iso-model"].libName)
	require.Equal(t, "iso_model", crateMap["iso-model"].importName())
}

func TestLookupWorkspaceCrateCollision(t *testing.T) {
	// Both packages import as "x_y_z"; the pick must be deterministic.
	t.Run("exact package name wins", func(t *testing.T) {
		crateMap := map[directory]*crateInfo{
			"a": {name: "x-y-z", dir: "a"},
			"b": {name: "x_y_z", dir: "b"},
		}

		got := lookupWorkspaceCrate("x_y_z", crateMap)
		require.NotNil(t, got)
		require.Equal(t, crateName("x_y_z"), got.name)
	})

	t.Run("else lexicographically smallest package name", func(t *testing.T) {
		crateMap := map[directory]*crateInfo{
			"a": {name: "x-y_z", dir: "a"},
			"b": {name: "x_y-z", dir: "b"},
		}

		got := lookupWorkspaceCrate("x_y_z", crateMap)
		require.NotNil(t, got)
		require.Equal(t, crateName("x-y_z"), got.name)
	})

	t.Run("no match returns nil", func(t *testing.T) {
		crateMap := map[directory]*crateInfo{
			"a": {name: "iso-model", dir: "a"},
		}

		require.Nil(t, lookupWorkspaceCrate("serde", crateMap))
	})
}

func TestGetModulePath(t *testing.T) {
	tests := map[string]struct {
		filePath string
		ci       *crateInfo
		want     string
	}{
		"main.rs is crate root": {
			filePath: "myapp/src/main.rs",
			ci:       &crateInfo{name: "myapp", dir: "myapp"},
			want:     "myapp",
		},
		"lib.rs is crate root": {
			filePath: "mylib/src/lib.rs",
			ci:       &crateInfo{name: "mylib", dir: "mylib"},
			want:     "mylib",
		},
		"module file": {
			filePath: "myapp/src/config.rs",
			ci:       &crateInfo{name: "myapp", dir: "myapp"},
			want:     "myapp::config",
		},
		"nested module file": {
			filePath: "myapp/src/handler/routes.rs",
			ci:       &crateInfo{name: "myapp", dir: "myapp"},
			want:     "myapp::handler::routes",
		},
		"mod.rs represents parent directory": {
			filePath: "myapp/src/handler/mod.rs",
			ci:       &crateInfo{name: "myapp", dir: "myapp"},
			want:     "myapp::handler",
		},
		"root directory crate": {
			filePath: "src/main.rs",
			ci:       &crateInfo{name: "root_crate", dir: "."},
			want:     "root_crate",
		},
		"root directory module": {
			filePath: "src/config.rs",
			ci:       &crateInfo{name: "root_crate", dir: "."},
			want:     "root_crate::config",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := getModulePath(tt.filePath, tt.ci, StrategyModule)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGetCrateForFile(t *testing.T) {
	tests := map[string]struct {
		filePath string
		crateMap map[directory]*crateInfo
		wantName crateName
		wantNil  bool
	}{
		"finds crate in same directory": {
			filePath: "app/src/main.rs",
			crateMap: map[directory]*crateInfo{
				"app": {name: "app", dir: "app"},
			},
			wantName: "app",
		},
		"finds crate in parent directory": {
			filePath: "app/src/handler/routes.rs",
			crateMap: map[directory]*crateInfo{
				"app": {name: "app", dir: "app"},
			},
			wantName: "app",
		},
		"returns nil when no crate found": {
			filePath: "orphan/src/main.rs",
			crateMap: map[directory]*crateInfo{
				"app": {name: "app", dir: "app"},
			},
			wantNil: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := getCrateForFile(tt.filePath, tt.crateMap)
			if tt.wantNil {
				require.Nil(t, got)
			} else {
				require.NotNil(t, got)
				require.Equal(t, tt.wantName, got.name)
			}
		})
	}
}

func TestResolveUsePath(t *testing.T) {
	currentCrate := &crateInfo{name: "myapp", dir: "myapp"}
	crateMap := map[directory]*crateInfo{
		"myapp":       currentCrate,
		"core":        {name: "core", dir: "core"},
		"iso-model":   {name: "iso-model", dir: "iso-model"},
		"casesplit":   {name: "casesplit", dir: "casesplit"},
		"renamed-lib": {name: "renamed-lib", dir: "renamed-lib", libName: "renamed"},
	}

	tests := map[string]struct {
		usePath    string
		wantPkg    string
		wantIntrnl bool
		wantSkip   bool
	}{
		"crate internal module": {
			usePath:    "crate::config::Config",
			wantPkg:    "myapp::config",
			wantIntrnl: true,
		},
		"crate nested module": {
			usePath:    "crate::handler::router::Router",
			wantPkg:    "myapp::handler::router",
			wantIntrnl: true,
		},
		"crate bare (no subpath)": {
			usePath:  "crate",
			wantSkip: true,
		},
		"crate root item (only 2 segments)": {
			usePath:  "crate::SomeRootItem",
			wantSkip: true,
		},
		"self is skipped": {
			usePath:  "self::something",
			wantSkip: true,
		},
		"super is skipped": {
			usePath:  "super::something",
			wantSkip: true,
		},
		// std counted as efferent dep inflates instability — sysroot must be skipped
		"std is skipped": {
			usePath:  "std::fmt",
			wantSkip: true,
		},
		// std nested module still sysroot — must not appear as Ce
		"std nested module is skipped": {
			usePath:  "std::collections::HashMap",
			wantSkip: true,
		},
		// alloc counted as efferent dep inflates instability — sysroot must be skipped
		"alloc is skipped": {
			usePath:  "alloc::vec::Vec",
			wantSkip: true,
		},
		// core is a workspace crate here — resolves as external workspace dep, not skipped
		"workspace crate core resolves normally": {
			usePath: "core::fmt::Debug",
			wantPkg: "core::fmt",
		},
		// proc_macro counted as efferent dep inflates instability — sysroot must be skipped
		"proc_macro is skipped": {
			usePath:  "proc_macro::TokenStream",
			wantSkip: true,
		},
		// test counted as efferent dep inflates instability — sysroot must be skipped
		"test is skipped": {
			usePath:  "test::black_box",
			wantSkip: true,
		},
		"external serde dependency": {
			usePath: "serde::Deserialize",
			wantPkg: "serde",
		},
		"external tokio nested module": {
			usePath: "tokio::runtime::Runtime",
			wantPkg: "tokio::runtime",
		},
		"workspace crate dependency with module": {
			usePath: "core::service::Service",
			wantPkg: "core::service",
		},
		"workspace crate item at root": {
			usePath: "core::SomeItem",
			wantPkg: "core",
		},
		"workspace crate bare": {
			usePath: "core",
			wantPkg: "core",
		},
		// Cargo normalizes hyphens to underscores for the import name:
		// package "iso-model" is imported as "iso_model". The resolved key
		// re-roots at the Cargo package spelling so it matches boundaries.
		"hyphenated crate via import name": {
			usePath: "iso_model::model::Mirror",
			wantPkg: "iso-model::model",
		},
		"hyphenated crate item at root": {
			usePath: "iso_model::Mirror",
			wantPkg: "iso-model",
		},
		"unchanged crate name still resolves": {
			usePath: "casesplit::tokens::Token",
			wantPkg: "casesplit::tokens",
		},
		"explicit lib name resolves to owning package": {
			usePath: "renamed::report::Report",
			wantPkg: "renamed-lib::report",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pkg, isInternal, skip := resolveUsePath(
				tt.usePath,
				currentCrate,
				crateMap,
				StrategyModule,
			)
			require.Equal(t, tt.wantSkip, skip, "skip mismatch")

			if !skip {
				require.Equal(t, tt.wantPkg, pkg)
				require.Equal(t, tt.wantIntrnl, isInternal)
			}
		})
	}
}

func TestResolveUsePathPackageStrategy(t *testing.T) {
	currentCrate := &crateInfo{name: "myapp", dir: "myapp"}
	crateMap := map[directory]*crateInfo{
		"myapp":     currentCrate,
		"iso-model": {name: "iso-model", dir: "iso-model"},
	}

	tests := map[string]struct {
		usePath string
		wantPkg string
	}{
		"hyphenated crate via import name": {
			usePath: "iso_model::model::Mirror",
			wantPkg: "iso-model",
		},
		"external dependency stays as written": {
			usePath: "serde::Deserialize",
			wantPkg: "serde",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pkg, _, skip := resolveUsePath(tt.usePath, currentCrate, crateMap, StrategyPackage)
			require.False(t, skip, "skip mismatch")
			require.Equal(t, tt.wantPkg, pkg)
		})
	}
}

func TestResolveUsePathSysrootCoreNotInWorkspace(t *testing.T) {
	// core counted as efferent dep inflates instability — sysroot must be skipped
	// when core is not a workspace member (i.e., not in crateMap).
	currentCrate := &crateInfo{name: "myapp", dir: "myapp"}
	crateMap := map[directory]*crateInfo{
		"myapp": currentCrate,
		// note: no "core" entry — core is sysroot here
	}

	tests := map[string]struct {
		usePath  string
		wantSkip bool
	}{
		"sysroot core bare is skipped": {
			usePath:  "core",
			wantSkip: true,
		},
		"sysroot core nested is skipped": {
			usePath:  "core::fmt::Debug",
			wantSkip: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, _, skip := resolveUsePath(tt.usePath, currentCrate, crateMap, StrategyModule)
			require.Equal(t, tt.wantSkip, skip, "skip mismatch")
		})
	}
}

func TestGetUseItemName(t *testing.T) {
	tests := map[string]struct {
		usePath string
		want    string
	}{
		"simple": {
			usePath: "std::fmt",
			want:    "fmt",
		},
		"deep path": {
			usePath: "crate::config::Config",
			want:    "Config",
		},
		"single": {
			usePath: "serde",
			want:    "serde",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := getUseItemName(tt.usePath)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseUseList(t *testing.T) {
	tests := map[string]struct {
		input string
		want  []string
	}{
		"simple list": {
			input: "{HashMap, HashSet}",
			want:  []string{"HashMap", "HashSet"},
		},
		"with self": {
			input: "{self, Read, Write}",
			want:  []string{"Read", "Write"},
		},
		"single item": {
			input: "{Serialize}",
			want:  []string{"Serialize"},
		},
		"empty": {
			input: "{}",
			want:  nil,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := parseUseList(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseDeriveArgs(t *testing.T) {
	tests := map[string]struct {
		input string
		want  []string
	}{
		"single derive": {
			input: "(Serialize)",
			want:  []string{"Serialize"},
		},
		"multiple derives": {
			input: "(Serialize, Deserialize)",
			want:  []string{"Serialize", "Deserialize"},
		},
		"multiple derives with spaces": {
			input: "(Clone, Debug, Default)",
			want:  []string{"Clone", "Debug", "Default"},
		},
		"empty": {
			input: "()",
			want:  nil,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := parseDeriveArgs(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestExtractIdentifierFromExpr(t *testing.T) {
	tests := map[string]struct {
		expr string
		want string
	}{
		"scoped call": {
			expr: "Router::new",
			want: "Router",
		},
		"nested scoped": {
			expr: "fmt::format",
			want: "fmt",
		},
		"simple identifier": {
			expr: "Config",
			want: "Config",
		},
		"direct call": {
			expr: "handle",
			want: "handle",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := extractIdentifierFromExpr(tt.expr)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestBuildImportNameMap(t *testing.T) {
	currentCrate := &crateInfo{name: "myapp", dir: "myapp"}
	crateMap := map[directory]*crateInfo{
		"myapp": currentCrate,
		"core":  {name: "core", dir: "core"},
	}

	tests := map[string]struct {
		uses   []capturedUse
		assert func(t *testing.T, nameMap map[string]importNameInfo)
	}{
		// sysroot types absent from name map — HashMap::new not counted as Ce
		"should map simple imports": {
			uses: []capturedUse{
				{path: "crate::config::Config"},
				{path: "std::collections::HashMap"},
				{path: "core::service::Service"},
			},
			assert: func(t *testing.T, nameMap map[string]importNameInfo) {
				require.Contains(t, nameMap, "Config")
				require.Equal(t, "myapp::config", nameMap["Config"].pkg)
				require.Equal(t, "myapp::config::Config", nameMap["Config"].fullPath)

				// std is sysroot — skipped, must not appear in name map
				require.NotContains(t, nameMap, "HashMap")

				require.Contains(t, nameMap, "Service")
				require.Equal(t, "core::service", nameMap["Service"].pkg)
				require.Equal(t, "core::service::Service", nameMap["Service"].fullPath)
			},
		},
		// sysroot aliases absent from name map — sysroot types must not be counted as Ce via aliases
		"should map aliased imports by alias": {
			uses: []capturedUse{
				{path: "tokio::runtime::Runtime", alias: "TokioRuntime"},
				{path: "std::collections::HashMap", alias: "Map"},
			},
			assert: func(t *testing.T, nameMap map[string]importNameInfo) {
				require.NotContains(t, nameMap, "Runtime")
				require.Contains(t, nameMap, "TokioRuntime")
				require.Equal(t, "tokio::runtime", nameMap["TokioRuntime"].pkg)
				require.Equal(t, "tokio::runtime::Runtime", nameMap["TokioRuntime"].fullPath)

				// std is sysroot — alias must not appear either
				require.NotContains(t, nameMap, "HashMap")
				require.NotContains(t, nameMap, "Map")
			},
		},
		"should skip crate root items": {
			uses: []capturedUse{
				{path: "crate::RootItem"},
			},
			assert: func(t *testing.T, nameMap map[string]importNameInfo) {
				require.NotContains(t, nameMap, "RootItem")
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			nameMap := buildImportNameMap(tt.uses, currentCrate, crateMap, StrategyModule)
			tt.assert(t, nameMap)
		})
	}
}

func TestHydrateUsageExpressions(t *testing.T) {
	tests := map[string]struct {
		usages        []capturedUsage
		importedNames map[string]importNameInfo
		setup         func() *analyzer.PackageAnalysis
		assert        func(t *testing.T, pkgNode *analyzer.PackageAnalysis)
	}{
		"should track scoped call as coupling stat": {
			usages: []capturedUsage{
				{
					expr: "Router::new",
					pos:  analyzer.Position{File: "src/main.rs", Line: 10, ColStart: 5, ColEnd: 16},
				},
			},
			importedNames: map[string]importNameInfo{
				"Router": {pkg: "axum::routing", fullPath: "axum::routing::Router"},
			},
			setup: func() *analyzer.PackageAnalysis {
				tree := analyzer.NewPackageAnalysisTree()
				pkgNode := tree.Add("myapp")
				pkgNode.Out.Add("axum::routing")

				return pkgNode
			},
			assert: func(t *testing.T, pkgNode *analyzer.PackageAnalysis) {
				info, exists := pkgNode.Out.Get("axum::routing")
				require.True(t, exists)

				stats := info.CouplingStats()
				require.Contains(t, stats, "axum::routing::Router")
				require.Equal(t, uint(1), stats["axum::routing::Router"].Count)
				require.Equal(t, uint(10), stats["axum::routing::Router"].Positions[0].Line)
			},
		},
		"should track type usage as coupling stat": {
			usages: []capturedUsage{
				{
					expr: "Config",
					pos:  analyzer.Position{File: "src/main.rs", Line: 5, ColStart: 10, ColEnd: 16},
				},
			},
			importedNames: map[string]importNameInfo{
				"Config": {pkg: "myapp::config", fullPath: "myapp::config::Config"},
			},
			setup: func() *analyzer.PackageAnalysis {
				tree := analyzer.NewPackageAnalysisTree()
				pkgNode := tree.Add("myapp")
				pkgNode.Out.Add("myapp::config")

				return pkgNode
			},
			assert: func(t *testing.T, pkgNode *analyzer.PackageAnalysis) {
				info, exists := pkgNode.Out.Get("myapp::config")
				require.True(t, exists)

				stats := info.CouplingStats()
				require.Contains(t, stats, "myapp::config::Config")
				require.Equal(t, uint(1), stats["myapp::config::Config"].Count)
			},
		},
		"should track aliased import usage": {
			usages: []capturedUsage{
				{
					expr: "TokioRuntime",
					pos:  analyzer.Position{File: "src/main.rs", Line: 8, ColStart: 5, ColEnd: 17},
				},
			},
			importedNames: map[string]importNameInfo{
				"TokioRuntime": {pkg: "tokio::runtime", fullPath: "tokio::runtime::Runtime"},
			},
			setup: func() *analyzer.PackageAnalysis {
				tree := analyzer.NewPackageAnalysisTree()
				pkgNode := tree.Add("myapp")
				pkgNode.Out.Add("tokio::runtime")

				return pkgNode
			},
			assert: func(t *testing.T, pkgNode *analyzer.PackageAnalysis) {
				info, exists := pkgNode.Out.Get("tokio::runtime")
				require.True(t, exists)

				stats := info.CouplingStats()
				require.Contains(t, stats, "tokio::runtime::Runtime")
				require.Equal(t, uint(1), stats["tokio::runtime::Runtime"].Count)
			},
		},
		"should skip non-imported identifiers": {
			usages: []capturedUsage{
				{
					expr: "String",
					pos:  analyzer.Position{File: "src/main.rs", Line: 5, ColStart: 1, ColEnd: 7},
				},
				{
					expr: "println",
					pos:  analyzer.Position{File: "src/main.rs", Line: 6, ColStart: 1, ColEnd: 8},
				},
			},
			importedNames: map[string]importNameInfo{
				"Config": {pkg: "myapp::config", fullPath: "myapp::config::Config"},
			},
			setup: func() *analyzer.PackageAnalysis {
				tree := analyzer.NewPackageAnalysisTree()
				pkgNode := tree.Add("myapp")
				pkgNode.Out.Add("myapp::config")

				return pkgNode
			},
			assert: func(t *testing.T, pkgNode *analyzer.PackageAnalysis) {
				info, _ := pkgNode.Out.Get("myapp::config")
				require.Empty(t, info.CouplingStats())
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pkgNode := tt.setup()
			hydrateUsageExpressions(tt.usages, tt.importedNames, pkgNode)
			tt.assert(t, pkgNode)
		})
	}
}

func TestResolveInwardDependencies(t *testing.T) {
	tests := map[string]struct {
		setup  func(g *analyzer.PackageAnalysisTree)
		assert func(t *testing.T, g *analyzer.PackageAnalysisTree)
	}{
		"should add to in node when imported crate module exists in graph": {
			setup: func(g *analyzer.PackageAnalysisTree) {
				pkgA := g.Add("myapp")
				pkgA.Out.Add("myapp::config")
				g.Add("myapp::config")
			},
			assert: func(t *testing.T, g *analyzer.PackageAnalysisTree) {
				pkgConfig, _ := g.Get("myapp::config")
				_, depExists := pkgConfig.In.Get("myapp")
				require.True(t, depExists, "expected myapp to appear in myapp::config's in node")
			},
		},
		"should skip imports that do not exist in the graph": {
			setup: func(g *analyzer.PackageAnalysisTree) {
				pkgA := g.Add("myapp")
				pkgA.Out.Add("serde")
			},
			assert: func(t *testing.T, g *analyzer.PackageAnalysisTree) {
				pkgA, _ := g.Get("myapp")
				require.Empty(t, pkgA.In.GetChildren())
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

func TestParseUseListNested(t *testing.T) {
	tests := map[string]struct {
		input string
		want  []string
	}{
		"nested use list": {
			input: "{A, b::{C, D}}",
			want:  []string{"A", "b::C", "b::D"},
		},
		"deeply nested": {
			input: "{A, b::{C, d::{E, F}}, G}",
			want:  []string{"A", "b::C", "b::d::E", "b::d::F", "G"},
		},
		"nested with self": {
			input: "{self, io::{Read, Write}}",
			want:  []string{"io::Read", "io::Write"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := parseUseList(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}
