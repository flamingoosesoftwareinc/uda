// Package golang implements the Go source analyzer (package + import + symbol tracking).
package golang

import (
	"context"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	files "github.com/flamingoosesoftwareinc/fsift"
	"github.com/flamingoosesoftwareinc/slogerr"
	tsgomod "github.com/flamingoosesoftwareinc/tree-sitter-go-mod/bindings/go"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/logschema"
	"github.com/flamingoosesoftwareinc/uda/internal/ts"
	treesitter "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// Query strings for tree-sitter.
var gomodQueryStr = `(module_directive (module_path) @module_path)`

var goQueryStr = `
(package_clause (package_identifier) @package)
(import_spec
	path: (interpreted_string_literal
		(interpreted_string_literal_content) @import
	)
)
(import_spec
	name: (package_identifier)
	path: (interpreted_string_literal)) @import_alias
(qualified_type
	package: (package_identifier)) @import_type_use
(selector_expression) @import_func_use
`

// Compiled query singletons.
var (
	compiledGomodQuery *treesitter.Query
	compiledGoQuery    *treesitter.Query
	gomodQueryOnce     sync.Once
	goQueryOnce        sync.Once
	errGomodQuery      error
	errGoQuery         error
)

func getCompiledGomodQuery() (*treesitter.Query, error) {
	gomodQueryOnce.Do(func() {
		lang := treesitter.NewLanguage(tsgomod.Language())

		var qErr *treesitter.QueryError

		compiledGomodQuery, qErr = treesitter.NewQuery(lang, gomodQueryStr)
		if qErr != nil {
			errGomodQuery = qErr
		}
	})

	return compiledGomodQuery, errGomodQuery
}

func getCompiledGoQuery() (*treesitter.Query, error) {
	goQueryOnce.Do(func() {
		lang := treesitter.NewLanguage(tsgo.Language())

		var qErr *treesitter.QueryError

		compiledGoQuery, qErr = treesitter.NewQuery(lang, goQueryStr)
		if qErr != nil {
			errGoQuery = qErr
		}
	})

	return compiledGoQuery, errGoQuery
}

type goAnalyzer struct {
	boundaries []analyzer.PackageBoundary
}

var (
	_ analyzer.Analyzer         = &goAnalyzer{}
	_ analyzer.BoundaryProvider = &goAnalyzer{}
)

// GoAnalyzer returns a Go source analyzer (satisfies analyzer.Analyzer and
// analyzer.BoundaryProvider). Concrete pointer return so tests can call
// Boundaries without a type assertion; cmd uses the interface upcast.
//
//nolint:revive // unexported-return: the concrete type is intentionally package-private; consumers upcast to analyzer.Analyzer / BoundaryProvider.
func GoAnalyzer() *goAnalyzer {
	return &goAnalyzer{}
}

func (g *goAnalyzer) Name() string { return "Go" }

func (g *goAnalyzer) Analyze(ctx context.Context, dir fs.FS) ([]analyzer.Metrics, error) {
	gomodFiles, err := listGomodFiles(ctx, dir)
	if err != nil {
		return nil, err
	}

	slog.LogAttrs(ctx, slog.LevelDebug, "found go.mod files",
		logschema.UdaAnalyzerFilepaths(gomodFiles),
	)

	gomodPaths, err := extractModulePaths(ctx, dir, gomodFiles)
	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, "identified go module paths", "paths", gomodPaths)

	metrics, boundaries, err := analyzeGoMetrics(ctx, dir, gomodPaths)
	if err != nil {
		return nil, err
	}

	g.boundaries = boundaries

	return metrics, nil
}

// Boundaries returns the filesystem directories for each analyzed package.
// Lazily invokes Analyze() if boundaries have not been populated yet.
func (g *goAnalyzer) Boundaries(
	ctx context.Context,
	dir fs.FS,
) ([]analyzer.PackageBoundary, error) {
	if g.boundaries == nil {
		if _, err := g.Analyze(ctx, dir); err != nil {
			return nil, err
		}
	}

	return g.boundaries, nil
}

func listGomodFiles(ctx context.Context, dir fs.FS) ([]string, error) {
	return files.ListFiles(
		ctx,
		dir,
		files.SkipHiddenDirs(),
		files.SkipHiddenFiles(),
		gomodFileFilter(),
	)
}

func gomodFileFilter() files.FileFilter {
	return func(path string, d fs.DirEntry) bool {
		if d.IsDir() {
			return false
		}

		return filepath.Base(path) != "go.mod"
	}
}

type (
	directory  string
	modulePath string
)

func extractModulePaths(
	ctx context.Context,
	dir fs.FS,
	gomodFilepaths []string,
) (map[directory]modulePath, error) {
	tsparser := treesitter.NewParser()
	defer tsparser.Close()

	gomodLanguage := treesitter.NewLanguage(tsgomod.Language())
	if err := tsparser.SetLanguage(gomodLanguage); err != nil {
		return nil, err
	}

	// Get compiled query (compiled once, reused)
	compiledQuery, err := getCompiledGomodQuery()
	if err != nil {
		return nil, err
	}

	gomodPaths := make(map[directory]modulePath, len(gomodFilepaths))

	for _, gmFilepath := range gomodFilepaths {
		tree, text, err := ts.Parse(ctx, tsparser, dir, gmFilepath)
		if err != nil {
			return nil, slogerr.New(err,
				logschema.UdaAnalyzerFile(gmFilepath),
				logschema.UdaAnalyzerLanguage("Go"),
				logschema.UdaErrorPhase("parse"),
			)
		}

		queryCursor := treesitter.NewQueryCursor()
		matches := queryCursor.Matches(compiledQuery, tree.RootNode(), text)

		for match := matches.Next(); match != nil; match = matches.Next() {
			for _, capture := range match.Captures {
				node := capture.Node
				gomodPaths[directory(filepath.Dir(gmFilepath))] = modulePath(node.Utf8Text(text))
			}
		}

		// Free tree-sitter objects (query is singleton, don't close it)
		tree.Close()
		queryCursor.Close()
	}

	return gomodPaths, nil
}

type capturedAlias struct {
	name string
	pkg  string
}

type capturedExpr struct {
	expr string
	pos  analyzer.Position
}

type captures struct {
	p                  analyzer.Package
	i                  []analyzer.Import
	aliases            []capturedAlias
	qualifiedTypesUsed []capturedExpr
	selectExpressions  []capturedExpr
}

//nolint:funlen // per-file walk: parse + capture + hydrate + dedup + boundaries.
func analyzeGoMetrics(
	ctx context.Context,
	dir fs.FS,
	gomodPaths map[directory]modulePath,
) ([]analyzer.Metrics, []analyzer.PackageBoundary, error) {
	depGraph := analyzer.NewPackageAnalysisTree()

	goFilepaths, err := listGoFiles(ctx, dir)
	if err != nil {
		return nil, nil, err
	}

	tsparser := treesitter.NewParser()
	defer tsparser.Close()

	goLanguage := treesitter.NewLanguage(tsgo.Language())
	if err := tsparser.SetLanguage(goLanguage); err != nil {
		return nil, nil, err
	}

	// Get compiled query (compiled once, reused)
	compiledQuery, err := getCompiledGoQuery()
	if err != nil {
		return nil, nil, err
	}

	// Intern file paths to reduce memory usage in Position structs
	pathInterner := analyzer.NewStringInterner()

	// Track package → directories for boundary computation
	pkgDirs := make(map[string]map[string]bool)

	for _, goFilepath := range goFilepaths {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		// Intern the file path so all positions share the same string
		goFilepath = pathInterner.Intern(goFilepath)

		pkgPathPrefix := getPkgPathPrefix(goFilepath, gomodPaths)

		tree, text, err := ts.Parse(ctx, tsparser, dir, goFilepath)
		if err != nil {
			return nil, nil, slogerr.New(err,
				logschema.UdaAnalyzerFile(goFilepath),
				logschema.UdaAnalyzerLanguage("Go"),
				logschema.UdaErrorPhase("parse"),
			)
		}

		qc := treesitter.NewQueryCursor()
		caps := getCaptures(ctx, pkgPathPrefix, goFilepath, compiledQuery, qc, tree, text)

		// Free tree-sitter objects (query is singleton, don't close it)
		tree.Close()
		qc.Close()

		// Collect directory for boundary mapping
		pkgName := string(caps.p)
		if pkgName != "" {
			fileDir := filepath.Dir(goFilepath)

			if pkgDirs[pkgName] == nil {
				pkgDirs[pkgName] = make(map[string]bool)
			}

			pkgDirs[pkgName][fileDir] = true
		}

		pkgNode := getOrInitPkgNode(ctx, depGraph, caps)
		hydrateOutNode(caps.i, pkgNode.Out)
		hydrateAliases(ctx, caps.aliases, pkgNode.Aliases, pkgNode.Out)
		hydrateQualifiedExpressions(ctx, caps.qualifiedTypesUsed, pkgNode.Aliases, pkgNode.Out)
		hydrateQualifiedExpressions(ctx, caps.selectExpressions, pkgNode.Aliases, pkgNode.Out)
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

//nolint:funlen // tree-sitter capture dispatch: one branch per query capture name.
func getCaptures(
	ctx context.Context,
	pkgPathPrefix modulePath,
	filePath string,
	query *treesitter.Query,
	queryCursor *treesitter.QueryCursor,
	tree *treesitter.Tree,
	text []byte,
) captures {
	caps := captures{}

	captureNames := query.CaptureNames()
	matches := queryCursor.Matches(query, tree.RootNode(), text)
	capturePairs := ts.ProcessMatches(ctx, matches, captureNames, text)

	for _, capturePair := range capturePairs {
		switch capturePair.CaptureName {
		case "package":
			detectedPkgName := capturePair.NodeStr
			defaultPkgPath := string(pkgPathPrefix)

			dir := filepath.Dir(defaultPkgPath)
			if detectedPkgName == filepath.Base(defaultPkgPath) {
				caps.p = analyzer.Package(filepath.Join(dir, detectedPkgName))
			} else {
				caps.p = analyzer.Package(filepath.Join(defaultPkgPath, detectedPkgName))
			}

			slog.DebugContext(
				ctx,
				"pkg names",
				"detectedPkgName",
				detectedPkgName,
				"defaultPkgPath",
				defaultPkgPath,
				"pkgPath",
				caps.p,
			)
		case "import":
			if isStdlib(capturePair.NodeStr) {
				continue
			}

			slog.LogAttrs(ctx, slog.LevelDebug, "import detected",
				logschema.UdaParseImport(capturePair.NodeStr),
			)
			caps.i = append(caps.i, analyzer.Import(capturePair.NodeStr))
		case "import_alias":
			aliasName, quotedAliasedPackage, _ := strings.Cut(capturePair.NodeStr, " ")

			aliasedPackage, err := strconv.Unquote(quotedAliasedPackage)
			if err != nil {
				slog.DebugContext(
					ctx,
					"getCaptures error unquoting aliased package",
					"aliasedPackage",
					quotedAliasedPackage,
					"error",
					err,
				)
				aliasedPackage = quotedAliasedPackage
			}

			if isStdlib(aliasedPackage) {
				continue
			}

			slog.LogAttrs(ctx, slog.LevelDebug, "alias detected",
				logschema.UdaParseAlias(aliasName),
				logschema.UdaParseImport(aliasedPackage),
			)
			caps.aliases = append(
				caps.aliases,
				capturedAlias{name: aliasName, pkg: aliasedPackage},
			)
		case "import_func_use":
			slog.LogAttrs(ctx, slog.LevelDebug, "import_func_use detected",
				logschema.UdaParseExpression(capturePair.NodeStr),
			)
			caps.selectExpressions = append(caps.selectExpressions, capturedExpr{
				expr: capturePair.NodeStr,
				pos: analyzer.Position{
					File:     filePath,
					Line:     capturePair.StartRow + 1,
					ColStart: capturePair.StartCol + 1,
					ColEnd:   capturePair.EndCol + 1,
				},
			})
		case "import_type_use":
			slog.LogAttrs(ctx, slog.LevelDebug, "import_type_use detected",
				logschema.UdaParseExpression(capturePair.NodeStr),
			)
			caps.qualifiedTypesUsed = append(caps.qualifiedTypesUsed, capturedExpr{
				expr: capturePair.NodeStr,
				pos: analyzer.Position{
					File:     filePath,
					Line:     capturePair.StartRow + 1,
					ColStart: capturePair.StartCol + 1,
					ColEnd:   capturePair.EndCol + 1,
				},
			})
		default:
			slog.DebugContext(
				ctx,
				"unknown capture name",
				"captureName",
				capturePair.CaptureName,
				"value",
				capturePair.NodeStr,
			)
		}
	}

	// Deduplicate chained selector expressions. Tree-sitter captures every
	// nested selector_expression in a method chain (e.g. table.New, table.New().Border,
	// table.New().Border().Headers). Keep only the outermost (longest) expression
	// for each starting position.
	caps.selectExpressions = deduplicateChainedExprs(caps.selectExpressions)

	return caps
}

// deduplicateChainedExprs keeps only the shortest (innermost) expression for
// each starting position. Tree-sitter captures every nested selector_expression
// in a method chain (e.g. table.New, table.New().Border, table.New().Border().Rows).
// The innermost expression (table.New) is the actual package-level symbol; the
// rest are methods on returned structs, not separate external dependencies.
func deduplicateChainedExprs(exprs []capturedExpr) []capturedExpr {
	type posKey struct {
		file     string
		line     uint
		colStart uint
	}

	shortest := make(map[posKey]int) // posKey → index into exprs

	for i, ce := range exprs {
		k := posKey{file: ce.pos.File, line: ce.pos.Line, colStart: ce.pos.ColStart}
		if prev, ok := shortest[k]; ok {
			if len(ce.expr) < len(exprs[prev].expr) {
				shortest[k] = i
			}
		} else {
			shortest[k] = i
		}
	}

	deduped := make([]capturedExpr, 0, len(shortest))
	for _, idx := range shortest {
		deduped = append(deduped, exprs[idx])
	}

	return deduped
}

func listGoFiles(ctx context.Context, dir fs.FS) ([]string, error) {
	return files.ListFiles(
		ctx,
		dir,
		files.SkipHiddenDirs(),
		files.SkipHiddenFiles(),
		goFileFilter(),
	)
}

func goFileFilter() files.FileFilter {
	return func(path string, d fs.DirEntry) bool {
		if d.IsDir() {
			return false
		}

		return filepath.Ext(path) != ".go"
	}
}

func getPkgPathPrefix(goFilepath string, gomodPaths map[directory]modulePath) modulePath {
	goFileDir := filepath.Dir(goFilepath)
	for fileDir := goFileDir; ; fileDir = filepath.Dir(fileDir) {
		modPath, ok := gomodPaths[directory(fileDir)]
		if ok {
			rel, _ := filepath.Rel(fileDir, goFileDir)
			if rel == "." {
				return modPath
			}

			return modulePath(filepath.Join(string(modPath), rel))
		}

		if fileDir == "." {
			break
		}
	}

	return modulePath(goFileDir)
}

func getOrInitPkgNode(
	ctx context.Context,
	depGraph *analyzer.PackageAnalysisTree,
	c captures,
) *analyzer.PackageAnalysis {
	pkgNode, exists := depGraph.Get(string(c.p))
	if !exists {
		pkgNode = depGraph.Add(string(c.p))
	}

	slog.DebugContext(ctx, "getOrInitPkgNode", "pkgNode", pkgNode.Key())

	return pkgNode
}

func hydrateOutNode(i []analyzer.Import, outNode *analyzer.PackageImportInfo) {
	for _, im := range i {
		outNode.Add(string(im))
	}
}

func hydrateAliases(
	ctx context.Context,
	aliases []capturedAlias,
	aliasMap map[string]string,
	outNode *analyzer.PackageImportInfo,
) {
	slog.DebugContext(ctx, "hydrateAliases", "aliases", aliases)

	for _, alias := range aliases {
		if _, exists := aliasMap[alias.name]; !exists {
			aliasMap[alias.name] = alias.pkg
		}

		outNode.Add(alias.pkg)
	}
}

func hydrateQualifiedExpressions(
	ctx context.Context,
	expr []capturedExpr,
	aliasMap map[string]string,
	outNode *analyzer.PackageImportInfo,
) {
	slog.DebugContext(
		ctx,
		"hydrateQualifiedExpressions",
		"expr",
		expr,
	)

	for _, exprCapture := range expr {
		qualifier, qualifiedType, _ := strings.Cut(exprCapture.expr, ".")

		associatedImportInfo, resolvedQualifier, exists := resolveQualifierPackage(
			qualifier,
			aliasMap,
			outNode,
		)
		if !exists {
			slog.DebugContext(
				ctx,
				"hydrateQualifiedExpressions could not resolve package",
				"qualifier",
				qualifier,
			)

			continue
		}

		resolvedQualifiedExpr := strings.Join([]string{resolvedQualifier, qualifiedType}, ".")
		associatedImportInfo.Add(resolvedQualifiedExpr, resolvedQualifiedExpr, exprCapture.pos)
	}
}

func resolveQualifierPackage(
	qualifier string,
	aliasMap map[string]string,
	outNode *analyzer.PackageImportInfo,
) (*analyzer.ImportInfo, string, bool) {
	// If the qualifier is an alias, resolve to the actual package path
	pkg, isAlias := aliasMap[qualifier]
	if isAlias {
		if importInfo, exists := outNode.Get(pkg); exists {
			return importInfo, filepath.Base(pkg), true
		}

		return nil, "", false
	}

	// Otherwise, match qualifier against basename of imported packages
	for _, importInfo := range outNode.GetChildren() {
		if qualifier == filepath.Base(importInfo.Key()) {
			return importInfo, qualifier, true
		}
	}

	return nil, "", false
}
