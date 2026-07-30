// Package tscore is the shared TypeScript parse + capture engine used by the boundary-strategy implementations.
package tscore

import (
	"context"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/flamingoosesoftwareinc/slogerr"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/logschema"
	"github.com/flamingoosesoftwareinc/uda/internal/ts"
	treesitter "github.com/tree-sitter/go-tree-sitter"
)

// AnalyzeMetrics walks the TypeScript project, builds the dependency graph using
// the given boundary assigner, and returns per-package metrics and boundaries.
func AnalyzeMetrics(
	ctx context.Context,
	dir fs.FS,
	factory AssignerFactory,
) ([]analyzer.Metrics, []analyzer.PackageBoundary, error) {
	project, err := discoverTsProject(ctx, dir, factory)
	if err != nil {
		return nil, nil, err
	}

	tsparser := treesitter.NewParser()
	defer tsparser.Close()

	run := &tsRun{
		project:      project,
		depGraph:     analyzer.NewPackageAnalysisTree(),
		tsparser:     tsparser,
		pathInterner: analyzer.NewStringInterner(),
		pkgDirs:      make(map[string]map[string]struct{}),
	}

	for _, tsFilepath := range project.tsFilepaths {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		if err := run.processFile(ctx, dir, tsFilepath); err != nil {
			return nil, nil, err
		}
	}

	boundaries := buildTsBoundaries(run.pkgDirs)

	return buildTsMetrics(ctx, run.depGraph), boundaries, nil
}

// tsRun holds the mutable state threaded through a single TypeScript analysis pass.
type tsRun struct {
	project        *tsProject
	depGraph       *analyzer.PackageAnalysisTree
	tsparser       *treesitter.Parser
	pathInterner   *analyzer.StringInterner
	pkgDirs        map[string]map[string]struct{}
	currentLangPtr unsafe.Pointer
}

// processFile parses one TypeScript file and folds its imports and usages into
// the dependency graph and boundary map.
func (r *tsRun) processFile(ctx context.Context, dir fs.FS, tsFilepath string) error {
	tsFilepath = r.pathInterner.Intern(tsFilepath)

	filePkgPath := r.project.assigner.AssignBoundary(tsFilepath, r.project.pkgNames)
	fileDir := filepath.Dir(tsFilepath)

	if r.pkgDirs[filePkgPath] == nil {
		r.pkgDirs[filePkgPath] = make(map[string]struct{})
	}

	r.pkgDirs[filePkgPath][fileDir] = struct{}{}

	langPtr := LanguageForFile(tsFilepath)
	if langPtr != r.currentLangPtr {
		if err := r.tsparser.SetLanguage(treesitter.NewLanguage(langPtr)); err != nil {
			return err
		}

		r.currentLangPtr = langPtr
	}

	tree, text, err := ts.Parse(ctx, r.tsparser, dir, tsFilepath)
	if err != nil {
		return slogerr.New(err,
			logschema.UdaAnalyzerFile(tsFilepath),
			logschema.UdaAnalyzerLanguage("TypeScript"),
			logschema.UdaErrorPhase("parse"),
		)
	}

	compiledQuery, err := GetCompiledQuery(tsFilepath)
	if err != nil {
		return err
	}

	queryCursor := treesitter.NewQueryCursor()
	caps := GetCapturesFromMatches(filePkgPath, tsFilepath, compiledQuery, queryCursor, tree, text)
	tree.Close()
	queryCursor.Close()

	pkgNode := r.pkgNodeFor(filePkgPath)
	fileAliases := findFileAliases(fileDir, r.project.aliases)
	resolvedPaths := resolveImportPaths(
		caps.Imports,
		tsFilepath,
		filePkgPath,
		r.project.pkgNames,
		fileAliases,
		r.project.allFilePaths,
		r.project.assigner,
	)

	HydrateImports(ctx, caps.Imports, resolvedPaths, pkgNode)
	importedNames := BuildImportNameMap(caps.Imports, resolvedPaths)
	HydrateIdentifierUsages(ctx, caps.IdentUsages, importedNames, pkgNode)
	HydrateMemberExpressions(ctx, caps.MemberExprs, pkgNode)

	return nil
}

// pkgNodeFor returns the existing dependency-graph node for filePkgPath, creating one if absent.
func (r *tsRun) pkgNodeFor(filePkgPath string) *analyzer.PackageAnalysis {
	if pn, exists := r.depGraph.Get(filePkgPath); exists {
		return pn
	}

	return r.depGraph.Add(filePkgPath)
}

// tsProject holds the discovered inputs for a TypeScript analysis run.
type tsProject struct {
	pkgNames     map[Directory]PackageName
	aliases      map[Directory][]PathAlias
	tsFilepaths  []string
	allFilePaths map[string]struct{}
	assigner     BoundaryAssigner
}

// discoverTsProject finds package.json/tsconfig/source files, extracts package
// names and path aliases, and builds the boundary assigner for the run.
func discoverTsProject(
	ctx context.Context,
	dir fs.FS,
	factory AssignerFactory,
) (*tsProject, error) {
	pkgJSONFiles, err := ListPackageJSONFiles(ctx, dir)
	if err != nil {
		return nil, err
	}

	slog.LogAttrs(ctx, slog.LevelDebug, "found package.json files",
		logschema.UdaAnalyzerFilepaths(pkgJSONFiles),
	)

	pkgNames, err := ExtractPackageNames(ctx, dir, pkgJSONFiles)
	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, "identified package names", "names", pkgNames)

	tsconfigFiles, err := ListTsconfigFiles(ctx, dir)
	if err != nil {
		return nil, err
	}

	slog.LogAttrs(ctx, slog.LevelDebug, "found tsconfig.json files",
		logschema.UdaAnalyzerFilepaths(tsconfigFiles),
	)

	aliases, err := ExtractPathAliases(ctx, dir, tsconfigFiles)
	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, "extracted path aliases", "aliases", aliases)

	excludePatterns := ExtractExcludePatterns(ctx, dir, tsconfigFiles)
	slog.DebugContext(ctx, "extracted exclude patterns", "patterns", excludePatterns)

	tsFilepaths, err := ListTsFiles(ctx, dir, excludePatterns)
	if err != nil {
		return nil, err
	}

	assigner, err := factory(tsFilepaths)
	if err != nil {
		return nil, err
	}

	allFilePaths := make(map[string]struct{}, len(tsFilepaths))
	for _, fp := range tsFilepaths {
		allFilePaths[fp] = struct{}{}
	}

	return &tsProject{
		pkgNames:     pkgNames,
		aliases:      aliases,
		tsFilepaths:  tsFilepaths,
		allFilePaths: allFilePaths,
		assigner:     assigner,
	}, nil
}

// buildTsBoundaries converts the boundary→directories map into package boundaries.
func buildTsBoundaries(pkgDirs map[string]map[string]struct{}) []analyzer.PackageBoundary {
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

// buildTsMetrics resolves inward dependencies and computes per-package metrics.
func buildTsMetrics(
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

// findFileAliases returns the tsconfig path aliases that apply to fileDir by
// walking up to the nearest ancestor directory that declares aliases.
func findFileAliases(fileDir string, aliases map[Directory][]PathAlias) []PathAlias {
	for dir := fileDir; ; dir = filepath.Dir(dir) {
		if a, ok := aliases[Directory(dir)]; ok {
			return a
		}

		if dir == "." || dir == filepath.Dir(dir) {
			return nil
		}
	}
}

func resolveImportPaths(
	imports []CapturedImport,
	currentFilePath string,
	currentBoundary string,
	pkgNames map[Directory]PackageName,
	aliases []PathAlias,
	allFilePaths map[string]struct{},
	assigner BoundaryAssigner,
) map[string]string {
	resolved := make(map[string]string, len(imports))

	for _, imp := range imports {
		if _, done := resolved[imp.Source]; done {
			continue
		}

		file := ResolveImportToFile(imp.Source, currentFilePath, aliases, allFilePaths)
		if file == "" {
			resolved[imp.Source] = matchPackageName(imp.Source, pkgNames)

			continue
		}

		targetBoundary := assigner.AssignBoundary(file, pkgNames)
		if targetBoundary == currentBoundary {
			// Same boundary — internal import, skip
			continue
		}

		resolved[imp.Source] = targetBoundary
	}

	return resolved
}

func matchPackageName(importSource string, pkgNames map[Directory]PackageName) string {
	best := importSource
	bestLen := 0

	for _, name := range pkgNames {
		n := string(name)
		if n == importSource ||
			(strings.HasPrefix(importSource, n) && importSource[len(n)] == '/') {
			if len(n) > bestLen {
				best = n
				bestLen = len(n)
			}
		}
	}

	return best
}
