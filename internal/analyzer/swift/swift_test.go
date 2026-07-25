package swift

import (
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/stretchr/testify/require"
)

func TestGetModulePath(t *testing.T) {
	tests := map[string]struct {
		filePath string
		pi       *packageInfo
		want     string
	}{
		"standard SPM layout - Sources": {
			filePath: "Sources/MyTarget/Routes.swift",
			pi:       &packageInfo{name: "MyPackage", dir: "."},
			want:     "MyTarget",
		},
		"standard SPM layout - nested file": {
			filePath: "Sources/MyTarget/Controllers/HomeController.swift",
			pi:       &packageInfo{name: "MyPackage", dir: "."},
			want:     "MyTarget",
		},
		"standard SPM layout - Tests": {
			filePath: "Tests/MyTargetTests/MyTargetTests.swift",
			pi:       &packageInfo{name: "MyPackage", dir: "."},
			want:     "MyTargetTests",
		},
		"non-standard layout uses package name": {
			filePath: "src/main.swift",
			pi:       &packageInfo{name: "MyPackage", dir: "."},
			want:     "MyPackage",
		},
		"package in subdirectory": {
			filePath: "app/Sources/App/main.swift",
			pi:       &packageInfo{name: "App", dir: "app"},
			want:     "App",
		},
		"workspace member package": {
			filePath: "packages/Core/Sources/Core/Model.swift",
			pi:       &packageInfo{name: "Core", dir: "packages/Core"},
			want:     "Core",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := getModulePath(tt.filePath, tt.pi)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGetPackageForFile(t *testing.T) {
	tests := map[string]struct {
		filePath   string
		packageMap map[directory]*packageInfo
		wantName   packageName
		wantNil    bool
	}{
		"finds package in same directory": {
			filePath: "Sources/App/main.swift",
			packageMap: map[directory]*packageInfo{
				".": {name: "App", dir: "."},
			},
			wantName: "App",
		},
		"finds package in parent directory": {
			filePath: "Sources/App/Controllers/HomeController.swift",
			packageMap: map[directory]*packageInfo{
				".": {name: "App", dir: "."},
			},
			wantName: "App",
		},
		"finds workspace member package": {
			filePath: "packages/Core/Sources/Core/Model.swift",
			packageMap: map[directory]*packageInfo{
				"packages/Core": {name: "Core", dir: "packages/Core"},
			},
			wantName: "Core",
		},
		"returns nil when no package found": {
			filePath: "orphan/main.swift",
			packageMap: map[directory]*packageInfo{
				"app": {name: "App", dir: "app"},
			},
			wantNil: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := getPackageForFile(tt.filePath, tt.packageMap)
			if tt.wantNil {
				require.Nil(t, got)
			} else {
				require.NotNil(t, got)
				require.Equal(t, tt.wantName, got.name)
			}
		})
	}
}

func TestResolveImportPath(t *testing.T) {
	currentPackage := &packageInfo{
		name:    "MyApp",
		dir:     ".",
		targets: []string{"MyApp", "MyAppTests"},
	}
	packageMap := map[directory]*packageInfo{
		".": currentPackage,
		"packages/Core": {
			name:    "Core",
			dir:     "packages/Core",
			targets: []string{"Core", "CoreTests"},
		},
	}

	tests := map[string]struct {
		importModule string
		wantPkg      string
		wantInternal bool
	}{
		"Foundation is external": {
			importModule: "Foundation",
			wantPkg:      "Foundation",
			wantInternal: false,
		},
		"UIKit is external": {
			importModule: "UIKit",
			wantPkg:      "UIKit",
			wantInternal: false,
		},
		"SwiftUI is external": {
			importModule: "SwiftUI",
			wantPkg:      "SwiftUI",
			wantInternal: false,
		},
		"workspace target is internal": {
			importModule: "Core",
			wantPkg:      "Core",
			wantInternal: true,
		},
		"own target is internal": {
			importModule: "MyApp",
			wantPkg:      "MyApp",
			wantInternal: true,
		},
		"external SPM dependency": {
			importModule: "Alamofire",
			wantPkg:      "Alamofire",
			wantInternal: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pkg, isInternal := resolveImportPath(tt.importModule, packageMap)
			require.Equal(t, tt.wantPkg, pkg)
			require.Equal(t, tt.wantInternal, isInternal)
		})
	}
}

func TestBuildImportNameMap(t *testing.T) {
	currentPackage := &packageInfo{
		name:    "MyApp",
		dir:     ".",
		targets: []string{"MyApp"},
	}
	packageMap := map[directory]*packageInfo{
		".":    currentPackage,
		"Core": {name: "Core", dir: "Core", targets: []string{"Core"}},
	}

	tests := map[string]struct {
		imports       []capturedImport
		moduleExports map[string]map[string]struct{}
		assert        func(t *testing.T, nameMap map[string]importNameInfo)
	}{
		"should map external framework": {
			imports: []capturedImport{
				{module: "Foundation"},
				{module: "UIKit"},
			},
			moduleExports: nil,
			assert: func(t *testing.T, nameMap map[string]importNameInfo) {
				require.Contains(t, nameMap, "Foundation")
				require.Equal(t, "Foundation", nameMap["Foundation"].pkg)

				require.Contains(t, nameMap, "UIKit")
				require.Equal(t, "UIKit", nameMap["UIKit"].pkg)

				// Framework types are not mapped — coupling to frameworks
				// is not meaningful to measure
				require.NotContains(t, nameMap, "URL")
				require.NotContains(t, nameMap, "UIViewController")
			},
		},
		"should map internal target": {
			imports: []capturedImport{
				{module: "Core"},
			},
			moduleExports: nil,
			assert: func(t *testing.T, nameMap map[string]importNameInfo) {
				require.Contains(t, nameMap, "Core")
				require.Equal(t, "Core", nameMap["Core"].pkg)
			},
		},
		"should map exported symbols from internal modules": {
			imports: []capturedImport{
				{module: "Core"},
			},
			moduleExports: map[string]map[string]struct{}{
				"Core": {"User": {}, "UserService": {}},
			},
			assert: func(t *testing.T, nameMap map[string]importNameInfo) {
				require.Contains(t, nameMap, "Core")
				require.Equal(t, "Core", nameMap["Core"].pkg)

				require.Contains(t, nameMap, "User")
				require.Equal(t, "Core", nameMap["User"].pkg)
				require.Equal(t, "Core.User", nameMap["User"].fullPath)

				require.Contains(t, nameMap, "UserService")
				require.Equal(t, "Core", nameMap["UserService"].pkg)
				require.Equal(t, "Core.UserService", nameMap["UserService"].fullPath)
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			nameMap := buildImportNameMap(tt.imports, packageMap, tt.moduleExports)
			tt.assert(t, nameMap)
		})
	}
}

func TestHydrateUsageExpressions(t *testing.T) {
	tests := map[string]struct {
		usages      []capturedUsage
		importNames map[string]importNameInfo
		setup       func() *analyzer.PackageAnalysis
		assert      func(t *testing.T, pkgNode *analyzer.PackageAnalysis)
	}{
		"should track internal module type usage": {
			usages: []capturedUsage{
				{
					expr: "User",
					pos:  analyzer.Position{File: "main.swift", Line: 5, ColStart: 16, ColEnd: 20},
				},
				{
					expr: "UserService",
					pos:  analyzer.Position{File: "main.swift", Line: 4, ColStart: 15, ColEnd: 26},
				},
			},
			importNames: map[string]importNameInfo{
				"Core":        {pkg: "Core", fullPath: "Core"},
				"User":        {pkg: "Core", fullPath: "Core.User"},
				"UserService": {pkg: "Core", fullPath: "Core.UserService"},
			},
			setup: func() *analyzer.PackageAnalysis {
				tree := analyzer.NewPackageAnalysisTree()
				pkgNode := tree.Add("App")
				pkgNode.Out.Add("Core")

				return pkgNode
			},
			assert: func(t *testing.T, pkgNode *analyzer.PackageAnalysis) {
				info, exists := pkgNode.Out.Get("Core")
				require.True(t, exists)

				stats := info.CouplingStats()
				require.Contains(t, stats, "Core.User")
				require.Equal(t, uint(1), stats["Core.User"].Count)
				require.Contains(t, stats, "Core.UserService")
				require.Equal(t, uint(1), stats["Core.UserService"].Count)
			},
		},
		"should deduplicate usages at same position": {
			usages: []capturedUsage{
				{
					expr: "User",
					pos:  analyzer.Position{File: "main.swift", Line: 10, ColStart: 5, ColEnd: 9},
				},
				{
					expr: "User",
					pos:  analyzer.Position{File: "main.swift", Line: 10, ColStart: 5, ColEnd: 9},
				},
			},
			importNames: map[string]importNameInfo{
				"Core": {pkg: "Core", fullPath: "Core"},
				"User": {pkg: "Core", fullPath: "Core.User"},
			},
			setup: func() *analyzer.PackageAnalysis {
				tree := analyzer.NewPackageAnalysisTree()
				pkgNode := tree.Add("MyApp")
				pkgNode.Out.Add("Core")

				return pkgNode
			},
			assert: func(t *testing.T, pkgNode *analyzer.PackageAnalysis) {
				info, exists := pkgNode.Out.Get("Core")
				require.True(t, exists)

				stats := info.CouplingStats()
				require.Contains(t, stats, "Core.User")
				require.Equal(t, uint(1), stats["Core.User"].Count)
			},
		},
		"should skip non-imported types": {
			usages: []capturedUsage{
				{
					expr: "MyCustomType",
					pos:  analyzer.Position{File: "main.swift", Line: 5, ColStart: 1, ColEnd: 12},
				},
			},
			importNames: map[string]importNameInfo{
				"Core": {pkg: "Core", fullPath: "Core"},
				"User": {pkg: "Core", fullPath: "Core.User"},
			},
			setup: func() *analyzer.PackageAnalysis {
				tree := analyzer.NewPackageAnalysisTree()
				pkgNode := tree.Add("MyApp")
				pkgNode.Out.Add("Core")

				return pkgNode
			},
			assert: func(t *testing.T, pkgNode *analyzer.PackageAnalysis) {
				info, _ := pkgNode.Out.Get("Core")
				require.Empty(t, info.CouplingStats())
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pkgNode := tt.setup()
			hydrateUsageExpressions(tt.usages, tt.importNames, pkgNode)
			tt.assert(t, pkgNode)
		})
	}
}

func TestResolveInwardDependencies(t *testing.T) {
	tests := map[string]struct {
		setup  func(g *analyzer.PackageAnalysisTree)
		assert func(t *testing.T, g *analyzer.PackageAnalysisTree)
	}{
		"should add to in node when imported target exists in graph": {
			setup: func(g *analyzer.PackageAnalysisTree) {
				pkgApp := g.Add("App")
				pkgApp.Out.Add("Core")
				g.Add("Core")
			},
			assert: func(t *testing.T, g *analyzer.PackageAnalysisTree) {
				pkgCore, _ := g.Get("Core")
				_, depExists := pkgCore.In.Get("App")
				require.True(t, depExists, "expected App to appear in Core's in node")
			},
		},
		"should skip imports that do not exist in the graph": {
			setup: func(g *analyzer.PackageAnalysisTree) {
				pkgApp := g.Add("App")
				pkgApp.Out.Add("Foundation")
			},
			assert: func(t *testing.T, g *analyzer.PackageAnalysisTree) {
				pkgApp, _ := g.Get("App")
				require.Empty(t, pkgApp.In.GetChildren())
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
