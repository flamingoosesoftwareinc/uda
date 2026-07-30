// Package python implements the Python source analyzer (manifest + module + import tracking).
package python

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	files "github.com/flamingoosesoftwareinc/fsift"
	"github.com/flamingoosesoftwareinc/slogerr"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/logschema"
	"github.com/flamingoosesoftwareinc/uda/internal/ts"
	"github.com/pelletier/go-toml/v2"
	treesitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// BoundaryStrategy selects the granularity at which Python packages are grouped.
type BoundaryStrategy string

// Boundary strategies select the granularity at which packages are grouped.
const (
	StrategyModule     BoundaryStrategy = "module"
	StrategyPackage    BoundaryStrategy = "package"
	StrategySubpackage BoundaryStrategy = "subpackage"
)

// srcLayoutDir is the conventional "src" directory used by PEP 517/518 src-layout
// projects. Hoisted so the magic literal appears once at the layout-detection
// site rather than being repeated across helpers.
const srcLayoutDir = "src"

// Option configures a pythonAnalyzer.
type Option func(*pythonAnalyzer)

// WithBoundaryStrategy sets the package-boundary strategy for the analyzer.
func WithBoundaryStrategy(s BoundaryStrategy) Option {
	return func(a *pythonAnalyzer) {
		switch s {
		case StrategyModule, StrategyPackage, StrategySubpackage:
			a.strategy = s
		default:
			warnUnknownStrategy(string(s))
		}
	}
}

// warnUnknownStrategy logs the unknown-strategy fallback. Called from the
// Option closure at constructor time before any caller context exists.
func warnUnknownStrategy(s string) {
	slog.LogAttrs(
		context.Background(),
		slog.LevelWarn,
		"unknown boundary strategy, falling back to default",
		logschema.UdaAnalyzerStrategy(s),
	)
}

type pythonAnalyzer struct {
	strategy   BoundaryStrategy
	boundaries []analyzer.PackageBoundary
}

var (
	_ analyzer.Analyzer         = &pythonAnalyzer{}
	_ analyzer.BoundaryProvider = &pythonAnalyzer{}
)

// PythonAnalyzer returns a Python source analyzer (satisfies
// analyzer.Analyzer and analyzer.BoundaryProvider). Concrete pointer return
// so tests can call Boundaries without a type assertion; cmd uses the
// interface upcast.
//
//nolint:revive // unexported-return: the concrete type is intentionally package-private; consumers upcast to analyzer.Analyzer / BoundaryProvider.
func PythonAnalyzer(opts ...Option) *pythonAnalyzer {
	a := &pythonAnalyzer{strategy: StrategyModule}
	for _, opt := range opts {
		opt(a)
	}

	return a
}

func (p *pythonAnalyzer) Name() string { return "Python" }

func (p *pythonAnalyzer) Analyze(ctx context.Context, dir fs.FS) ([]analyzer.Metrics, error) {
	packageFiles, err := listPackageFiles(ctx, dir)
	if err != nil {
		return nil, err
	}

	slog.LogAttrs(ctx, slog.LevelDebug, "found package files",
		logschema.UdaAnalyzerFilepaths(packageFiles),
	)

	var packageMap map[directory]*packageInfo
	if len(packageFiles) > 0 {
		packageMap = extractPackageInfo(ctx, dir, packageFiles)
	} else {
		// No manifest files found - auto-detect packages from directory structure
		packageMap, err = detectPackagesFromStructure(ctx, dir)
		if err != nil {
			return nil, err
		}
	}

	slog.DebugContext(ctx, "identified package info", "packages", packageMap)

	metrics, boundaries, err := analyzePythonMetrics(ctx, dir, packageMap, p.strategy)
	if err != nil {
		return nil, err
	}

	p.boundaries = boundaries

	return metrics, nil
}

// Boundaries returns the filesystem directories for each analyzed package.
// Lazily invokes Analyze() if boundaries have not been populated yet.
func (p *pythonAnalyzer) Boundaries(
	ctx context.Context,
	dir fs.FS,
) ([]analyzer.PackageBoundary, error) {
	if p.boundaries == nil {
		if _, err := p.Analyze(ctx, dir); err != nil {
			return nil, err
		}
	}

	return p.boundaries, nil
}

// Phase 1: Module boundary detection via pyproject.toml, setup.py, setup.cfg

type (
	directory   string
	packageName string
)

type packageInfo struct {
	name         packageName
	dir          directory
	srcDir       string              // "src" for src-layout, "" for flat layout
	dependencies map[string]struct{} // external dependency names
	subpackages  map[string]string   // maps subpackage name to its relative path
}

func listPackageFiles(ctx context.Context, dir fs.FS) ([]string, error) {
	return files.ListFiles(
		ctx,
		dir,
		files.SkipHiddenDirs(),
		files.SkipHiddenFiles(),
		skipPythonBuildDirs(),
		packageFileFilter(),
	)
}

func packageFileFilter() files.FileFilter {
	return func(path string, d fs.DirEntry) bool {
		if d.IsDir() {
			return false
		}

		base := filepath.Base(path)

		return base != "pyproject.toml" && base != "setup.py" && base != "setup.cfg"
	}
}

func skipPythonBuildDirs() files.FileFilter {
	skipDirs := map[string]struct{}{
		"__pycache__":    {},
		".venv":          {},
		"venv":           {},
		".tox":           {},
		".nox":           {},
		".pytest_cache":  {},
		".mypy_cache":    {},
		"dist":           {},
		"build":          {},
		"*.egg-info":     {},
		".eggs":          {},
		"node_modules":   {},
		".ruff_cache":    {},
		"__pypackages__": {},
	}

	return func(_ string, d fs.DirEntry) bool {
		if !d.IsDir() {
			return false
		}

		name := d.Name()
		if _, ok := skipDirs[name]; ok {
			return true
		}
		// Handle *.egg-info directories
		if strings.HasSuffix(name, ".egg-info") {
			return true
		}

		return false
	}
}

// pyprojectTomlPartial represents the subset of pyproject.toml we care about.
type pyprojectTomlPartial struct {
	Project struct {
		Name         string   `toml:"name"`
		Dependencies []string `toml:"dependencies"`
	} `toml:"project"`
	Tool struct {
		Poetry struct {
			Name         string         `toml:"name"`
			Dependencies map[string]any `toml:"dependencies"`
		} `toml:"poetry"`
		Setuptools struct {
			Packages []string `toml:"packages"`
		} `toml:"setuptools"`
	} `toml:"tool"`
}

func extractPackageInfo(
	ctx context.Context,
	dir fs.FS,
	packageFilePaths []string,
) map[directory]*packageInfo {
	packageMap := make(map[directory]*packageInfo, len(packageFilePaths))

	for _, pfPath := range packageFilePaths {
		pfDir := directory(filepath.Dir(pfPath))
		base := filepath.Base(pfPath)

		// Skip if we already have info for this directory
		if _, exists := packageMap[pfDir]; exists {
			continue
		}

		var (
			pkgInfo *packageInfo
			err     error
		)

		switch base {
		case "pyproject.toml":
			pkgInfo, err = parsePackageFromPyprojectToml(dir, pfPath, pfDir)
		case "setup.py":
			// For setup.py, we infer the package name from the directory
			pkgInfo = inferPackageFromDir(dir, pfDir)
		case "setup.cfg":
			// For setup.cfg, we also infer from directory
			pkgInfo = inferPackageFromDir(dir, pfDir)
		}

		if err != nil {
			slog.LogAttrs(
				ctx,
				slog.LevelDebug,
				"extractPackageInfo: failed to parse",
				logschema.UdaErrorPath(pfPath),
				logschema.UdaErrorMessage(err.Error()),
			)

			continue
		}

		if pkgInfo != nil && pkgInfo.name != "" {
			packageMap[pfDir] = pkgInfo
		}
	}

	return packageMap
}

func parsePackageFromPyprojectToml(
	dir fs.FS,
	pfPath string,
	pfDir directory,
) (*packageInfo, error) {
	data, err := fs.ReadFile(dir, pfPath)
	if err != nil {
		return nil, slogerr.New(err,
			logschema.UdaAnalyzerFile(pfPath),
			logschema.UdaAnalyzerLanguage("Python"),
			logschema.UdaErrorPhase("read"),
		)
	}

	var pyproject pyprojectTomlPartial
	if err := toml.Unmarshal(data, &pyproject); err != nil {
		return nil, slogerr.New(err,
			logschema.UdaAnalyzerFile(pfPath),
			logschema.UdaAnalyzerLanguage("Python"),
			logschema.UdaErrorPhase("parse"),
		)
	}

	// Try to get package name from [project] section first (PEP 621)
	name := pyproject.Project.Name
	if name == "" {
		// Fallback to [tool.poetry] section
		name = pyproject.Tool.Poetry.Name
	}

	if name == "" {
		// Fallback to directory name
		if pfDir == "." {
			// Use the pyproject.toml directory name
			return inferPackageFromDir(dir, pfDir), nil
		}

		name = string(pfDir)
	}

	// Normalize package name (PEP 503: replace - with _)
	name = strings.ReplaceAll(name, "-", "_")

	// Extract dependencies
	deps := make(map[string]struct{})

	for _, dep := range pyproject.Project.Dependencies {
		// Parse dependency string (e.g., "requests>=2.0" -> "requests")
		depName := extractDependencyName(dep)
		if depName != "" {
			deps[depName] = struct{}{}
		}
	}

	for depName := range pyproject.Tool.Poetry.Dependencies {
		if depName != "python" {
			deps[depName] = struct{}{}
		}
	}

	// Detect src layout
	srcDir := detectSrcLayout(dir, pfDir)

	// For src-layout packages, the distribution name (e.g. "python-dotenv") may differ
	// from the importable package name (e.g. "dotenv", the directory under src/).
	// Use the actual directory name under src/ as the package name since that is
	// what Python uses for imports.
	if srcDir != "" {
		importableName := findImportablePackageName(dir, pfDir, srcDir)
		if importableName != "" {
			name = importableName
		}
	}

	return &packageInfo{
		name:         packageName(name),
		dir:          pfDir,
		srcDir:       srcDir,
		dependencies: deps,
		subpackages:  make(map[string]string),
	}, nil
}

func inferPackageFromDir(dir fs.FS, pfDir directory) *packageInfo {
	name := string(pfDir)
	if pfDir == "." {
		// Try to find a Python package directory
		name = findPythonPackageName(dir, pfDir)
	}

	name = strings.ReplaceAll(name, "-", "_")
	srcDir := detectSrcLayout(dir, pfDir)

	return &packageInfo{
		name:         packageName(name),
		dir:          pfDir,
		srcDir:       srcDir,
		dependencies: make(map[string]struct{}),
		subpackages:  make(map[string]string),
	}
}

// findImportablePackageName discovers the actual importable package name from
// the directory under srcDir. For example, for python-dotenv with src-layout,
// the src/ directory contains "dotenv/", so the importable name is "dotenv".
func findImportablePackageName(dir fs.FS, pfDir directory, srcDir string) string {
	srcPath := filepath.Join(string(pfDir), srcDir)
	if pfDir == "." {
		srcPath = srcDir
	}

	entries, err := fs.ReadDir(dir, srcPath)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") &&
			!strings.HasPrefix(entry.Name(), "_") {
			pkgPath := filepath.Join(srcPath, entry.Name())
			if isPythonPackage(dir, pkgPath) {
				return entry.Name()
			}
		}
	}

	return ""
}

func detectSrcLayout(dir fs.FS, pfDir directory) string {
	srcPath := filepath.Join(string(pfDir), srcLayoutDir)
	if pfDir == "." {
		srcPath = srcLayoutDir
	}

	if info, err := fs.Stat(dir, srcPath); err == nil && info.IsDir() {
		return srcLayoutDir
	}

	return ""
}

// isCandidatePackageDir reports whether entry is a directory eligible to be a
// Python package (not hidden, not dunder/private).
func isCandidatePackageDir(entry fs.DirEntry) bool {
	return entry.IsDir() &&
		!strings.HasPrefix(entry.Name(), ".") &&
		!strings.HasPrefix(entry.Name(), "_")
}

// firstPythonPackageDir returns the name of the first immediate subdirectory of
// searchPath that is a Python package, or "" if searchPath is unreadable or
// contains no package.
func firstPythonPackageDir(dir fs.FS, searchPath string) string {
	entries, err := fs.ReadDir(dir, searchPath)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if !isCandidatePackageDir(entry) {
			continue
		}

		if isPythonPackage(dir, filepath.Join(searchPath, entry.Name())) {
			return entry.Name()
		}
	}

	return ""
}

func findPythonPackageName(dir fs.FS, pfDir directory) string {
	basePath := string(pfDir)
	if basePath == "." {
		basePath = ""
	}

	// Check for src layout first
	srcPath := srcLayoutDir
	if basePath != "" {
		srcPath = filepath.Join(basePath, srcLayoutDir)
	}

	if info, err := fs.Stat(dir, srcPath); err == nil && info.IsDir() {
		if name := firstPythonPackageDir(dir, srcPath); name != "" {
			return name
		}
	}

	// Check for flat layout
	searchPath := basePath
	if searchPath == "" {
		searchPath = "."
	}

	if name := firstPythonPackageDir(dir, searchPath); name != "" {
		return name
	}

	return "unknown"
}

// isPythonPackage checks if a directory is a Python package.
// Supports both traditional packages (with __init__.py) and
// namespace packages (PEP 420 - directories with .py files but no __init__.py).
func isPythonPackage(dir fs.FS, pkgPath string) bool {
	// Traditional package: has __init__.py
	initPath := filepath.Join(pkgPath, "__init__.py")
	if _, err := fs.Stat(dir, initPath); err == nil {
		return true
	}

	// Namespace package (PEP 420): directory containing .py files
	entries, err := fs.ReadDir(dir, pkgPath)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".py") {
			return true
		}
	}

	return false
}

// detectPackagesFromStructure finds Python packages when no manifest files exist.
// It looks for directories containing .py files (namespace packages) or __init__.py (traditional packages).
func detectPackagesFromStructure(
	ctx context.Context,
	dir fs.FS,
) (map[directory]*packageInfo, error) {
	packageMap := make(map[directory]*packageInfo)

	// Check for src-layout first
	srcPath := srcLayoutDir
	if info, err := fs.Stat(dir, srcPath); err == nil && info.IsDir() {
		if name := firstPythonPackageDir(dir, srcPath); name != "" {
			packageMap[directory(".")] = &packageInfo{
				name:         packageName(name),
				dir:          ".",
				srcDir:       srcLayoutDir,
				dependencies: make(map[string]struct{}),
				subpackages:  make(map[string]string),
			}
			slog.DebugContext(
				ctx, "detected package from src-layout",
				"name", name,
			)

			return packageMap, nil
		}
	}

	// Check for flat layout - look for Python packages in root
	entries, err := fs.ReadDir(dir, ".")
	if err != nil {
		return packageMap, fmt.Errorf("read root dir: %w", err)
	}

	for _, entry := range entries {
		if !isCandidatePackageDir(entry) || !isPythonPackage(dir, entry.Name()) {
			continue
		}

		packageMap[directory(".")] = &packageInfo{
			name:         packageName(entry.Name()),
			dir:          ".",
			srcDir:       "",
			dependencies: make(map[string]struct{}),
			subpackages:  make(map[string]string),
		}
		slog.DebugContext(ctx, "detected package from flat layout", "name", entry.Name())

		return packageMap, nil
	}

	// No packages found - check if there are loose .py files in root
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".py") {
			// Use directory name or "root" as package name for loose files
			packageMap[directory(".")] = &packageInfo{
				name:         "root",
				dir:          ".",
				srcDir:       "",
				dependencies: make(map[string]struct{}),
				subpackages:  make(map[string]string),
			}

			slog.DebugContext(ctx, "detected loose Python files, using 'root' as package")

			return packageMap, nil
		}
	}

	return packageMap, nil
}

func extractDependencyName(depStr string) string {
	// Parse dependency strings like "requests>=2.0", "numpy[extra]", "pkg @ url"
	depStr = strings.TrimSpace(depStr)
	if depStr == "" {
		return ""
	}

	// Handle environment markers first (e.g., "pywin32 ; sys_platform == 'win32'")
	if idx := strings.Index(depStr, ";"); idx != -1 {
		depStr = strings.TrimSpace(depStr[:idx])
	}

	// Handle @ for URL dependencies
	if idx := strings.Index(depStr, "@"); idx != -1 {
		depStr = strings.TrimSpace(depStr[:idx])
	}

	// Handle version specifiers
	for _, sep := range []string{">=", "<=", "==", "!=", "~=", ">", "<"} {
		if idx := strings.Index(depStr, sep); idx != -1 {
			depStr = strings.TrimSpace(depStr[:idx])

			break
		}
	}

	// Handle extras [extra1,extra2]
	if idx := strings.Index(depStr, "["); idx != -1 {
		depStr = strings.TrimSpace(depStr[:idx])
	}

	return strings.ReplaceAll(depStr, "-", "_")
}

// Phase 2: File discovery

func listPythonFiles(ctx context.Context, dir fs.FS) ([]string, error) {
	return files.ListFiles(
		ctx,
		dir,
		files.SkipHiddenDirs(),
		files.SkipHiddenFiles(),
		skipPythonBuildDirs(),
		pythonFileFilter(),
	)
}

func pythonFileFilter() files.FileFilter {
	return func(path string, d fs.DirEntry) bool {
		if d.IsDir() {
			return false
		}

		return filepath.Ext(path) != ".py"
	}
}

// getPackageForFile finds which package a .py file belongs to.
// For src-layout packages, only files that reside under the src directory
// are attributed to the package. Files outside the src tree (e.g. setup.py
// at the repo root, or a tests/ directory alongside src/) are not part of
// the package and return nil.
// fileWithinPackageSrc reports whether filePath lives inside pkgInfo's src tree.
// Packages without a configured srcDir own every file mapped to them.
func fileWithinPackageSrc(pkgInfo *packageInfo, filePath string) bool {
	if pkgInfo.srcDir == "" {
		return true
	}

	srcRoot := filepath.Join(string(pkgInfo.dir), pkgInfo.srcDir)
	if pkgInfo.dir == "." {
		srcRoot = pkgInfo.srcDir
	}

	rel, err := filepath.Rel(srcRoot, filePath)

	return err == nil && !strings.HasPrefix(rel, "..")
}

func getPackageForFile(filePath string, packageMap map[directory]*packageInfo) *packageInfo {
	for dir := filepath.Dir(filePath); ; dir = filepath.Dir(dir) {
		pkgInfo, ok := packageMap[directory(dir)]
		if ok {
			if !fileWithinPackageSrc(pkgInfo, filePath) {
				// File is outside the src tree — not part of this package.
				return nil
			}

			return pkgInfo
		}

		if dir == "." || dir == filepath.Dir(dir) {
			break
		}
	}

	return nil
}

// getModulePath returns the Python module path for a file within a package.
// e.g. for "src/myapp/routes.py" in package "myapp", returns "myapp.routes"
// e.g. for "myapp/__init__.py" in package "myapp", returns "myapp"
// e.g. for "myapp/handlers/auth.py" in package "myapp", returns "myapp.handlers.auth"
//
// The strategy parameter controls boundary granularity:
//   - StrategyModule: per-.py-file granularity (default, existing behavior)
//   - StrategyPackage: collapse everything to the top-level package name
//   - StrategySubpackage: collapse to the directory containing the .py file
func getModulePath(filePath string, pkgInfo *packageInfo, strategy BoundaryStrategy) string {
	// StrategyPackage: always collapse to top-level package name
	if strategy == StrategyPackage {
		return string(pkgInfo.name)
	}

	pkgDir := string(pkgInfo.dir)

	// Determine the base directory for module path calculation
	var baseDir string
	if pkgInfo.srcDir != "" {
		baseDir = filepath.Join(pkgDir, pkgInfo.srcDir)
	} else {
		baseDir = pkgDir
	}

	rel, err := filepath.Rel(baseDir, filePath)
	if err != nil || strings.HasPrefix(rel, "..") {
		// Fallback: use path relative to package root
		rel, _ = filepath.Rel(pkgDir, filePath)
	}

	// Remove .py extension
	rel = strings.TrimSuffix(rel, ".py")

	// __init__.py represents the package itself
	isInitFile := filepath.Base(rel) == "__init__"
	if isInitFile {
		rel = filepath.Dir(rel)
		if rel == "." {
			return string(pkgInfo.name)
		}
	}

	// Convert path separators to dots
	parts := strings.Split(filepath.ToSlash(rel), "/")

	// Filter out empty parts
	var filteredParts []string

	for _, p := range parts {
		if p != "" && p != "." {
			filteredParts = append(filteredParts, p)
		}
	}

	if len(filteredParts) == 0 {
		return string(pkgInfo.name)
	}

	// Build the full module path first
	var modulePath string
	if filteredParts[0] == string(pkgInfo.name) {
		modulePath = strings.Join(filteredParts, ".")
	} else {
		modulePath = string(pkgInfo.name) + "." + strings.Join(filteredParts, ".")
	}

	// StrategySubpackage: collapse to the directory-package level
	if strategy == StrategySubpackage {
		// For __init__.py files, the module path already represents the directory
		// (e.g., "myapp.handlers" for myapp/handlers/__init__.py), so return as-is.
		if isInitFile {
			return modulePath
		}
		// The module path is like "myapp.handlers.auth" — strip the last segment
		// (the file name) to get "myapp.handlers" (the containing directory).
		// If the file is directly in the package root, return the package name.
		const rootPlusFile = 2 // pkg.file → strip file, keep pkg

		dotParts := strings.Split(modulePath, ".")
		if len(dotParts) <= rootPlusFile {
			// e.g. "myapp.config" -> "myapp", or "myapp" -> "myapp"
			return string(pkgInfo.name)
		}
		// e.g. "myapp.handlers.auth" -> "myapp.handlers"
		return strings.Join(dotParts[:len(dotParts)-1], ".")
	}

	return modulePath
}

// Phase 3: Tree-sitter analysis

type capturedImport struct {
	module   string            // The imported module path
	names    []string          // Imported names (for "from x import a, b")
	alias    string            // Alias if any
	isFrom   bool              // True for "from x import y", false for "import x"
	relLevel int               // Relative import level (0 for absolute, 1 for ., 2 for .., etc.)
	pos      analyzer.Position // Position of the import
}

type capturedUsage struct {
	expr string // e.g., "Config" or "os.path.join"
	pos  analyzer.Position
}

type captures struct {
	modulePath string
	isInitFile bool
	imports    []capturedImport
	usages     []capturedUsage
}

var pythonQuery = `
; import module
(import_statement name: (dotted_name) @import_module)

; import module as alias
(import_statement name: (aliased_import
  name: (dotted_name) @import_module_aliased
  alias: (identifier) @import_alias))

; from module import name
(import_from_statement
  module_name: (dotted_name) @from_module
  name: (dotted_name) @from_name)

; from module import name as alias
(import_from_statement
  module_name: (dotted_name) @from_module
  name: (aliased_import
    name: (dotted_name) @from_name_aliased
    alias: (identifier) @from_alias))

; Relative imports: from . import, from .. import
(import_from_statement
  module_name: (relative_import (import_prefix) @relative_prefix .)
  name: (dotted_name) @relative_name)

; Relative imports with module: from .module import name
(import_from_statement
  module_name: (relative_import
    (import_prefix) @relative_prefix_mod
    (dotted_name) @relative_module)
  name: (dotted_name) @relative_mod_name)

; Relative import with alias: from . import name as alias
(import_from_statement
  module_name: (relative_import (import_prefix) @relative_prefix_alias .)
  name: (aliased_import
    name: (dotted_name) @relative_name_aliased
    alias: (identifier) @relative_alias))

; Relative import with module and alias: from .mod import name as alias
(import_from_statement
  module_name: (relative_import
    (import_prefix) @relative_prefix_mod_alias
    (dotted_name) @relative_module_alias)
  name: (aliased_import
    name: (dotted_name) @relative_mod_name_aliased
    alias: (identifier) @relative_mod_alias))

; Function/class calls
(call function: (identifier) @direct_call)
(call function: (attribute
  object: (identifier) @call_object
  attribute: (identifier) @call_method))

; Attribute access
(attribute
  object: (identifier) @attr_object
  attribute: (identifier) @attr_name)

; Type annotations
(type (identifier) @type_annotation)

; Decorators
(decorator (identifier) @decorator_name)
(decorator (call function: (identifier) @decorator_call))

; Base class references
(class_definition superclasses: (argument_list (identifier) @base_class))

; Metaclass references (keyword arguments in class definition superclasses)
(class_definition superclasses: (argument_list
  (keyword_argument name: (identifier) @_meta_kw
                    value: (identifier) @metaclass_ref)))

; Subscripted types (value and arguments, all depths)
; Runtime subscript expressions: a = Optional[str], mydict["key"]
(subscript value: (identifier) @subscript_type)
(subscript subscript: (identifier) @subscript_arg)

; Generic types in type annotations: x: Optional[str], y: Dict[str, int]
(generic_type (identifier) @subscript_type)
(type_parameter (type (identifier) @subscript_arg))
`

var (
	compiledPythonQuery *treesitter.Query
	pythonQueryOnce     sync.Once
	errPythonQuery      error
)

func getCompiledPythonQuery() (*treesitter.Query, error) {
	pythonQueryOnce.Do(func() {
		pythonLang := treesitter.NewLanguage(tree_sitter_python.Language())

		var qErr *treesitter.QueryError

		compiledPythonQuery, qErr = treesitter.NewQuery(pythonLang, pythonQuery)
		if qErr != nil {
			errPythonQuery = qErr
		}
	})

	return compiledPythonQuery, errPythonQuery
}

func getCapturesFromMatches(
	modulePath string,
	filePath string,
	query *treesitter.Query,
	queryCursor *treesitter.QueryCursor,
	tree *treesitter.Tree,
	text []byte,
) captures {
	caps := captures{
		modulePath: modulePath,
		isInitFile: filepath.Base(filePath) == "__init__.py",
	}

	captureNames := query.CaptureNames()
	matches := queryCursor.Matches(query, tree.RootNode(), text)

	// Track processed imports and usages to avoid duplicates
	processedFromImports := make(map[string]int) // from_module -> index in caps.imports
	seenUsages := make(map[string]struct{})      // "expr:line:col" -> seen

	for match := matches.Next(); match != nil; match = matches.Next() {
		captureMap := make(map[string]captureInfo)

		for _, capture := range match.Captures {
			name := captureNames[capture.Index]
			startPos := capture.Node.StartPosition()
			endPos := capture.Node.EndPosition()
			captureMap[name] = captureInfo{
				text:     capture.Node.Utf8Text(text),
				startRow: startPos.Row,
				startCol: startPos.Column,
				endRow:   endPos.Row,
				endCol:   endPos.Column,
			}
		}

		appendSimpleImports(&caps, captureMap, filePath)
		appendFromImports(&caps, captureMap, filePath, processedFromImports)
		appendRelativeImports(&caps, captureMap, filePath)
		appendUsages(&caps, captureMap, filePath, seenUsages)
	}

	return caps
}

// capturePosition converts a tree-sitter captureInfo into a 1-based analyzer.Position.
func capturePosition(filePath string, ci captureInfo) analyzer.Position {
	return analyzer.Position{
		File:     filePath,
		Line:     ci.startRow + 1,
		ColStart: ci.startCol + 1,
		ColEnd:   ci.endCol + 1,
	}
}

// appendSimpleImports records `import x` and `import x as y` statements.
func appendSimpleImports(caps *captures, captureMap map[string]captureInfo, filePath string) {
	// Simple import: import os
	if modInfo, ok := captureMap["import_module"]; ok {
		caps.imports = append(caps.imports, capturedImport{
			module: modInfo.text,
			isFrom: false,
			pos:    capturePosition(filePath, modInfo),
		})
	}

	// Aliased import: import numpy as np
	if modInfo, ok := captureMap["import_module_aliased"]; ok {
		alias := ""
		if aliasInfo, ok := captureMap["import_alias"]; ok {
			alias = aliasInfo.text
		}

		caps.imports = append(caps.imports, capturedImport{
			module: modInfo.text,
			alias:  alias,
			isFrom: false,
			pos:    capturePosition(filePath, modInfo),
		})
	}
}

// appendFromImports records `from x import y` and `from x import y as z` statements.
func appendFromImports(
	caps *captures,
	captureMap map[string]captureInfo,
	filePath string,
	processedFromImports map[string]int,
) {
	modInfo, ok := captureMap["from_module"]
	if !ok {
		return
	}

	// From import: from os import path
	if nameInfo, ok := captureMap["from_name"]; ok {
		key := modInfo.text + ":" + nameInfo.text
		if idx, exists := processedFromImports[key]; exists {
			// Already processed this exact import, update names
			caps.imports[idx].names = append(caps.imports[idx].names, nameInfo.text)
		} else {
			processedFromImports[key] = len(caps.imports)
			caps.imports = append(caps.imports, capturedImport{
				module: modInfo.text,
				names:  []string{nameInfo.text},
				isFrom: true,
				pos:    capturePosition(filePath, modInfo),
			})
		}
	}

	// From import with alias: from os import path as p
	if nameInfo, ok := captureMap["from_name_aliased"]; ok {
		alias := ""
		if aliasInfo, ok := captureMap["from_alias"]; ok {
			alias = aliasInfo.text
		}

		key := modInfo.text + ":" + nameInfo.text + ":" + alias
		if _, exists := processedFromImports[key]; !exists {
			processedFromImports[key] = len(caps.imports)
			caps.imports = append(caps.imports, capturedImport{
				module: modInfo.text,
				names:  []string{nameInfo.text},
				alias:  alias,
				isFrom: true,
				pos:    capturePosition(filePath, modInfo),
			})
		}
	}
}

// appendRelativeImports records `from . import y` and `from .mod import y` variants.
func appendRelativeImports(caps *captures, captureMap map[string]captureInfo, filePath string) {
	// Relative import: from . import something
	if prefixInfo, ok := captureMap["relative_prefix"]; ok {
		if nameInfo, ok := captureMap["relative_name"]; ok {
			relLevel := len(prefixInfo.text)
			caps.imports = append(caps.imports, capturedImport{
				module:   "",
				names:    []string{nameInfo.text},
				isFrom:   true,
				relLevel: relLevel,
				pos:      capturePosition(filePath, prefixInfo),
			})
		}
	}

	// Relative import with module: from .module import something
	if prefixInfo, ok := captureMap["relative_prefix_mod"]; ok {
		modName := ""
		if modInfo, ok := captureMap["relative_module"]; ok {
			modName = modInfo.text
		}

		if nameInfo, ok := captureMap["relative_mod_name"]; ok {
			relLevel := len(prefixInfo.text)
			caps.imports = append(caps.imports, capturedImport{
				module:   modName,
				names:    []string{nameInfo.text},
				isFrom:   true,
				relLevel: relLevel,
				pos:      capturePosition(filePath, prefixInfo),
			})
		}
	}

	// Relative import with alias: from . import name as alias
	if prefixInfo, ok := captureMap["relative_prefix_alias"]; ok {
		if nameInfo, ok := captureMap["relative_name_aliased"]; ok {
			alias := ""
			if aliasInfo, ok := captureMap["relative_alias"]; ok {
				alias = aliasInfo.text
			}

			relLevel := len(prefixInfo.text)
			caps.imports = append(caps.imports, capturedImport{
				module:   "",
				names:    []string{nameInfo.text},
				alias:    alias,
				isFrom:   true,
				relLevel: relLevel,
				pos:      capturePosition(filePath, prefixInfo),
			})
		}
	}

	// Relative import with module and alias: from .mod import name as alias
	if prefixInfo, ok := captureMap["relative_prefix_mod_alias"]; ok {
		modName := ""
		if modInfo, ok := captureMap["relative_module_alias"]; ok {
			modName = modInfo.text
		}

		if nameInfo, ok := captureMap["relative_mod_name_aliased"]; ok {
			alias := ""
			if aliasInfo, ok := captureMap["relative_mod_alias"]; ok {
				alias = aliasInfo.text
			}

			relLevel := len(prefixInfo.text)
			caps.imports = append(caps.imports, capturedImport{
				module:   modName,
				names:    []string{nameInfo.text},
				alias:    alias,
				isFrom:   true,
				relLevel: relLevel,
				pos:      capturePosition(filePath, prefixInfo),
			})
		}
	}
}

// appendUsages records qualified-symbol usages (calls, attributes, annotations,
// decorators, base classes, subscripts), deduplicating by expression + position.
func appendUsages(
	caps *captures,
	captureMap map[string]captureInfo,
	filePath string,
	seenUsages map[string]struct{},
) {
	addUsage := func(expr string, pos analyzer.Position) {
		key := fmt.Sprintf("%s:%d:%d", expr, pos.Line, pos.ColStart)
		if _, ok := seenUsages[key]; ok {
			return
		}

		seenUsages[key] = struct{}{}

		caps.usages = append(caps.usages, capturedUsage{
			expr: expr,
			pos:  pos,
		})
	}

	// Direct call: Config()
	if callInfo, ok := captureMap["direct_call"]; ok {
		addUsage(callInfo.text, capturePosition(filePath, callInfo))
	}

	// Method call: os.path
	if objInfo, ok := captureMap["call_object"]; ok {
		if methInfo, ok := captureMap["call_method"]; ok {
			addUsage(
				objInfo.text+"."+methInfo.text,
				capturePosition(filePath, objInfo),
			)
		}
	}

	// Attribute access: config.value
	if objInfo, ok := captureMap["attr_object"]; ok {
		if attrInfo, ok := captureMap["attr_name"]; ok {
			addUsage(
				objInfo.text+"."+attrInfo.text,
				capturePosition(filePath, objInfo),
			)
		}
	}

	// Type annotation: def foo(x: Config)
	if typeInfo, ok := captureMap["type_annotation"]; ok {
		addUsage(typeInfo.text, capturePosition(filePath, typeInfo))
	}

	// Decorator: @decorator
	if decInfo, ok := captureMap["decorator_name"]; ok {
		addUsage(decInfo.text, capturePosition(filePath, decInfo))
	}

	// Decorator call: @decorator()
	if decInfo, ok := captureMap["decorator_call"]; ok {
		addUsage(decInfo.text, capturePosition(filePath, decInfo))
	}

	// Base class reference: class Foo(Base)
	if baseInfo, ok := captureMap["base_class"]; ok {
		addUsage(baseInfo.text, capturePosition(filePath, baseInfo))
	}

	// Metaclass reference: class Foo(metaclass=ABCMeta)
	if metaInfo, ok := captureMap["metaclass_ref"]; ok {
		addUsage(metaInfo.text, capturePosition(filePath, metaInfo))
	}

	// Subscript type: Optional[str] -> captures "Optional"
	if subInfo, ok := captureMap["subscript_type"]; ok {
		addUsage(subInfo.text, capturePosition(filePath, subInfo))
	}

	// Subscript argument: Optional[str] -> captures "str"; Dict[str, Config] -> captures "str", "Config"
	if subArgInfo, ok := captureMap["subscript_arg"]; ok {
		addUsage(subArgInfo.text, capturePosition(filePath, subArgInfo))
	}
}

type captureInfo struct {
	text     string
	startRow uint
	startCol uint
	endRow   uint
	endCol   uint
}

// resolveImportPath resolves a Python import to a package/module path.
// Returns the resolved package key and whether it's internal.
func resolveImportPath(
	imp capturedImport,
	currentModule string,
	currentPkg *packageInfo,
	isInitFile bool,
) (string, bool, bool) {
	// Handle relative imports
	if imp.relLevel > 0 {
		resolved := resolveRelativeImport(imp, currentModule, isInitFile)
		if resolved == "" {
			return "", false, true
		}
		// Check if the resolved module is internal
		pkgPrefix := string(currentPkg.name) + "."
		if strings.HasPrefix(resolved, pkgPrefix) || resolved == string(currentPkg.name) {
			return resolved, true, false
		}

		return resolved, false, false
	}

	module := imp.module

	// Check if it's an internal module (starts with package name)
	pkgName := string(currentPkg.name)
	if strings.HasPrefix(module, pkgName+".") {
		return module, true, false
	}

	if module == pkgName {
		// For "from pkgName import Y", the dependency is on the submodule Y,
		// not on pkgName itself (which would be self-referential for __init__.py).
		if imp.isFrom && len(imp.names) > 0 {
			return module + "." + imp.names[0], true, false
		}

		return module, true, false
	}

	// For "from x import y", the dependency is on "x"
	// For "import x.y.z", the dependency is on "x.y.z" (or top-level "x")
	if !imp.isFrom {
		// "import x.y.z" - we care about the module path
		parts := strings.Split(module, ".")
		if len(parts) > 1 {
			// For "import a.b.c", dependency is on "a" (the package)
			return parts[0], false, false
		}

		return module, false, false
	}

	// "from x import y" - dependency is on "x"
	return module, false, false
}

// resolveRelativeImport resolves a relative import to an absolute module path.
// In Python:
// - from . import x in myapp.handlers.auth -> myapp.handlers.x (same package)
// - from .. import x in myapp.handlers.auth -> myapp.x (parent package)
// - from ...pkg import x in myapp.handlers.auth.oauth -> myapp.pkg.x
// - from .submod import x in myapp.handlers -> myapp.handlers.submod.x
//
// The relLevel indicates how many levels up from the containing package:
// - relLevel=1 (from .) = same package as current module
// - relLevel=2 (from ..) = parent of current package
// - relLevel=3 (from ...) = grandparent package.
func resolveRelativeImport(
	imp capturedImport,
	currentModule string,
	isInitFile bool,
) string {
	parts := strings.Split(currentModule, ".")

	// For a file like myapp/handlers/auth.py with module path myapp.handlers.auth,
	// the containing package is myapp.handlers (parts[:len(parts)-1]).
	// relLevel=1 means same package, so we use parts[:len(parts)-1]
	// relLevel=2 means parent package, so we use parts[:len(parts)-2]
	// In general: parts[:len(parts)-relLevel]
	//
	// For __init__.py files, the module path IS the package directory.
	// from . import x means same package, so we don't strip the last part.
	// For regular files, the last part is the file name, so we strip it.

	offset := imp.relLevel
	if isInitFile {
		offset = imp.relLevel - 1
	}

	// Calculate the target index: we go up offset from the current module
	targetIdx := len(parts) - offset
	if targetIdx < 0 {
		return "" // Invalid relative import
	}

	baseParts := parts[:targetIdx]

	// Add the module from the relative import if present (e.g., from .config import X)
	if imp.module != "" {
		baseParts = append(baseParts, strings.Split(imp.module, ".")...)
	}

	if len(baseParts) == 0 {
		return ""
	}

	return strings.Join(baseParts, ".")
}

// importNameInfo holds metadata about an imported name for usage site tracking.
type importNameInfo struct {
	pkg      string // resolved package key
	fullPath string // full resolved import path
}

// buildImportNameMap creates a map from local imported names to their resolved package and full path.
func buildImportNameMap(
	imports []capturedImport,
	currentModule string,
	currentPkg *packageInfo,
	isInitFile bool,
) map[string]importNameInfo {
	nameMap := make(map[string]importNameInfo)

	for _, imp := range imports {
		depPkg, _, skip := resolveImportPath(imp, currentModule, currentPkg, isInitFile)
		if skip || depPkg == "" {
			continue
		}

		if imp.isFrom {
			addFromImportNames(nameMap, imp, depPkg)
		} else {
			addPlainImportName(nameMap, imp, depPkg)
		}
	}

	return nameMap
}

// addFromImportNames maps each name of a `from x import a, b` statement to depPkg.
func addFromImportNames(nameMap map[string]importNameInfo, imp capturedImport, depPkg string) {
	for _, name := range imp.names {
		localName := name
		if imp.alias != "" && len(imp.names) == 1 {
			localName = imp.alias
		}

		fullPath := depPkg + "." + name
		// If depPkg was resolved to include the submodule name
		// (e.g., "from pkg import submod" → depPkg="pkg.submod"),
		// don't double it.
		if strings.HasSuffix(depPkg, "."+name) {
			fullPath = depPkg
		}

		nameMap[localName] = importNameInfo{
			pkg:      depPkg,
			fullPath: fullPath,
		}
	}
}

// addPlainImportName maps the local name of an `import x`/`import x as y` statement to depPkg.
func addPlainImportName(nameMap map[string]importNameInfo, imp capturedImport, depPkg string) {
	var localName string
	if imp.alias != "" {
		localName = imp.alias
	} else {
		// For "import a.b.c", the local name is "a"
		parts := strings.Split(imp.module, ".")
		localName = parts[0]
	}

	nameMap[localName] = importNameInfo{
		pkg:      depPkg,
		fullPath: imp.module,
	}
}

// extractIdentifierFromExpr extracts the first identifier from an expression.
func extractIdentifierFromExpr(expr string) string {
	if before, _, ok := strings.Cut(expr, "."); ok {
		return before
	}

	return expr
}

// hydrateUsageExpressions records usage sites for imported symbols.
func hydrateUsageExpressions(
	usages []capturedUsage,
	importedNames map[string]importNameInfo,
	pkgNode *analyzer.PackageAnalysis,
) {
	for _, usage := range usages {
		identifier := extractIdentifierFromExpr(usage.expr)

		info, exists := importedNames[identifier]
		if !exists {
			continue
		}

		importInfo, ok := pkgNode.Out.Get(info.pkg)
		if !ok {
			continue
		}

		importInfo.Add(info.fullPath, info.fullPath, usage.pos)
	}
}

// Phase 4: Analysis orchestration

//nolint:funlen // per-file walk: parse + capture + hydrate + dedup + boundaries.
func analyzePythonMetrics(
	ctx context.Context,
	dir fs.FS,
	packageMap map[directory]*packageInfo,
	strategy BoundaryStrategy,
) ([]analyzer.Metrics, []analyzer.PackageBoundary, error) {
	depGraph := analyzer.NewPackageAnalysisTree()

	pyFilepaths, err := listPythonFiles(ctx, dir)
	if err != nil {
		return nil, nil, err
	}

	tsparser := treesitter.NewParser()
	defer tsparser.Close()

	pythonLanguage := treesitter.NewLanguage(tree_sitter_python.Language())
	if err := tsparser.SetLanguage(pythonLanguage); err != nil {
		return nil, nil, err
	}

	compiledQuery, err := getCompiledPythonQuery()
	if err != nil {
		return nil, nil, err
	}

	pathInterner := analyzer.NewStringInterner()

	// Track package → directories for boundary computation
	pkgDirs := make(map[string]map[string]bool)

	for _, pyFilepath := range pyFilepaths {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		pyFilepath = pathInterner.Intern(pyFilepath)

		pkgInfo := getPackageForFile(pyFilepath, packageMap)
		if pkgInfo == nil {
			slog.LogAttrs(ctx, slog.LevelDebug, "no package found for file, skipping",
				logschema.UdaAnalyzerFile(pyFilepath),
			)

			continue
		}

		modulePath := getModulePath(pyFilepath, pkgInfo, strategy)

		// Collect directory for boundary mapping
		fileDir := filepath.Dir(pyFilepath)

		if pkgDirs[modulePath] == nil {
			pkgDirs[modulePath] = make(map[string]bool)
		}

		pkgDirs[modulePath][fileDir] = true

		tree, text, err := ts.Parse(ctx, tsparser, dir, pyFilepath)
		if err != nil {
			return nil, nil, slogerr.New(err,
				logschema.UdaAnalyzerFile(pyFilepath),
				logschema.UdaAnalyzerLanguage("Python"),
				logschema.UdaErrorPhase("parse"),
			)
		}

		qc := treesitter.NewQueryCursor()

		caps := getCapturesFromMatches(modulePath, pyFilepath, compiledQuery, qc, tree, text)

		tree.Close()
		qc.Close()

		var pkgNode *analyzer.PackageAnalysis
		if pn, exists := depGraph.Get(modulePath); exists {
			pkgNode = pn
		} else {
			pkgNode = depGraph.Add(modulePath)
		}

		// Register imports
		for _, imp := range caps.imports {
			depPkg, _, skip := resolveImportPath(imp, modulePath, pkgInfo, caps.isInitFile)
			if skip || depPkg == "" {
				continue
			}

			pkgNode.Out.Add(depPkg)
		}

		// Build import name lookup map and hydrate usage expressions
		importedNames := buildImportNameMap(
			caps.imports,
			modulePath,
			pkgInfo,
			caps.isInitFile,
		)
		hydrateUsageExpressions(caps.usages, importedNames, pkgNode)
	}

	// Build boundaries
	boundaries := make([]analyzer.PackageBoundary, 0, len(pkgDirs))
	for name, dirs := range pkgDirs {
		dirSlice := make([]string, 0, len(dirs))
		for d := range dirs {
			dirSlice = append(dirSlice, d)
		}

		boundaries = append(boundaries, analyzer.PackageBoundary{
			Name: name,
			Dirs: dirSlice,
		})
	}

	analyzer.ResolveInwardDependencies(depGraph)
	pkgNodes := depGraph.GetRootNodes()

	finalMetrics := make([]analyzer.Metrics, 0, len(pkgNodes))
	for _, pkgNode := range pkgNodes {
		outwardCouplingStats := analyzer.GetCouplingStats(ctx, pkgNode.Out)
		inwardCouplingStats := analyzer.GetCouplingStats(ctx, pkgNode.In)

		metrics := analyzer.Metrics{
			Package: analyzer.Package(pkgNode.Key()),
			Inward:  inwardCouplingStats,
			Outward: outwardCouplingStats,
		}
		slog.DebugContext(ctx, "computed metrics", "metrics", metrics)
		finalMetrics = append(finalMetrics, metrics)
	}

	return finalMetrics, boundaries, nil
}
