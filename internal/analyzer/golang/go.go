package golang

import (
	"context"
	"io/fs"
	"log/slog"
	"path/filepath"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/files"
	"github.com/flamingoosesoftwareinc/uda/internal/ts"
	treesitter "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tsgomod "github.com/tree-sitter/tree-sitter-gomod/bindings/go"
)

type goAnalyzer struct{}

var _ analyzer.Analyzer = &goAnalyzer{}

func GoAnalyzer() *goAnalyzer {
	return &goAnalyzer{}
}

func (g *goAnalyzer) Analyze(ctx context.Context, dir fs.FS) (analyzer.PackageImports, error) {
	// find go.mod file(s) to get the module scope and path
	// e.g. "." "github.com/flamingoosesoftwareinc/uda"
	// e.g. "./go" "github.com/blahblahblah/asdf"
	// for directories with multiple go.mod, the parent directory of the go.mod file represents the boundary of the go module
	// so we know that all files under said directory should be scope to the associated module path
	// github.com/camdencheek/tree-sitter-go-mod
	// (module_directive (module_path) @module_path) to parse out the module path
	//
	// filter out file paths that are not .go
	gomodFiles, err := listGomodFiles(ctx, dir)
	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, "found go.mod files", "filepaths", gomodFiles)

	gomodPaths, err := extractModulePaths(ctx, dir, gomodFiles)
	if err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "identified go module paths", "paths", gomodPaths)

	// per module scope, path
	// read all files
	// parse using treesitter
	// query using treesitter to extract Package and Imports
	// prefix package name based on module scope and package parent dirs
	// e.g. if module is github.com/ahmedalhulaibi/foo and package is "cli", dir is "go/internal/cli" and it imports "treesitter", "context", "io/fs", "github.com/asdfasdf/aoiso"
	// then the result would be "github.com/ahmedalhulaibi/foo/go/internal/cli": []string{"treesitter", "context", "io/fs", "github.com/asdfasdf/aoiso"}

	// TODO: count the number of imported types/functions and the number of exported types/functions
	// wip query:
	// (qualified_type (_ (package_identifier))) @package_type_use
	// (selector_expression) @package_func_use
	// need a query for capturing exported types

	return analyzeGoFiles(ctx, dir, gomodPaths)
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

	query := `(module_directive (module_path) @module_path)`
	gomodPaths := make(map[directory]modulePath, len(gomodFilepaths))

	for _, gmFilepath := range gomodFilepaths {
		tree, text, err := ts.Parse(ctx, tsparser, dir, gmFilepath)
		if err != nil {
			return nil, err
		}

		q, qc, err := ts.Query(ctx, tsparser, gomodLanguage, tree, text, query)
		if err != nil {
			return nil, err
		}

		matches := qc.Matches(q, tree.RootNode(), text)

		for match := matches.Next(); match != nil; match = matches.Next() {
			for _, capture := range match.Captures {
				node := capture.Node
				gomodPaths[directory(filepath.Dir(gmFilepath))] = modulePath(node.Utf8Text(text))
			}
		}
	}

	return gomodPaths, nil
}

func analyzeGoFiles(
	ctx context.Context,
	dir fs.FS,
	gomodPaths map[directory]modulePath,
) (analyzer.PackageImports, error) {
	goFilepaths, err := listGoFiles(ctx, dir)
	if err != nil {
		return nil, err
	}

	tsparser := treesitter.NewParser()
	defer tsparser.Close()

	goLanguage := treesitter.NewLanguage(tsgo.Language())
	if err := tsparser.SetLanguage(goLanguage); err != nil {
		return nil, err
	}

	query := `
(package_clause (package_identifier) @package) 
(import_spec
		path: (interpreted_string_literal)) @import
		`

	pi := make(analyzer.PackageImports)

	for _, goFilepath := range goFilepaths {
		pkgPathPrefix := getPkgPathPrefix(goFilepath, gomodPaths)

		tree, text, err := ts.Parse(ctx, tsparser, dir, goFilepath)
		if err != nil {
			return nil, err
		}

		q, qc, err := ts.Query(ctx, tsparser, goLanguage, tree, text, query)
		if err != nil {
			return nil, err
		}

		captureNames := q.CaptureNames()

		matches := qc.Matches(q, tree.RootNode(), text)

		pkgPath := analyzer.Package("")
		imports := make([]analyzer.Import, 0, 32)

		for match := matches.Next(); match != nil; match = matches.Next() {
			pkgPath, imports = processCaptures(
				match,
				captureNames,
				pkgPath,
				pkgPathPrefix,
				text,
				imports,
			)
		}
		slog.DebugContext(
			ctx,
			"processed file",
			"path",
			goFilepath,
			"pkgDetected",
			pkgPath,
			"imports",
			imports,
		)

		pi[pkgPath] = imports

	}

	return pi, nil
}

func getPkgPathPrefix(goFilepath string, gomodPaths map[directory]modulePath) modulePath {
	gf := filepath.Dir(goFilepath)
	for fileDir := gf; fileDir != "."; fileDir = filepath.Dir(fileDir) {

		modPath, ok := gomodPaths[directory(fileDir)]
		if ok {
			rel, _ := filepath.Rel(fileDir, gf)
			if rel == "." {
				return modPath
			}

			return modulePath(filepath.Join(string(modPath), rel))
		}
	}
	// check one last time in case pwd is root with go.mod
	modPath, ok := gomodPaths[directory(".")]
	if ok {
		return modulePath(filepath.Join(string(modPath), gf))
	}
	return modulePath(gf)
}

func processCaptures(
	match *treesitter.QueryMatch,
	captureNames []string,
	pkgPath analyzer.Package,
	pkgPathPrefix modulePath,
	text []byte,
	imports []analyzer.Import,
) (analyzer.Package, []analyzer.Import) {
	for _, capture := range match.Captures {
		node := capture.Node
		captureName := captureNames[capture.Index]

		switch captureName {
		case "package":
			detectedPkgName := string(node.Utf8Text(text))
			defaultPkgPath := string(pkgPathPrefix)

			dir := filepath.Dir(defaultPkgPath)
			if detectedPkgName == filepath.Base(defaultPkgPath) {
				pkgPath = analyzer.Package(filepath.Join(dir, detectedPkgName))
			} else {
				pkgPath = analyzer.Package(filepath.Join(defaultPkgPath, detectedPkgName))
			}

			slog.Debug(
				"pkg names",
				"detectedPkgName",
				detectedPkgName,
				"defaultPkgPath",
				defaultPkgPath,
				"pkgPath",
				pkgPath,
			)
		case "import":
			imports = append(imports, analyzer.Import(node.Utf8Text(text)))
		}
	}
	return pkgPath, imports
}
