package python

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
		"__init__.py is package root": {
			filePath: "myapp/__init__.py",
			pi:       &packageInfo{name: "myapp", dir: "."},
			want:     "myapp",
		},
		"module file": {
			filePath: "myapp/config.py",
			pi:       &packageInfo{name: "myapp", dir: "."},
			want:     "myapp.config",
		},
		"nested module file": {
			filePath: "myapp/handlers/auth.py",
			pi:       &packageInfo{name: "myapp", dir: "."},
			want:     "myapp.handlers.auth",
		},
		"nested __init__.py": {
			filePath: "myapp/handlers/__init__.py",
			pi:       &packageInfo{name: "myapp", dir: "."},
			want:     "myapp.handlers",
		},
		"src layout package root": {
			filePath: "src/myapp/__init__.py",
			pi:       &packageInfo{name: "myapp", dir: ".", srcDir: "src"},
			want:     "myapp",
		},
		"src layout module": {
			filePath: "src/myapp/config.py",
			pi:       &packageInfo{name: "myapp", dir: ".", srcDir: "src"},
			want:     "myapp.config",
		},
		"src layout nested module": {
			filePath: "src/myapp/handlers/auth.py",
			pi:       &packageInfo{name: "myapp", dir: ".", srcDir: "src"},
			want:     "myapp.handlers.auth",
		},
		"package in subdirectory": {
			filePath: "packages/core/core/__init__.py",
			pi:       &packageInfo{name: "core", dir: "packages/core"},
			want:     "core",
		},
		"package in subdirectory module": {
			filePath: "packages/core/core/models.py",
			pi:       &packageInfo{name: "core", dir: "packages/core"},
			want:     "core.models",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := getModulePath(tt.filePath, tt.pi, StrategyModule)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGetModulePathWithStrategy(t *testing.T) {
	tests := map[string]struct {
		filePath string
		pi       *packageInfo
		strategy BoundaryStrategy
		want     string
	}{
		// StrategyModule — per-.py-file granularity (existing behavior)
		"module/flat module file": {
			filePath: "myapp/config.py",
			pi:       &packageInfo{name: "myapp", dir: "."},
			strategy: StrategyModule,
			want:     "myapp.config",
		},
		"module/nested module file": {
			filePath: "myapp/handlers/auth.py",
			pi:       &packageInfo{name: "myapp", dir: "."},
			strategy: StrategyModule,
			want:     "myapp.handlers.auth",
		},
		"module/__init__.py is package root": {
			filePath: "myapp/__init__.py",
			pi:       &packageInfo{name: "myapp", dir: "."},
			strategy: StrategyModule,
			want:     "myapp",
		},
		"module/src layout nested": {
			filePath: "src/myapp/handlers/auth.py",
			pi:       &packageInfo{name: "myapp", dir: ".", srcDir: "src"},
			strategy: StrategyModule,
			want:     "myapp.handlers.auth",
		},
		"module/monorepo subdirectory": {
			filePath: "packages/core/core/models.py",
			pi:       &packageInfo{name: "core", dir: "packages/core"},
			strategy: StrategyModule,
			want:     "core.models",
		},

		// StrategyPackage — collapse everything to top-level package name
		"package/flat module file": {
			filePath: "myapp/config.py",
			pi:       &packageInfo{name: "myapp", dir: "."},
			strategy: StrategyPackage,
			want:     "myapp",
		},
		"package/nested module file": {
			filePath: "myapp/handlers/auth.py",
			pi:       &packageInfo{name: "myapp", dir: "."},
			strategy: StrategyPackage,
			want:     "myapp",
		},
		"package/__init__.py": {
			filePath: "myapp/__init__.py",
			pi:       &packageInfo{name: "myapp", dir: "."},
			strategy: StrategyPackage,
			want:     "myapp",
		},
		"package/src layout nested": {
			filePath: "src/myapp/handlers/auth.py",
			pi:       &packageInfo{name: "myapp", dir: ".", srcDir: "src"},
			strategy: StrategyPackage,
			want:     "myapp",
		},
		"package/monorepo subdirectory": {
			filePath: "packages/core/core/models.py",
			pi:       &packageInfo{name: "core", dir: "packages/core"},
			strategy: StrategyPackage,
			want:     "core",
		},

		// StrategySubpackage — collapse to directory-package level
		"subpackage/flat module file": {
			filePath: "myapp/config.py",
			pi:       &packageInfo{name: "myapp", dir: "."},
			strategy: StrategySubpackage,
			want:     "myapp",
		},
		"subpackage/nested module in subpackage": {
			filePath: "myapp/handlers/auth.py",
			pi:       &packageInfo{name: "myapp", dir: "."},
			strategy: StrategySubpackage,
			want:     "myapp.handlers",
		},
		"subpackage/__init__.py at root": {
			filePath: "myapp/__init__.py",
			pi:       &packageInfo{name: "myapp", dir: "."},
			strategy: StrategySubpackage,
			want:     "myapp",
		},
		"subpackage/nested __init__.py": {
			filePath: "myapp/handlers/__init__.py",
			pi:       &packageInfo{name: "myapp", dir: "."},
			strategy: StrategySubpackage,
			want:     "myapp.handlers",
		},
		"subpackage/deeply nested module": {
			filePath: "myapp/handlers/v2/auth.py",
			pi:       &packageInfo{name: "myapp", dir: "."},
			strategy: StrategySubpackage,
			want:     "myapp.handlers.v2",
		},
		"subpackage/src layout nested": {
			filePath: "src/myapp/handlers/auth.py",
			pi:       &packageInfo{name: "myapp", dir: ".", srcDir: "src"},
			strategy: StrategySubpackage,
			want:     "myapp.handlers",
		},
		"subpackage/src layout root module": {
			filePath: "src/myapp/config.py",
			pi:       &packageInfo{name: "myapp", dir: ".", srcDir: "src"},
			strategy: StrategySubpackage,
			want:     "myapp",
		},
		"subpackage/monorepo nested module": {
			filePath: "packages/core/core/service.py",
			pi:       &packageInfo{name: "core", dir: "packages/core"},
			strategy: StrategySubpackage,
			want:     "core",
		},
		"subpackage/monorepo deeper module": {
			filePath: "packages/core/core/db/models.py",
			pi:       &packageInfo{name: "core", dir: "packages/core"},
			strategy: StrategySubpackage,
			want:     "core.db",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := getModulePath(tt.filePath, tt.pi, tt.strategy)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultStrategyIsModule(t *testing.T) {
	a := PythonAnalyzer()
	require.Equal(t, StrategyModule, a.strategy)
}

func TestGetPackageForFile(t *testing.T) {
	tests := map[string]struct {
		filePath string
		pkgMap   map[directory]*packageInfo
		wantName packageName
		wantNil  bool
	}{
		"finds package in same directory": {
			filePath: "myapp/config.py",
			pkgMap: map[directory]*packageInfo{
				".": {name: "myapp", dir: "."},
			},
			wantName: "myapp",
		},
		"finds package in parent directory": {
			filePath: "myapp/handlers/auth.py",
			pkgMap: map[directory]*packageInfo{
				".": {name: "myapp", dir: "."},
			},
			wantName: "myapp",
		},
		"returns nil when no package found": {
			filePath: "orphan/src/main.py",
			pkgMap: map[directory]*packageInfo{
				"other": {name: "other", dir: "other"},
			},
			wantNil: true,
		},
		"finds package in monorepo": {
			filePath: "packages/core/core/models.py",
			pkgMap: map[directory]*packageInfo{
				"packages/core": {name: "core", dir: "packages/core"},
				"packages/api":  {name: "api", dir: "packages/api"},
			},
			wantName: "core",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := getPackageForFile(tt.filePath, tt.pkgMap)
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
	currentPkg := &packageInfo{name: "myapp", dir: "."}

	tests := map[string]struct {
		imp        capturedImport
		currModule string
		isInitFile bool
		wantPkg    string
		wantIntrnl bool
		wantSkip   bool
	}{
		"internal module import": {
			imp: capturedImport{
				module: "myapp.config",
				isFrom: true,
				names:  []string{"Config"},
			},
			currModule: "myapp",
			wantPkg:    "myapp.config",
			wantIntrnl: true,
		},
		"internal nested module": {
			imp: capturedImport{
				module: "myapp.handlers.auth",
				isFrom: true,
				names:  []string{"AuthHandler"},
			},
			currModule: "myapp.routes",
			wantPkg:    "myapp.handlers.auth",
			wantIntrnl: true,
		},
		"from package import submodule": {
			// from myapp import utils -> dependency on myapp.utils
			imp: capturedImport{
				module: "myapp",
				isFrom: true,
				names:  []string{"utils"},
			},
			currModule: "myapp",
			wantPkg:    "myapp.utils",
			wantIntrnl: true,
		},
		"import package bare": {
			// import myapp -> dependency on myapp (not a from-import)
			imp: capturedImport{
				module: "myapp",
				isFrom: false,
			},
			currModule: "myapp.routes",
			wantPkg:    "myapp",
			wantIntrnl: true,
		},
		"external stdlib import": {
			imp:        capturedImport{module: "os", isFrom: false},
			currModule: "myapp",
			wantPkg:    "os",
			wantIntrnl: false,
		},
		"external stdlib from import": {
			imp:        capturedImport{module: "os.path", isFrom: true, names: []string{"join"}},
			currModule: "myapp",
			wantPkg:    "os.path",
			wantIntrnl: false,
		},
		"external package import": {
			imp:        capturedImport{module: "requests", isFrom: false},
			currModule: "myapp",
			wantPkg:    "requests",
			wantIntrnl: false,
		},
		"relative import single dot from submodule": {
			// from . import config in myapp/handlers/auth.py -> myapp.handlers.config
			imp: capturedImport{
				module:   "",
				isFrom:   true,
				names:    []string{"config"},
				relLevel: 1,
			},
			currModule: "myapp.handlers.auth",
			wantPkg:    "myapp.handlers",
			wantIntrnl: true,
		},
		"relative import double dot": {
			// from .. import utils in myapp/handlers/auth.py -> myapp.utils
			imp: capturedImport{
				module:   "",
				isFrom:   true,
				names:    []string{"utils"},
				relLevel: 2,
			},
			currModule: "myapp.handlers.auth",
			wantPkg:    "myapp",
			wantIntrnl: true,
		},
		"relative import with module": {
			// from .models import User in myapp/handlers/auth.py -> myapp.handlers.models.User
			imp: capturedImport{
				module:   "models",
				isFrom:   true,
				names:    []string{"User"},
				relLevel: 1,
			},
			currModule: "myapp.handlers.auth",
			wantPkg:    "myapp.handlers.models",
			wantIntrnl: true,
		},
		"relative import with alias": {
			// from ..services.auth import verify as check_auth in myapp/api/routes.py
			imp: capturedImport{
				module:   "services.auth",
				isFrom:   true,
				names:    []string{"verify"},
				alias:    "check_auth",
				relLevel: 2,
			},
			currModule: "myapp.api.routes",
			wantPkg:    "myapp.services.auth",
			wantIntrnl: true,
		},
		"relative import from init file single dot": {
			// from .routes import get_users in myapp/api/__init__.py -> myapp.api.routes
			imp: capturedImport{
				module:   "routes",
				isFrom:   true,
				names:    []string{"get_users"},
				relLevel: 1,
			},
			currModule: "myapp.api",
			isInitFile: true,
			wantPkg:    "myapp.api.routes",
			wantIntrnl: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pkg, isInternal, skip := resolveImportPath(
				tt.imp, tt.currModule, currentPkg, tt.isInitFile,
			)
			require.Equal(t, tt.wantSkip, skip, "skip mismatch")

			if !skip {
				require.Equal(t, tt.wantPkg, pkg)
				require.Equal(t, tt.wantIntrnl, isInternal)
			}
		})
	}
}

func TestResolveRelativeImport(t *testing.T) {
	tests := map[string]struct {
		imp          capturedImport
		currentMod   string
		isInitFile   bool
		wantResolved string
	}{
		"single dot same package": {
			// from . import config in myapp/handlers/auth.py -> myapp.handlers
			imp:          capturedImport{module: "", relLevel: 1, names: []string{"config"}},
			currentMod:   "myapp.handlers.auth",
			wantResolved: "myapp.handlers",
		},
		"single dot with module": {
			// from .config import Config in myapp/handlers/auth.py -> myapp.handlers.config
			imp:          capturedImport{module: "config", relLevel: 1, names: []string{"Config"}},
			currentMod:   "myapp.handlers.auth",
			wantResolved: "myapp.handlers.config",
		},
		"double dot parent": {
			// from .. import models in myapp/handlers/auth.py -> myapp
			imp:          capturedImport{module: "", relLevel: 2, names: []string{"models"}},
			currentMod:   "myapp.handlers.auth",
			wantResolved: "myapp",
		},
		"double dot with module": {
			// from ..models import User in myapp/handlers/auth.py -> myapp.models
			imp:          capturedImport{module: "models", relLevel: 2, names: []string{"User"}},
			currentMod:   "myapp.handlers.auth",
			wantResolved: "myapp.models",
		},
		"triple dot": {
			// from ...utils import helper in myapp/handlers/auth/oauth.py -> myapp.utils
			imp:          capturedImport{module: "utils", relLevel: 3, names: []string{"helper"}},
			currentMod:   "myapp.handlers.auth.oauth",
			wantResolved: "myapp.utils",
		},
		"invalid relative (too many dots)": {
			imp:          capturedImport{module: "", relLevel: 5, names: []string{"something"}},
			currentMod:   "myapp.handlers",
			wantResolved: "",
		},
		"init file single dot same package": {
			// from .routes import get_users in webapp/api/__init__.py -> webapp.api.routes
			imp: capturedImport{
				module:   "routes",
				relLevel: 1,
				names:    []string{"get_users"},
			},
			currentMod:   "webapp.api",
			isInitFile:   true,
			wantResolved: "webapp.api.routes",
		},
		"init file single dot no module": {
			// from . import utils in webapp/api/__init__.py -> webapp.api
			imp:          capturedImport{module: "", relLevel: 1, names: []string{"utils"}},
			currentMod:   "webapp.api",
			isInitFile:   true,
			wantResolved: "webapp.api",
		},
		"init file double dot parent": {
			// from .. import models in webapp/api/__init__.py -> webapp
			imp:          capturedImport{module: "", relLevel: 2, names: []string{"models"}},
			currentMod:   "webapp.api",
			isInitFile:   true,
			wantResolved: "webapp",
		},
		"init file double dot with module": {
			// from ..models import User in webapp/api/__init__.py -> webapp.models
			imp:          capturedImport{module: "models", relLevel: 2, names: []string{"User"}},
			currentMod:   "webapp.api",
			isInitFile:   true,
			wantResolved: "webapp.models",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := resolveRelativeImport(tt.imp, tt.currentMod, tt.isInitFile)
			require.Equal(t, tt.wantResolved, got)
		})
	}
}

func TestExtractDependencyName(t *testing.T) {
	tests := map[string]struct {
		input string
		want  string
	}{
		"simple": {
			input: "requests",
			want:  "requests",
		},
		"with version": {
			input: "requests>=2.0",
			want:  "requests",
		},
		"with extras": {
			input: "requests[security]",
			want:  "requests",
		},
		"with version and extras": {
			input: "requests[security]>=2.0",
			want:  "requests",
		},
		"url dependency": {
			input: "mypkg @ https://example.com/pkg.tar.gz",
			want:  "mypkg",
		},
		"with hyphen": {
			input: "my-package>=1.0",
			want:  "my_package",
		},
		"with environment marker": {
			input: "pywin32 ; sys_platform == 'win32'",
			want:  "pywin32",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := extractDependencyName(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestExtractIdentifierFromExpr(t *testing.T) {
	tests := map[string]struct {
		expr string
		want string
	}{
		"simple identifier": {
			expr: "Config",
			want: "Config",
		},
		"dotted access": {
			expr: "os.path",
			want: "os",
		},
		"method call": {
			expr: "config.get",
			want: "config",
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
	currentPkg := &packageInfo{name: "myapp", dir: "."}

	tests := map[string]struct {
		imports []capturedImport
		assert  func(t *testing.T, nameMap map[string]importNameInfo)
	}{
		"should map from import": {
			imports: []capturedImport{
				{module: "myapp.config", names: []string{"Config"}, isFrom: true},
				{module: "os.path", names: []string{"join"}, isFrom: true},
			},
			assert: func(t *testing.T, nameMap map[string]importNameInfo) {
				require.Contains(t, nameMap, "Config")
				require.Equal(t, "myapp.config", nameMap["Config"].pkg)
				require.Equal(t, "myapp.config.Config", nameMap["Config"].fullPath)

				require.Contains(t, nameMap, "join")
				require.Equal(t, "os.path", nameMap["join"].pkg)
				require.Equal(t, "os.path.join", nameMap["join"].fullPath)
			},
		},
		"should map from package import submodule": {
			imports: []capturedImport{
				{module: "myapp", names: []string{"utils"}, isFrom: true},
			},
			assert: func(t *testing.T, nameMap map[string]importNameInfo) {
				require.Contains(t, nameMap, "utils")
				require.Equal(t, "myapp.utils", nameMap["utils"].pkg)
				require.Equal(t, "myapp.utils", nameMap["utils"].fullPath)
			},
		},
		"should map aliased imports": {
			imports: []capturedImport{
				{module: "numpy", alias: "np", isFrom: false},
				{module: "pandas", alias: "pd", isFrom: false},
			},
			assert: func(t *testing.T, nameMap map[string]importNameInfo) {
				require.Contains(t, nameMap, "np")
				require.Equal(t, "numpy", nameMap["np"].pkg)

				require.Contains(t, nameMap, "pd")
				require.Equal(t, "pandas", nameMap["pd"].pkg)
			},
		},
		"should map simple import": {
			imports: []capturedImport{
				{module: "os", isFrom: false},
				{module: "sys", isFrom: false},
			},
			assert: func(t *testing.T, nameMap map[string]importNameInfo) {
				require.Contains(t, nameMap, "os")
				require.Equal(t, "os", nameMap["os"].pkg)

				require.Contains(t, nameMap, "sys")
				require.Equal(t, "sys", nameMap["sys"].pkg)
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			nameMap := buildImportNameMap(tt.imports, "myapp", currentPkg, false)
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
		"should track function call as coupling stat": {
			usages: []capturedUsage{
				{
					expr: "Config",
					pos: analyzer.Position{
						File:     "myapp/main.py",
						Line:     10,
						ColStart: 5,
						ColEnd:   11,
					},
				},
			},
			importedNames: map[string]importNameInfo{
				"Config": {pkg: "myapp.config", fullPath: "myapp.config.Config"},
			},
			setup: func() *analyzer.PackageAnalysis {
				tree := analyzer.NewPackageAnalysisTree()
				pkgNode := tree.Add("myapp")
				pkgNode.Out.Add("myapp.config")

				return pkgNode
			},
			assert: func(t *testing.T, pkgNode *analyzer.PackageAnalysis) {
				info, exists := pkgNode.Out.Get("myapp.config")
				require.True(t, exists)

				stats := info.CouplingStats()
				require.Contains(t, stats, "myapp.config.Config")
				require.Equal(t, uint(1), stats["myapp.config.Config"].Count)
				require.Equal(t, uint(10), stats["myapp.config.Config"].Positions[0].Line)
			},
		},
		"should track aliased import usage": {
			usages: []capturedUsage{
				{
					expr: "np.array",
					pos: analyzer.Position{
						File:     "myapp/data.py",
						Line:     5,
						ColStart: 10,
						ColEnd:   18,
					},
				},
			},
			importedNames: map[string]importNameInfo{
				"np": {pkg: "numpy", fullPath: "numpy"},
			},
			setup: func() *analyzer.PackageAnalysis {
				tree := analyzer.NewPackageAnalysisTree()
				pkgNode := tree.Add("myapp.data")
				pkgNode.Out.Add("numpy")

				return pkgNode
			},
			assert: func(t *testing.T, pkgNode *analyzer.PackageAnalysis) {
				info, exists := pkgNode.Out.Get("numpy")
				require.True(t, exists)

				stats := info.CouplingStats()
				require.Contains(t, stats, "numpy")
				require.Equal(t, uint(1), stats["numpy"].Count)
			},
		},
		"should skip non-imported identifiers": {
			usages: []capturedUsage{
				{
					expr: "str",
					pos:  analyzer.Position{File: "myapp/main.py", Line: 5, ColStart: 1, ColEnd: 4},
				},
				{
					expr: "print",
					pos:  analyzer.Position{File: "myapp/main.py", Line: 6, ColStart: 1, ColEnd: 6},
				},
			},
			importedNames: map[string]importNameInfo{
				"Config": {pkg: "myapp.config", fullPath: "myapp.config.Config"},
			},
			setup: func() *analyzer.PackageAnalysis {
				tree := analyzer.NewPackageAnalysisTree()
				pkgNode := tree.Add("myapp")
				pkgNode.Out.Add("myapp.config")

				return pkgNode
			},
			assert: func(t *testing.T, pkgNode *analyzer.PackageAnalysis) {
				info, _ := pkgNode.Out.Get("myapp.config")
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
		"should add to in node when imported module exists in graph": {
			setup: func(g *analyzer.PackageAnalysisTree) {
				pkgA := g.Add("myapp")
				pkgA.Out.Add("myapp.config")
				g.Add("myapp.config")
			},
			assert: func(t *testing.T, g *analyzer.PackageAnalysisTree) {
				pkgConfig, _ := g.Get("myapp.config")
				_, depExists := pkgConfig.In.Get("myapp")
				require.True(t, depExists, "expected myapp to appear in myapp.config's in node")
			},
		},
		"should skip imports that do not exist in the graph": {
			setup: func(g *analyzer.PackageAnalysisTree) {
				pkgA := g.Add("myapp")
				pkgA.Out.Add("requests")
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
