package tscore

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/logschema"
)

// ImportNameInfo maps a local imported name to its resolved package and coupling key.
type ImportNameInfo struct {
	Pkg string
	Key string
}

// HydrateImports records resolved imports as outward dependencies and tracks
// namespace (`import * as x`) aliases on the package node.
func HydrateImports(
	ctx context.Context,
	imports []CapturedImport,
	resolvedPaths map[string]string,
	pkgNode *analyzer.PackageAnalysis,
) {
	for _, imp := range imports {
		resolvedPath, ok := resolvedPaths[imp.Source]
		if !ok {
			continue
		}

		pkgNode.Out.Add(resolvedPath)

		if imp.Name == "*" {
			pkgNode.Aliases[imp.Alias] = resolvedPath
			slog.LogAttrs(ctx, slog.LevelDebug, "HydrateImports: namespace",
				logschema.UdaParseAlias(imp.Alias),
				logschema.UdaParseModule(resolvedPath),
			)
		}
	}
}

// HydrateMemberExpressions records member accesses (e.g. `ns.member`) on
// namespace-imported aliases as coupling references on the package node.
func HydrateMemberExpressions(
	ctx context.Context,
	memberExprs []MemberExpr,
	pkgNode *analyzer.PackageAnalysis,
) {
	for _, memberExpr := range memberExprs {
		resolvedPkg, isAlias := pkgNode.Aliases[memberExpr.Object]
		if !isAlias {
			continue
		}

		importInfo, exists := pkgNode.Out.Get(resolvedPkg)
		if !exists {
			continue
		}

		baseName := filepath.Base(resolvedPkg)
		key := baseName + "." + memberExpr.Property
		importInfo.Add(key, key, memberExpr.Pos)
		slog.DebugContext(ctx, "HydrateMemberExpressions", "key", key, "pkg", resolvedPkg)
	}
}

// BuildImportNameMap maps each imported local name to its resolved package and
// coupling key, skipping namespace and side-effect-only imports.
func BuildImportNameMap(
	imports []CapturedImport,
	resolvedPaths map[string]string,
) map[string]ImportNameInfo {
	nameMap := make(map[string]ImportNameInfo)

	for _, imp := range imports {
		resolvedPath, ok := resolvedPaths[imp.Source]
		if !ok {
			continue
		}

		baseName := filepath.Base(resolvedPath)

		switch imp.Name {
		case "*":
			continue
		case "default":
			key := baseName + ".default"
			nameMap[imp.Alias] = ImportNameInfo{Pkg: resolvedPath, Key: key}
		case "":
			continue
		default:
			key := baseName + "." + imp.Name

			localName := imp.Name
			if imp.Alias != "" {
				localName = imp.Alias
			}

			nameMap[localName] = ImportNameInfo{Pkg: resolvedPath, Key: key}
		}
	}

	return nameMap
}

// HydrateIdentifierUsages records usages of imported identifiers as coupling
// references on the package node.
func HydrateIdentifierUsages(
	ctx context.Context,
	usages []IdentifierUsage,
	importedNames map[string]ImportNameInfo,
	pkgNode *analyzer.PackageAnalysis,
) {
	for _, usage := range usages {
		info, exists := importedNames[usage.Name]
		if !exists {
			continue
		}

		importInfo, ok := pkgNode.Out.Get(info.Pkg)
		if !ok {
			continue
		}

		importInfo.Add(info.Key, info.Key, usage.Pos)
		slog.DebugContext(ctx, "HydrateIdentifierUsages", "key", info.Key, "pkg", info.Pkg)
	}
}
