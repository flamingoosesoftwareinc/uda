// Package rust implements the Rust source analyzer (crate + use + module tracking).
package rust

import (
	"context"
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
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
)

// BoundaryStrategy selects the granularity at which Rust packages are grouped.
type BoundaryStrategy string

// Boundary strategies select the granularity at which packages are grouped.
const (
	StrategyPackage BoundaryStrategy = "package"
	StrategyModule  BoundaryStrategy = "module"
)

// Rust path keywords recognised by the use-path resolver. Hoisted so the
// literal "self" appears once at the keyword set rather than at every site.
const (
	pathSelf = "self"
	// cratePlusItem is "crate :: item" — the minimum use-path arity that
	// produces an external dependency under StrategyModule (anything below
	// is treated as same-crate intra-module use and skipped).
	cratePlusItem = 2
)

// Option configures a rustAnalyzer.
type Option func(*rustAnalyzer)

// WithBoundaryStrategy sets the package-boundary strategy for the analyzer.
func WithBoundaryStrategy(s BoundaryStrategy) Option {
	return func(a *rustAnalyzer) {
		switch s {
		case StrategyPackage, StrategyModule:
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

type rustAnalyzer struct {
	strategy   BoundaryStrategy
	boundaries []analyzer.PackageBoundary
}

var (
	_ analyzer.Analyzer         = &rustAnalyzer{}
	_ analyzer.BoundaryProvider = &rustAnalyzer{}
)

// sysrootCrates lists Rust sysroot crates that are part of the language
// substrate and must not be counted as efferent dependencies.
var sysrootCrates = map[string]struct{}{
	"std":        {},
	"core":       {},
	"alloc":      {},
	"proc_macro": {},
	"test":       {},
}

// RustAnalyzer returns a Rust source analyzer (satisfies analyzer.Analyzer
// and analyzer.BoundaryProvider). Concrete pointer return so tests can call
// Boundaries without a type assertion; cmd uses the interface upcast.
//
//nolint:revive // unexported-return: the concrete type is intentionally package-private; consumers upcast to analyzer.Analyzer / BoundaryProvider.
func RustAnalyzer(opts ...Option) *rustAnalyzer {
	a := &rustAnalyzer{strategy: StrategyPackage}
	for _, opt := range opts {
		opt(a)
	}

	return a
}

func (r *rustAnalyzer) Name() string { return "Rust" }

func (r *rustAnalyzer) Analyze(ctx context.Context, dir fs.FS) ([]analyzer.Metrics, error) {
	cargoFiles, err := listCargoTomlFiles(ctx, dir)
	if err != nil {
		return nil, err
	}

	slog.LogAttrs(ctx, slog.LevelDebug, "found Cargo.toml files",
		logschema.UdaAnalyzerFilepaths(cargoFiles),
	)

	crateMap, err := extractCrateInfo(ctx, dir, cargoFiles)
	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, "identified crate info", "crates", crateMap)

	metrics, boundaries, err := analyzeRustMetrics(ctx, dir, crateMap, r.strategy)
	if err != nil {
		return nil, err
	}

	r.boundaries = boundaries

	return metrics, nil
}

// Boundaries returns the filesystem directories for each analyzed package.
// Lazily invokes Analyze() if boundaries have not been populated yet.
func (r *rustAnalyzer) Boundaries(
	ctx context.Context,
	dir fs.FS,
) ([]analyzer.PackageBoundary, error) {
	if r.boundaries == nil {
		if _, err := r.Analyze(ctx, dir); err != nil {
			return nil, err
		}
	}

	return r.boundaries, nil
}

// Phase 1: Module boundary detection via Cargo.toml

type (
	directory string
	crateName string
)

type crateInfo struct {
	name         crateName
	libName      string // explicit [lib] name, empty when not declared
	dir          directory
	dependencies map[string]struct{} // external dependency names
}

// importName returns the name under which the crate is referenced from Rust
// source: the explicit [lib] name when declared, else the Cargo package name
// with hyphens normalized to underscores (Cargo's default lib target name).
func (c *crateInfo) importName() string {
	if c.libName != "" {
		return c.libName
	}

	return strings.ReplaceAll(string(c.name), "-", "_")
}

func listCargoTomlFiles(ctx context.Context, dir fs.FS) ([]string, error) {
	return files.ListFiles(
		ctx,
		dir,
		files.SkipHiddenDirs(),
		files.SkipHiddenFiles(),
		skipTargetDir(),
		cargoTomlFileFilter(),
	)
}

func cargoTomlFileFilter() files.FileFilter {
	return func(path string, d fs.DirEntry) bool {
		if d.IsDir() {
			return false
		}

		return filepath.Base(path) != "Cargo.toml"
	}
}

func skipTargetDir() files.FileFilter {
	return func(_ string, d fs.DirEntry) bool {
		if !d.IsDir() {
			return false
		}

		return d.Name() == "target"
	}
}

type cargoTomlPartial struct {
	Package struct {
		Name string `toml:"name"`
	} `toml:"package"`
	Lib struct {
		Name string `toml:"name"`
	} `toml:"lib"`
	Workspace struct {
		Members []string `toml:"members"`
	} `toml:"workspace"`
	Dependencies map[string]any `toml:"dependencies"`
}

func extractCrateInfo(
	ctx context.Context,
	dir fs.FS,
	cargoFilePaths []string,
) (map[directory]*crateInfo, error) {
	crateMap := make(map[directory]*crateInfo, len(cargoFilePaths))

	for _, cfPath := range cargoFilePaths {
		data, err := fs.ReadFile(dir, cfPath)
		if err != nil {
			return nil, slogerr.New(err,
				logschema.UdaAnalyzerFile(cfPath),
				logschema.UdaAnalyzerLanguage("Rust"),
				logschema.UdaErrorPhase("read"),
			)
		}

		var cargo cargoTomlPartial
		if err := toml.Unmarshal(data, &cargo); err != nil {
			slog.LogAttrs(
				ctx,
				slog.LevelDebug,
				"extractCrateInfo: failed to parse Cargo.toml",
				logschema.UdaErrorPath(cfPath),
				logschema.UdaErrorMessage(err.Error()),
			)

			continue
		}

		cfDir := directory(filepath.Dir(cfPath))

		// Skip workspace-only Cargo.toml files (no [package] section)
		if cargo.Package.Name == "" {
			slog.LogAttrs(
				ctx,
				slog.LevelDebug,
				"extractCrateInfo: workspace Cargo.toml (no package name)",
				logschema.UdaErrorPath(cfPath),
			)

			continue
		}

		deps := make(map[string]struct{}, len(cargo.Dependencies))
		for depName := range cargo.Dependencies {
			deps[depName] = struct{}{}
		}

		crateMap[cfDir] = &crateInfo{
			name:         crateName(cargo.Package.Name),
			libName:      cargo.Lib.Name,
			dir:          cfDir,
			dependencies: deps,
		}
	}

	return crateMap, nil
}

// Phase 2: File discovery

func listRustFiles(ctx context.Context, dir fs.FS) ([]string, error) {
	return files.ListFiles(
		ctx,
		dir,
		files.SkipHiddenDirs(),
		files.SkipHiddenFiles(),
		skipTargetDir(),
		rustFileFilter(),
	)
}

func rustFileFilter() files.FileFilter {
	return func(path string, d fs.DirEntry) bool {
		if d.IsDir() {
			return false
		}

		return filepath.Ext(path) != ".rs"
	}
}

// getCrateForFile finds which crate a .rs file belongs to.
func getCrateForFile(filePath string, crateMap map[directory]*crateInfo) *crateInfo {
	for dir := filepath.Dir(filePath); ; dir = filepath.Dir(dir) {
		if ci, ok := crateMap[directory(dir)]; ok {
			return ci
		}

		if dir == "." || dir == filepath.Dir(dir) {
			break
		}
	}

	return nil
}

// getModulePath returns the boundary path for a file within a crate.
// With StrategyModule, returns module-level paths (e.g. "app::routes").
// With StrategyPackage, returns the crate name (e.g. "app").
func getModulePath(filePath string, crate *crateInfo, strategy BoundaryStrategy) string {
	if strategy == StrategyPackage {
		return string(crate.name)
	}

	crateDir := string(crate.dir)
	srcDir := filepath.Join(crateDir, "src")

	rel, err := filepath.Rel(srcDir, filePath)
	// If the relative path starts with "..", the file is outside src/
	if err != nil || strings.HasPrefix(rel, "..") {
		// Fallback: use path relative to crate root
		rel, _ = filepath.Rel(crateDir, filePath)
	}

	// Remove .rs extension
	rel = strings.TrimSuffix(rel, ".rs")

	// mod.rs represents the parent directory module
	if filepath.Base(rel) == "mod" {
		rel = filepath.Dir(rel)
	}

	// main.rs and lib.rs are the crate root
	if rel == "main" || rel == "lib" {
		return string(crate.name)
	}

	// Convert path separators to ::
	parts := strings.Split(filepath.ToSlash(rel), "/")
	modulePath := string(crate.name) + "::" + strings.Join(parts, "::")

	return modulePath
}

// Phase 3: Tree-sitter analysis

type capturedUse struct {
	path  string // full use path e.g. "crate::config::Config", "std::fmt", "serde::Deserialize"
	alias string // optional alias from "use x as y"
	pos   analyzer.Position
}

type capturedUsage struct {
	expr string // e.g., "Router::new" or "Router" or "fmt::format"
	pos  analyzer.Position
}

type captures struct {
	modulePath string
	uses       []capturedUse
	usages     []capturedUsage // actual usage expressions
}

var rustQuery = `
(use_declaration
  argument: (scoped_identifier) @use_path)
(use_declaration
  argument: (use_as_clause
    path: (scoped_identifier) @use_as_path
    alias: (identifier) @use_alias))
(use_declaration
  argument: (scoped_use_list
    path: (scoped_identifier) @use_tree_path
    list: (use_list) @use_tree_list))
(use_declaration
  argument: (scoped_use_list
    path: (identifier) @use_tree_simple_path
    list: (use_list) @use_tree_simple_list))
(use_declaration
  argument: (identifier) @use_simple)
(call_expression
  function: (scoped_identifier) @scoped_call)
(struct_expression
  name: (type_identifier) @struct_init)
(call_expression
  function: (identifier) @direct_call)
(type_identifier) @type_usage
(attribute_item
  (attribute
    (identifier) @attr_name
    arguments: (token_tree) @attr_args))
(macro_invocation
  macro: (identifier) @macro_call)
`

var (
	compiledRustQuery *treesitter.Query
	rustQueryOnce     sync.Once
	errRustQuery      error
)

func getCompiledRustQuery() (*treesitter.Query, error) {
	rustQueryOnce.Do(func() {
		rustLang := treesitter.NewLanguage(tree_sitter_rust.Language())

		var qErr *treesitter.QueryError

		compiledRustQuery, qErr = treesitter.NewQuery(rustLang, rustQuery)
		if qErr != nil {
			errRustQuery = qErr
		}
	})

	return compiledRustQuery, errRustQuery
}

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

		appendRustUses(&caps, captureMap, filePath)
		appendRustUsages(&caps, captureMap, filePath)
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

// appendRustUses records `use` declarations (scoped, aliased, and use-tree forms).
func appendRustUses(caps *captures, captureMap map[string]captureInfo, filePath string) {
	// Simple scoped use: use std::fmt;  or  use crate::config::Config;
	if pathInfo, ok := captureMap["use_path"]; ok {
		caps.uses = append(caps.uses, capturedUse{
			path: pathInfo.text,
			pos:  capturePosition(filePath, pathInfo),
		})
	}

	// Use with alias: use tokio::runtime::Runtime as TokioRuntime;
	if pathInfo, ok := captureMap["use_as_path"]; ok {
		alias := ""
		if aliasInfo, ok := captureMap["use_alias"]; ok {
			alias = aliasInfo.text
		}

		caps.uses = append(caps.uses, capturedUse{
			path:  pathInfo.text,
			alias: alias,
			pos:   capturePosition(filePath, pathInfo),
		})
	}

	// Use tree with scoped path: use std::collections::{HashMap, HashSet};
	if pathInfo, ok := captureMap["use_tree_path"]; ok {
		if listInfo, ok := captureMap["use_tree_list"]; ok {
			items := parseUseList(listInfo.text)
			for _, item := range items {
				fullPath := pathInfo.text + "::" + item
				caps.uses = append(caps.uses, capturedUse{
					path: fullPath,
					pos:  capturePosition(filePath, pathInfo),
				})
			}
		}
	}

	// Use tree with simple path: use serde::{Serialize, Deserialize};
	if pathInfo, ok := captureMap["use_tree_simple_path"]; ok {
		if listInfo, ok := captureMap["use_tree_simple_list"]; ok {
			items := parseUseList(listInfo.text)
			for _, item := range items {
				fullPath := pathInfo.text + "::" + item
				caps.uses = append(caps.uses, capturedUse{
					path: fullPath,
					pos:  capturePosition(filePath, pathInfo),
				})
			}
		}
	}

	// Simple identifier use (rare): use something;
	if pathInfo, ok := captureMap["use_simple"]; ok {
		caps.uses = append(caps.uses, capturedUse{
			path: pathInfo.text,
			pos:  capturePosition(filePath, pathInfo),
		})
	}
}

// appendRustUsages records symbol usages (calls, struct inits, types, derives, macros).
func appendRustUsages(caps *captures, captureMap map[string]captureInfo, filePath string) {
	// Scoped call expression: Router::new(), fmt::format()
	if exprInfo, ok := captureMap["scoped_call"]; ok {
		caps.usages = append(caps.usages, capturedUsage{
			expr: exprInfo.text,
			pos:  capturePosition(filePath, exprInfo),
		})
	}

	// Struct initialization: Config { ... }
	if exprInfo, ok := captureMap["struct_init"]; ok {
		caps.usages = append(caps.usages, capturedUsage{
			expr: exprInfo.text,
			pos:  capturePosition(filePath, exprInfo),
		})
	}

	// Direct call expression: format_date(), handle()
	if exprInfo, ok := captureMap["direct_call"]; ok {
		caps.usages = append(caps.usages, capturedUsage{
			expr: exprInfo.text,
			pos:  capturePosition(filePath, exprInfo),
		})
	}

	// Type usage: &Request, Vec<Config>, etc.
	if typeInfo, ok := captureMap["type_usage"]; ok {
		caps.usages = append(caps.usages, capturedUsage{
			expr: typeInfo.text,
			pos:  capturePosition(filePath, typeInfo),
		})
	}

	// Derive attribute: #[derive(Serialize, Deserialize)]
	if attrName, ok := captureMap["attr_name"]; ok {
		if attrName.text == "derive" {
			if argsInfo, ok := captureMap["attr_args"]; ok {
				// Parse derive items from token tree like "(Serialize, Deserialize)"
				items := parseDeriveArgs(argsInfo.text)
				for _, item := range items {
					caps.usages = append(caps.usages, capturedUsage{
						expr: item,
						pos:  capturePosition(filePath, argsInfo),
					})
				}
			}
		}
	}

	// Macro invocation: info!(), debug!(), println!()
	if macroInfo, ok := captureMap["macro_call"]; ok {
		caps.usages = append(caps.usages, capturedUsage{
			expr: macroInfo.text,
			pos:  capturePosition(filePath, macroInfo),
		})
	}
}

type captureInfo struct {
	text     string
	startRow uint
	startCol uint
	endRow   uint
	endCol   uint
}

// parseUseList extracts items from a use list like "{HashMap, HashSet}" or "{Serialize, Deserialize}"
// Handles nested use lists like "{A, b::{C, D}}" correctly by recursively expanding them.
func parseUseList(listText string) []string {
	// Remove outer braces
	listText = strings.TrimPrefix(listText, "{")
	listText = strings.TrimSuffix(listText, "}")
	listText = strings.TrimSpace(listText)

	if listText == "" {
		return nil
	}

	// Split on commas that are not inside nested braces
	var (
		rawItems []string
		current  strings.Builder
	)

	braceDepth := 0

	for _, ch := range listText {
		switch ch {
		case '{':
			braceDepth++

			current.WriteRune(ch)
		case '}':
			braceDepth--

			current.WriteRune(ch)
		case ',':
			if braceDepth == 0 {
				// Top-level comma - split here
				item := strings.TrimSpace(current.String())
				if item != "" && item != pathSelf {
					rawItems = append(rawItems, item)
				}

				current.Reset()
			} else {
				// Comma inside nested braces - keep it
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}

	// Don't forget the last item
	item := strings.TrimSpace(current.String())
	if item != "" && item != pathSelf {
		rawItems = append(rawItems, item)
	}

	// Now expand any nested use items like "hash_map::{Entry, VacantEntry}"
	var items []string
	for _, rawItem := range rawItems {
		items = append(items, expandUseItem(rawItem)...)
	}

	return items
}

// expandUseItem expands a use item that may contain nested braces.
// e.g. "hash_map::{Entry, VacantEntry}" -> ["hash_map::Entry", "hash_map::VacantEntry"]
// e.g. "HashMap" -> ["HashMap"].
func expandUseItem(item string) []string {
	// Check if this item contains a nested use list
	braceIdx := strings.Index(item, "::{")
	if braceIdx == -1 {
		// No nested braces, return as-is
		return []string{item}
	}

	// Split into prefix and nested list
	prefix := item[:braceIdx]
	nestedList := item[braceIdx+2:] // Skip "::"

	// Recursively parse the nested list
	nestedItems := parseUseList(nestedList)

	// Build full paths
	var expanded []string
	for _, nested := range nestedItems {
		expanded = append(expanded, prefix+"::"+nested)
	}

	return expanded
}

// parseDeriveArgs extracts items from derive attribute args like "(Serialize, Deserialize)".
func parseDeriveArgs(argsText string) []string {
	// Remove parentheses
	argsText = strings.TrimPrefix(argsText, "(")
	argsText = strings.TrimSuffix(argsText, ")")
	argsText = strings.TrimSpace(argsText)

	if argsText == "" {
		return nil
	}

	parts := strings.Split(argsText, ",")

	items := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			items = append(items, p)
		}
	}

	return items
}

// resolveUsePath classifies and resolves a use path into a dependency package key.
// Returns the resolved package key (module path without the item) and whether it's internal.
//
// Categories:
// - crate::foo::Bar         → internal dependency on module "crateName::foo"
// - crate::handler::router::Router → internal dependency on module "crateName::handler::router"
// - super::foo              → skipped (intra-module)
// - self::foo               → skipped (same module)
// - std::fmt                → external "std" (single segment after crate = crate itself)
// - std::collections::HashMap → external "std::collections"
// - serde::Deserialize      → external "serde".
//

func resolveUsePath(
	usePath string,
	currentCrate *crateInfo,
	crateMap map[directory]*crateInfo,
	strategy BoundaryStrategy,
) (string, bool, bool) {
	allParts := strings.Split(usePath, "::")
	if len(allParts) == 0 {
		return "", false, true
	}

	root := allParts[0]

	switch root {
	case "crate":
		if strategy == StrategyPackage || len(allParts) <= cratePlusItem {
			return "", false, true
		}

		moduleParts := allParts[1 : len(allParts)-1]
		depPackage := string(currentCrate.name) + "::" + strings.Join(moduleParts, "::")

		return depPackage, true, false

	case "super", pathSelf:
		return "", false, true

	default:
		if crate := lookupWorkspaceCrate(root, crateMap); crate != nil {
			return resolveWorkspaceUsePath(crate, allParts, strategy)
		}

		// Sysroot crates are language substrate — skip unless a workspace crate
		// shadows the reserved name (handled by the lookup above).
		if _, isSysroot := sysrootCrates[root]; isSysroot {
			return "", false, true
		}

		return resolveExternalUsePath(root, allParts, strategy)
	}
}

// lookupWorkspaceCrate resolves a use-path root to the workspace crate it
// imports, matching by import name. When two crates normalize to the same
// import name the pick is deterministic: a crate whose package name equals
// the root exactly wins, else the lexicographically smallest package name.
func lookupWorkspaceCrate(root string, crateMap map[directory]*crateInfo) *crateInfo {
	var best *crateInfo

	for _, crate := range crateMap {
		if crate.importName() != root {
			continue
		}

		if string(crate.name) == root {
			return crate
		}

		if best == nil || crate.name < best.name {
			best = crate
		}
	}

	return best
}

// resolveWorkspaceUsePath resolves a use path rooted at a workspace crate,
// re-rooting the dependency key at the crate's Cargo package name so edges
// match the boundary keys built from Cargo.toml (source imports use the
// normalized crate name, boundaries use the package spelling).
func resolveWorkspaceUsePath(
	crate *crateInfo,
	allParts []string,
	strategy BoundaryStrategy,
) (string, bool, bool) {
	if strategy == StrategyPackage || len(allParts) <= cratePlusItem {
		return string(crate.name), false, false
	}

	moduleParts := append([]string{string(crate.name)}, allParts[1:len(allParts)-1]...)

	return strings.Join(moduleParts, "::"), false, false
}

// resolveExternalUsePath resolves a use path rooted at an external crate named
// root, mapping it to the dependency key at the configured boundary granularity.
func resolveExternalUsePath(
	root string,
	allParts []string,
	strategy BoundaryStrategy,
) (string, bool, bool) {
	if strategy == StrategyPackage || len(allParts) <= cratePlusItem {
		return root, false, false
	}

	moduleParts := allParts[:len(allParts)-1]

	return strings.Join(moduleParts, "::"), false, false
}

// getUseItemName returns the "leaf" name used in qualified expressions.
// e.g. "crate::config::Config" → "Config"
// e.g. "std::fmt" → "fmt".
func getUseItemName(usePath string) string {
	parts := strings.Split(usePath, "::")

	return parts[len(parts)-1]
}

// getResolvedItemPath returns the full item path with crate:: resolved to the actual crate name.
// e.g. "crate::config::Config" with crate "myapp" → "myapp::config::Config"
// e.g. "std::collections::HashMap" → "std::collections::HashMap".
func getResolvedItemPath(usePath string, currentCrate *crateInfo) string {
	if strings.HasPrefix(usePath, "crate::") {
		return string(currentCrate.name) + usePath[5:] // replace "crate" with crate name
	}

	return usePath
}

// importNameInfo holds metadata about an imported name for usage site tracking.
type importNameInfo struct {
	pkg      string // resolved package key
	fullPath string // full resolved item path
}

// buildImportNameMap creates a map from local imported names to their resolved package and full path.
// e.g., "Config" → {pkg: "myapp::config", fullPath: "myapp::config::Config"}
// e.g., "TokioRuntime" (alias for Runtime) → {pkg: "tokio::runtime", fullPath: "tokio::runtime::Runtime"}.
func buildImportNameMap(
	uses []capturedUse,
	currentCrate *crateInfo,
	crateMap map[directory]*crateInfo,
	strategy BoundaryStrategy,
) map[string]importNameInfo {
	nameMap := make(map[string]importNameInfo)

	for _, use := range uses {
		depPkg, _, skip := resolveUsePath(use.path, currentCrate, crateMap, strategy)
		if skip || depPkg == "" {
			continue
		}

		itemName := getUseItemName(use.path)
		fullPath := getResolvedItemPath(use.path, currentCrate)

		// Use alias as the local name if present
		localName := itemName
		if use.alias != "" {
			localName = use.alias
		}

		nameMap[localName] = importNameInfo{
			pkg:      depPkg,
			fullPath: fullPath,
		}
	}

	return nameMap
}

// extractIdentifierFromExpr extracts the first identifier from an expression.
// e.g., "Router::new" → "Router"
// e.g., "fmt::format" → "fmt"
// e.g., "Config" → "Config".
func extractIdentifierFromExpr(expr string) string {
	if before, _, ok := strings.Cut(expr, "::"); ok {
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
			continue // Not an imported symbol
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
func analyzeRustMetrics(
	ctx context.Context,
	dir fs.FS,
	crateMap map[directory]*crateInfo,
	strategy BoundaryStrategy,
) ([]analyzer.Metrics, []analyzer.PackageBoundary, error) {
	depGraph := analyzer.NewPackageAnalysisTree()

	rsFilepaths, err := listRustFiles(ctx, dir)
	if err != nil {
		return nil, nil, err
	}

	tsparser := treesitter.NewParser()
	defer tsparser.Close()

	rustLanguage := treesitter.NewLanguage(tree_sitter_rust.Language())
	if err := tsparser.SetLanguage(rustLanguage); err != nil {
		return nil, nil, err
	}

	// Compile query once for all files
	compiledQuery, err := getCompiledRustQuery()
	if err != nil {
		return nil, nil, err
	}

	// Intern file paths to reduce memory usage in Position structs
	pathInterner := analyzer.NewStringInterner()

	// Track package → directories for boundary computation
	pkgDirs := make(map[string]map[string]bool)

	for _, rsFilepath := range rsFilepaths {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		// Intern the file path so all positions share the same string
		rsFilepath = pathInterner.Intern(rsFilepath)

		crate := getCrateForFile(rsFilepath, crateMap)
		if crate == nil {
			slog.LogAttrs(ctx, slog.LevelDebug, "no crate found for file, skipping",
				logschema.UdaAnalyzerFile(rsFilepath),
			)

			continue
		}

		modulePath := getModulePath(rsFilepath, crate, strategy)

		// Collect directory for boundary mapping
		fileDir := filepath.Dir(rsFilepath)

		if pkgDirs[modulePath] == nil {
			pkgDirs[modulePath] = make(map[string]bool)
		}

		pkgDirs[modulePath][fileDir] = true

		tree, text, err := ts.Parse(ctx, tsparser, dir, rsFilepath)
		if err != nil {
			return nil, nil, slogerr.New(err,
				logschema.UdaAnalyzerFile(rsFilepath),
				logschema.UdaAnalyzerLanguage("Rust"),
				logschema.UdaErrorPhase("parse"),
			)
		}

		queryCursor := treesitter.NewQueryCursor()

		caps := getCapturesFromMatches(
			modulePath,
			rsFilepath,
			compiledQuery,
			queryCursor,
			tree,
			text,
		)

		// Free tree-sitter objects now that we've extracted all captures
		tree.Close()
		queryCursor.Close()

		// Get or init the package node
		var pkgNode *analyzer.PackageAnalysis
		if pn, exists := depGraph.Get(modulePath); exists {
			pkgNode = pn
		} else {
			pkgNode = depGraph.Add(modulePath)
		}

		// Register imports (without recording positions yet)
		for _, use := range caps.uses {
			depPkg, _, skip := resolveUsePath(use.path, crate, crateMap, strategy)
			if skip || depPkg == "" {
				continue
			}

			pkgNode.Out.Add(depPkg)
		}

		// Build import name lookup map and hydrate usage expressions
		importedNames := buildImportNameMap(caps.uses, crate, crateMap, strategy)
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
