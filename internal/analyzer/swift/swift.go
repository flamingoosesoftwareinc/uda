package swift

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	files "github.com/flamingoosesoftwareinc/fsift"
	"github.com/flamingoosesoftwareinc/slogerr"
	tree_sitter_swift "github.com/flamingoosesoftwareinc/tree-sitter-swift/bindings/go"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/swift/manifest"
	"github.com/flamingoosesoftwareinc/uda/internal/logschema"
	"github.com/flamingoosesoftwareinc/uda/internal/ts"
	treesitter "github.com/tree-sitter/go-tree-sitter"
)

type swiftAnalyzer struct {
	boundaries []analyzer.PackageBoundary
}

var (
	_ analyzer.Analyzer         = &swiftAnalyzer{}
	_ analyzer.BoundaryProvider = &swiftAnalyzer{}
)

// SwiftAnalyzer returns a Swift source analyzer (satisfies analyzer.Analyzer
// and analyzer.BoundaryProvider). Concrete pointer return so tests can call
// Boundaries without a type assertion; cmd uses the interface upcast.
//
//nolint:revive // unexported-return: the concrete type is intentionally package-private; consumers upcast to analyzer.Analyzer / BoundaryProvider.
func SwiftAnalyzer() *swiftAnalyzer {
	return &swiftAnalyzer{}
}

func (s *swiftAnalyzer) Name() string { return "Swift" }

func (s *swiftAnalyzer) Analyze(ctx context.Context, dir fs.FS) ([]analyzer.Metrics, error) {
	packageFiles, err := listPackageSwiftFiles(ctx, dir)
	if err != nil {
		return nil, err
	}

	slog.LogAttrs(ctx, slog.LevelDebug, "found Package.swift files",
		logschema.UdaAnalyzerFilepaths(packageFiles),
	)

	packageMap, err := extractPackageInfo(ctx, dir, packageFiles)
	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, "identified package info", "packages", packageMap)

	// Parse manifest for system-import filtering and target scoping.
	// nil manifest = no Package.swift; fall back to current behavior.
	parsedManifest, err := manifest.Parse(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.LogAttrs(ctx, slog.LevelDebug, "manifest parse failed, falling back",
			logschema.UdaErrorMessage(err.Error()),
		)
	}

	metrics, boundaries, err := analyzeSwiftMetrics(ctx, dir, packageMap, parsedManifest)
	if err != nil {
		return nil, err
	}

	s.boundaries = boundaries

	return metrics, nil
}

// Boundaries returns the filesystem directories for each analyzed package.
// Lazily invokes Analyze() if boundaries have not been populated yet.
func (s *swiftAnalyzer) Boundaries(
	ctx context.Context,
	dir fs.FS,
) ([]analyzer.PackageBoundary, error) {
	if s.boundaries == nil {
		if _, err := s.Analyze(ctx, dir); err != nil {
			return nil, err
		}
	}

	return s.boundaries, nil
}

// Phase 1: Module boundary detection via Package.swift

type (
	directory   string
	packageName string
)

type packageInfo struct {
	name    packageName
	dir     directory
	targets []string // target names from Package.swift
}

func listPackageSwiftFiles(ctx context.Context, dir fs.FS) ([]string, error) {
	return files.ListFiles(
		ctx,
		dir,
		files.SkipHiddenDirs(),
		files.SkipHiddenFiles(),
		skipBuildDirs(),
		packageSwiftFileFilter(),
	)
}

func packageSwiftFileFilter() files.FileFilter {
	return func(path string, d fs.DirEntry) bool {
		if d.IsDir() {
			return false
		}

		return filepath.Base(path) != "Package.swift"
	}
}

func skipBuildDirs() files.FileFilter {
	return func(_ string, d fs.DirEntry) bool {
		if !d.IsDir() {
			return false
		}

		name := d.Name()

		return name == ".build" || name == ".swiftpm" || name == "DerivedData"
	}
}

// extractPackageInfo parses Package.swift files via manifest.Parse to extract
// package metadata.
func extractPackageInfo(
	ctx context.Context,
	dir fs.FS,
	packageFilePaths []string,
) (map[directory]*packageInfo, error) {
	packageMap := make(map[directory]*packageInfo, len(packageFilePaths))

	for _, pfPath := range packageFilePaths {
		pfDir := directory(filepath.Dir(pfPath))

		// Create a sub-filesystem rooted at the Package.swift directory so
		// manifest.Parse can read "Package.swift" from it.
		subDir := string(pfDir)
		if subDir == "" {
			subDir = "."
		}

		subFS, err := fs.Sub(dir, subDir)
		if err != nil {
			return nil, fmt.Errorf("extractPackageInfo: sub fs %s: %w", subDir, err)
		}

		parsedManifest, err := manifest.Parse(subFS)
		if err != nil {
			slog.LogAttrs(
				ctx,
				slog.LevelDebug,
				"extractPackageInfo: manifest.Parse failed",
				logschema.UdaErrorPath(pfPath),
				logschema.UdaErrorMessage(err.Error()),
			)

			continue
		}

		if parsedManifest.Name == "" {
			slog.LogAttrs(
				ctx,
				slog.LevelDebug,
				"extractPackageInfo: could not extract package name",
				logschema.UdaErrorPath(pfPath),
			)

			continue
		}

		targets := make([]string, 0, len(parsedManifest.Targets))
		for _, t := range parsedManifest.Targets {
			targets = append(targets, t.Name)
		}

		packageMap[pfDir] = &packageInfo{
			name:    packageName(parsedManifest.Name),
			dir:     pfDir,
			targets: targets,
		}
	}

	return packageMap, nil
}

// Phase 2: File discovery

func listSwiftFiles(ctx context.Context, dir fs.FS) ([]string, error) {
	return files.ListFiles(
		ctx,
		dir,
		files.SkipHiddenDirs(),
		files.SkipHiddenFiles(),
		skipBuildDirs(),
		swiftFileFilter(),
	)
}

func swiftFileFilter() files.FileFilter {
	return func(path string, d fs.DirEntry) bool {
		if d.IsDir() {
			return false
		}

		return filepath.Ext(path) != ".swift" || filepath.Base(path) == "Package.swift"
	}
}

// getPackageForFile finds which package a .swift file belongs to.
func getPackageForFile(filePath string, packageMap map[directory]*packageInfo) *packageInfo {
	for dir := filepath.Dir(filePath); ; dir = filepath.Dir(dir) {
		if pi, ok := packageMap[directory(dir)]; ok {
			return pi
		}

		if dir == "." || dir == filepath.Dir(dir) {
			break
		}
	}

	return nil
}

// getModulePath returns the Swift module path for a file within a package.
// e.g. for "Sources/MyTarget/Routes.swift" in package "MyPackage", returns "MyTarget"
// e.g. for "app/Sources/App/main.swift" in package "App", returns "App"
// For files not in a standard Sources layout, uses the file path relative to package root.
func getModulePath(filePath string, pkgInfo *packageInfo) string {
	pkgDir := string(pkgInfo.dir)

	rel, err := filepath.Rel(pkgDir, filePath)
	if err != nil {
		return string(pkgInfo.name)
	}

	// Standard SPM layout: Sources/<TargetName>/...
	// or Tests/<TargetName>/...
	parts := strings.Split(filepath.ToSlash(rel), "/")

	if len(parts) >= 2 && (parts[0] == "Sources" || parts[0] == "Tests") {
		// Return the target name (second component)
		return parts[1]
	}

	// For non-standard layouts, use the package name
	return string(pkgInfo.name)
}

// Phase 3: Tree-sitter analysis

type capturedImport struct {
	module string // imported module name e.g. "Foundation", "UIKit"
	pos    analyzer.Position
}

type capturedUsage struct {
	expr string // e.g., "URLSession", "String", "MainActor"
	pos  analyzer.Position
}

type capturedDeclaration struct {
	name string // e.g., "User", "APIService"
}

type captures struct {
	modulePath   string
	imports      []capturedImport
	usages       []capturedUsage
	declarations []capturedDeclaration
}

var swiftQuery = `
; Module imports - identifier for import module names
(import_declaration (identifier) @import_module)

; Type declarations - capture names of exported types for symbol resolution
(class_declaration (type_identifier) @decl_type)
(protocol_declaration (type_identifier) @decl_type)

; Type usage - type_identifier for type references
(type_identifier) @type_usage

; Simple identifier - captures all identifiers including function calls
(simple_identifier) @simple_id

; User type in attributes like @MainActor
(user_type (type_identifier) @user_type)
`

var (
	compiledSwiftQuery *treesitter.Query
	swiftQueryOnce     sync.Once
	errSwiftQuery      error
)

func getCompiledSwiftQuery() (*treesitter.Query, error) {
	swiftQueryOnce.Do(func() {
		swiftLang := treesitter.NewLanguage(tree_sitter_swift.Language())

		var qErr *treesitter.QueryError

		compiledSwiftQuery, qErr = treesitter.NewQuery(swiftLang, swiftQuery)
		if qErr != nil {
			errSwiftQuery = qErr
		}
	})

	return compiledSwiftQuery, errSwiftQuery
}

type captureInfo struct {
	text     string
	startRow uint
	startCol uint
	endRow   uint
	endCol   uint
}

//nolint:funlen // tree-sitter capture dispatch: one branch per query capture name.
func getCapturesFromMatches(
	modulePath string,
	filePath string,
	query *treesitter.Query,
	queryCursor *treesitter.QueryCursor,
	tree *treesitter.Tree,
	text []byte,
) captures {
	caps := captures{modulePath: modulePath}

	captureNames := query.CaptureNames()
	matches := queryCursor.Matches(query, tree.RootNode(), text)

	// Track seen import modules to deduplicate imports from conditional
	// compilation blocks (#if/#else/#endif). Both branches are parsed by
	// tree-sitter, but only one will be active at runtime, so we keep only
	// the first occurrence of each module name per file.
	seenImportModules := make(map[string]struct{})
	// Lines (0-based rows) of duplicate imports that were skipped. Usages on
	// these lines are also skipped to avoid double-counting identifiers that
	// appear in conditional compilation branches.
	skippedImportRows := make(map[uint]struct{})

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

		posFromInfo := func(ci captureInfo) analyzer.Position {
			return analyzer.Position{
				File:     filePath,
				Line:     ci.startRow + 1,
				ColStart: ci.startCol + 1,
				ColEnd:   ci.endCol + 1,
			}
		}

		// Import declaration: import Foundation
		// Deduplicate by module name to handle #if/#else conditional compilation.
		if importInfo, ok := captureMap["import_module"]; ok {
			if _, seen := seenImportModules[importInfo.text]; !seen {
				seenImportModules[importInfo.text] = struct{}{}
				caps.imports = append(caps.imports, capturedImport{
					module: importInfo.text,
					pos:    posFromInfo(importInfo),
				})
			} else {
				skippedImportRows[importInfo.startRow] = struct{}{}
			}
		}

		// Type/protocol declaration name: struct User, class Service, enum Status, protocol X
		if declInfo, ok := captureMap["decl_type"]; ok {
			caps.declarations = append(caps.declarations, capturedDeclaration{
				name: declInfo.text,
			})
		}

		// Type usage - type_identifier
		if typeInfo, ok := captureMap["type_usage"]; ok {
			if _, skipped := skippedImportRows[typeInfo.startRow]; !skipped {
				caps.usages = append(caps.usages, capturedUsage{
					expr: typeInfo.text,
					pos:  posFromInfo(typeInfo),
				})
			}
		}

		// Simple identifier - captures function calls, variable names, etc.
		if idInfo, ok := captureMap["simple_id"]; ok {
			if _, skipped := skippedImportRows[idInfo.startRow]; !skipped {
				caps.usages = append(caps.usages, capturedUsage{
					expr: idInfo.text,
					pos:  posFromInfo(idInfo),
				})
			}
		}

		// User type in attributes
		if userTypeInfo, ok := captureMap["user_type"]; ok {
			if _, skipped := skippedImportRows[userTypeInfo.startRow]; !skipped {
				caps.usages = append(caps.usages, capturedUsage{
					expr: userTypeInfo.text,
					pos:  posFromInfo(userTypeInfo),
				})
			}
		}
	}

	return caps
}

// resolveImportPath classifies and resolves an import module name.
// Returns the resolved package key and whether it's internal.
//
// Categories:
// - Foundation, UIKit, SwiftUI → external framework
// - MyTarget (matches a target in workspace) → internal
// - OtherPackage (external SPM dependency) → external.
func resolveImportPath(
	importModule string,
	packageMap map[directory]*packageInfo,
) (string, bool) {
	// Check if it matches a target in any workspace package (internal)
	for _, pi := range packageMap {
		if slices.Contains(pi.targets, importModule) {
			return importModule, true
		}
		// Also check package name itself
		if string(pi.name) == importModule {
			return importModule, true
		}
	}

	// External dependency
	return importModule, false
}

// importNameInfo holds metadata about an imported module for usage site tracking.
type importNameInfo struct {
	pkg      string // resolved package key
	fullPath string // full resolved item path
}

// buildImportNameMap creates a map from local identifiers to their resolved package.
// It maps:
// 1. Module names to themselves (e.g., "Foundation" → Foundation)
// 2. Well-known framework types to their framework (e.g., "URL" → Foundation.URL)
// 3. Symbols exported by internal modules (e.g., "User" → Core.User).
func buildImportNameMap(
	imports []capturedImport,
	packageMap map[directory]*packageInfo,
	moduleExports map[string]map[string]struct{},
) map[string]importNameInfo {
	nameMap := make(map[string]importNameInfo)

	for _, imp := range imports {
		depPkg, _ := resolveImportPath(imp.module, packageMap)
		if depPkg == "" {
			continue
		}

		// Map the module name to itself so module-level references are tracked
		nameMap[imp.module] = importNameInfo{
			pkg:      depPkg,
			fullPath: imp.module,
		}

		// Map exported symbols from internal modules
		if exports, ok := moduleExports[imp.module]; ok {
			for symbolName := range exports {
				nameMap[symbolName] = importNameInfo{
					pkg:      depPkg,
					fullPath: imp.module + "." + symbolName,
				}
			}
		}
	}

	return nameMap
}

// hydrateUsageExpressions records usage sites for imported symbols.
// It matches captured usage identifiers against the import name map and records
// coupling positions for each match.
func hydrateUsageExpressions(
	usages []capturedUsage,
	importedNames map[string]importNameInfo,
	pkgNode *analyzer.PackageAnalysis,
) {
	// Deduplicate usages at the same position to avoid double-counting
	seenUsages := make(map[string]struct{})

	for _, usage := range usages {
		info, exists := importedNames[usage.expr]
		if !exists {
			continue
		}

		// Deduplicate by "expr:line:col"
		key := fmt.Sprintf("%s:%d:%d", usage.expr, usage.pos.Line, usage.pos.ColStart)
		if _, ok := seenUsages[key]; ok {
			continue
		}

		seenUsages[key] = struct{}{}

		importInfo, ok := pkgNode.Out.Get(info.pkg)
		if !ok {
			continue
		}

		importInfo.Add(info.fullPath, info.fullPath, usage.pos)
	}
}

// Phase 4: Analysis orchestration

// fileCaptures stores the parsed captures and metadata for a single Swift file,
// allowing two-pass analysis without re-parsing.
type fileCaptures struct {
	modulePath string
	pi         *packageInfo
	caps       captures
}

// resolveModulePath determines the module path for a Swift file based on its
// package info and directory structure.
func resolveModulePath(swiftFilepath string, pi *packageInfo) string {
	if pi != nil {
		return getModulePath(swiftFilepath, pi)
	}
	// No Package.swift found, use directory-based module path
	dir := filepath.Dir(swiftFilepath)
	if dir == "." {
		return strings.TrimSuffix(filepath.Base(swiftFilepath), ".swift")
	}

	parts := strings.Split(filepath.ToSlash(dir), "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return "main"
}

func analyzeSwiftMetrics(
	ctx context.Context,
	dir fs.FS,
	packageMap map[directory]*packageInfo,
	parsedManifest *manifest.Manifest,
) ([]analyzer.Metrics, []analyzer.PackageBoundary, error) {
	allFileCaptures, moduleExports, pkgDirs, err := collectSwiftFileCaptures(
		ctx,
		dir,
		packageMap,
		parsedManifest,
	)
	if err != nil {
		return nil, nil, err
	}

	slog.DebugContext(ctx, "collected module exports", "moduleExports", moduleExports)

	depGraph := analyzer.NewPackageAnalysisTree()

	// Pass 2: Register imports, build import name maps with module exports,
	// and hydrate usage expressions.
	for _, fileCapture := range allFileCaptures {
		// Get or init the package node
		var pkgNode *analyzer.PackageAnalysis
		if pn, exists := depGraph.Get(fileCapture.modulePath); exists {
			pkgNode = pn
		} else {
			pkgNode = depGraph.Add(fileCapture.modulePath)
		}

		registerSwiftImports(pkgNode, fileCapture.caps.imports, parsedManifest, packageMap)

		// When a manifest is present, filter system imports before building
		// the name map so that framework types (e.g. URL from Foundation)
		// don't inflate coupling.
		effectiveImports := filterSystemImports(fileCapture.caps.imports, parsedManifest)

		// Build import name lookup map (includes framework types + internal module exports)
		// and hydrate usage expressions with coupling positions
		importedNames := buildImportNameMap(effectiveImports, packageMap, moduleExports)
		hydrateUsageExpressions(fileCapture.caps.usages, importedNames, pkgNode)
	}

	boundaries := buildSwiftBoundaries(pkgDirs)

	return buildSwiftMetrics(ctx, depGraph), boundaries, nil
}

// collectSwiftFileCaptures parses every Swift file once, collecting per-file
// captures, the module→directories map for boundary computation, and the
// module→exported-symbols map used to resolve internal usages.
func collectSwiftFileCaptures(
	ctx context.Context,
	dir fs.FS,
	packageMap map[directory]*packageInfo,
	parsedManifest *manifest.Manifest,
) ([]fileCaptures, map[string]map[string]struct{}, map[string]map[string]struct{}, error) {
	swiftFilepaths, err := listSwiftFiles(ctx, dir)
	if err != nil {
		return nil, nil, nil, err
	}

	tsparser, compiledQuery, err := newSwiftParser()
	if err != nil {
		return nil, nil, nil, err
	}

	defer tsparser.Close()

	pathInterner := analyzer.NewStringInterner()

	var targetNames map[string]struct{}
	if parsedManifest != nil {
		targetNames = parsedManifest.TargetNames()
	}

	pkgDirs := make(map[string]map[string]struct{})
	moduleExports := make(map[string]map[string]struct{})
	allFileCaptures := make([]fileCaptures, 0, len(swiftFilepaths))

	for _, swiftFilepath := range swiftFilepaths {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}

		swiftFilepath = pathInterner.Intern(swiftFilepath)
		pkgInfo := getPackageForFile(swiftFilepath, packageMap)
		modulePath := resolveModulePath(swiftFilepath, pkgInfo)

		modulePath, skip := resolveSwiftTargetModule(
			modulePath,
			swiftFilepath,
			targetNames,
			parsedManifest,
		)
		if skip {
			continue
		}

		fileDir := filepath.Dir(swiftFilepath)

		if pkgDirs[modulePath] == nil {
			pkgDirs[modulePath] = make(map[string]struct{})
		}

		pkgDirs[modulePath][fileDir] = struct{}{}

		caps, err := parseSwiftCaptures(
			ctx,
			tsparser,
			compiledQuery,
			dir,
			swiftFilepath,
			modulePath,
		)
		if err != nil {
			return nil, nil, nil, err
		}

		recordModuleExports(moduleExports, modulePath, caps)

		allFileCaptures = append(allFileCaptures, fileCaptures{
			modulePath: modulePath,
			pi:         pkgInfo,
			caps:       caps,
		})
	}

	return allFileCaptures, moduleExports, pkgDirs, nil
}

// newSwiftParser creates a tree-sitter parser configured for Swift and compiles
// the shared usage query. The caller owns closing the returned parser.
func newSwiftParser() (*treesitter.Parser, *treesitter.Query, error) {
	tsparser := treesitter.NewParser()

	swiftLanguage := treesitter.NewLanguage(tree_sitter_swift.Language())
	if err := tsparser.SetLanguage(swiftLanguage); err != nil {
		tsparser.Close()

		return nil, nil, err
	}

	compiledQuery, err := getCompiledSwiftQuery()
	if err != nil {
		tsparser.Close()

		return nil, nil, err
	}

	return tsparser, compiledQuery, nil
}

// parseSwiftCaptures parses a single Swift file and extracts its captures.
func parseSwiftCaptures(
	ctx context.Context,
	tsparser *treesitter.Parser,
	compiledQuery *treesitter.Query,
	dir fs.FS,
	filePath string,
	modulePath string,
) (captures, error) {
	tree, text, err := ts.Parse(ctx, tsparser, dir, filePath)
	if err != nil {
		return captures{}, slogerr.New(err,
			logschema.UdaAnalyzerFile(filePath),
			logschema.UdaAnalyzerLanguage("Swift"),
			logschema.UdaErrorPhase("parse"),
		)
	}

	queryCursor := treesitter.NewQueryCursor()
	caps := getCapturesFromMatches(modulePath, filePath, compiledQuery, queryCursor, tree, text)

	tree.Close()
	queryCursor.Close()

	return caps, nil
}

// buildSwiftBoundaries converts the module→directories map into package boundaries.
func buildSwiftBoundaries(pkgDirs map[string]map[string]struct{}) []analyzer.PackageBoundary {
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

	return boundaries
}

// buildSwiftMetrics resolves inward dependencies and computes per-package metrics.
func buildSwiftMetrics(
	ctx context.Context,
	depGraph *analyzer.PackageAnalysisTree,
) []analyzer.Metrics {
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

	return finalMetrics
}

// resolveSwiftTargetModule folds a module path into its declared target when a
// manifest is available. It returns the resolved module path and whether the
// file should be skipped because it belongs to no declared target. When no
// manifest is present (targetNames == nil) the module path is returned as-is.
func resolveSwiftTargetModule(
	modulePath string,
	filePath string,
	targetNames map[string]struct{},
	parsedManifest *manifest.Manifest,
) (string, bool) {
	if targetNames == nil {
		return modulePath, false
	}

	if _, ok := targetNames[modulePath]; ok {
		return modulePath, false
	}

	// getModulePath already returns the second path component (e.g. "NukeTests"),
	// which is the target name for files under Sources/ or Tests/. Non-standard
	// layouts may produce module paths that don't match any target — fold those
	// into the parent target via the manifest's TargetForDir.
	if t := parsedManifest.TargetForDir(filepath.Dir(filePath)); t != nil {
		return t.Name, false
	}

	// File is not in any declared target — skip it. Non-target directories
	// (helpers, mocks, fixtures) are support files, not packages.
	return modulePath, true
}

// recordModuleExports adds a file's exported declarations to the module-exports map.
func recordModuleExports(
	moduleExports map[string]map[string]struct{},
	modulePath string,
	caps captures,
) {
	if len(caps.declarations) == 0 {
		return
	}

	if _, ok := moduleExports[modulePath]; !ok {
		moduleExports[modulePath] = make(map[string]struct{})
	}

	for _, decl := range caps.declarations {
		moduleExports[modulePath][decl.name] = struct{}{}
	}
}

// registerSwiftImports records a file's non-system imports as outward dependencies.
// System frameworks are skipped when a manifest is available because they inflate
// outward coupling without representing a real design choice.
func registerSwiftImports(
	pkgNode *analyzer.PackageAnalysis,
	imports []capturedImport,
	parsedManifest *manifest.Manifest,
	packageMap map[directory]*packageInfo,
) {
	for _, imp := range imports {
		if parsedManifest != nil && ClassifyImport(imp.module, parsedManifest) == ImportSystem {
			continue
		}

		depPkg, _ := resolveImportPath(imp.module, packageMap)
		if depPkg == "" {
			continue
		}

		pkgNode.Out.Add(depPkg)
	}
}

// filterSystemImports drops system-framework imports when a manifest is present so
// framework types (e.g. URL from Foundation) don't inflate coupling. Without a
// manifest the imports are returned unchanged.
func filterSystemImports(
	imports []capturedImport,
	parsedManifest *manifest.Manifest,
) []capturedImport {
	if parsedManifest == nil {
		return imports
	}

	filtered := make([]capturedImport, 0, len(imports))
	for _, imp := range imports {
		if ClassifyImport(imp.module, parsedManifest) != ImportSystem {
			filtered = append(filtered, imp)
		}
	}

	return filtered
}
