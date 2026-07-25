// Package manifest parses Swift Package Manager Package.swift descriptors.
package manifest

import (
	"fmt"
	"io/fs"
	"slices"
	"strings"

	tree_sitter_swift "github.com/flamingoosesoftwareinc/tree-sitter-swift/bindings/go"
	treesitter "github.com/tree-sitter/go-tree-sitter"
)

// Tree-sitter node kinds we navigate to extract the Package(...) call. Hoisted
// so the literal appears once rather than at every Kind() comparison.
const nodeKindCallExpression = "call_expression"

// Manifest represents a parsed Swift Package Manager Package.swift file.
type Manifest struct {
	Name    string   `json:"name"`
	Targets []Target `json:"targets"`
}

// Target represents a single target declared in Package.swift.
type Target struct {
	Name         string   `json:"name"`
	Path         string   `json:"path"`
	IsTest       bool     `json:"is_test"`
	Dependencies []string `json:"dependencies"`
}

// Parse reads Package.swift from the given filesystem and extracts the manifest.
func Parse(dir fs.FS) (*Manifest, error) {
	data, err := fs.ReadFile(dir, "Package.swift")
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}

	text := data

	parser := treesitter.NewParser()
	defer parser.Close()

	lang := treesitter.NewLanguage(tree_sitter_swift.Language())
	if err := parser.SetLanguage(lang); err != nil {
		return nil, fmt.Errorf("manifest: set language: %w", err)
	}

	tree := parser.Parse(text, nil)
	defer tree.Close()

	root := tree.RootNode()
	parsed := &Manifest{}

	// Find the Package(...) call expression
	pkgCall := findPackageCall(root, text)
	if pkgCall == nil {
		return parsed, nil
	}

	// Extract arguments from Package(...)
	args := findValueArguments(pkgCall)
	if args == nil {
		return parsed, nil
	}

	// Extract package name
	if nameArg := findArgByLabel(args, "name", text); nameArg != nil {
		parsed.Name = extractStringLiteral(nameArg, text)
	}

	// Extract targets
	if targetsArg := findArgByLabel(args, "targets", text); targetsArg != nil {
		arr := findChildByKind(targetsArg, "array_literal")
		if arr != nil {
			parsed.Targets = extractTargets(arr, text)
		}
	}

	return parsed, nil
}

// TargetNames returns the set of target names in the manifest.
func (m *Manifest) TargetNames() map[string]struct{} {
	names := make(map[string]struct{}, len(m.Targets))
	for _, t := range m.Targets {
		names[t.Name] = struct{}{}
	}

	return names
}

// TargetForDir returns the target whose path matches the given directory prefix,
// or nil if none matches.
func (m *Manifest) TargetForDir(dir string) *Target {
	dir = strings.TrimSuffix(dir, "/")
	for i := range m.Targets {
		tp := strings.TrimSuffix(m.Targets[i].Path, "/")
		if dir == tp || strings.HasPrefix(dir, tp+"/") {
			return &m.Targets[i]
		}
	}

	return nil
}

// findPackageCall walks the tree to find a call_expression whose callee is "Package".
func findPackageCall(node *treesitter.Node, text []byte) *treesitter.Node {
	if node.Kind() == nodeKindCallExpression {
		// The callee is the first child (simple_identifier "Package")
		for i := range node.ChildCount() {
			child := node.Child(i)
			if child != nil && child.Kind() == "simple_identifier" &&
				child.Utf8Text(text) == "Package" {
				return node
			}
		}
	}

	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil {
			continue
		}

		if found := findPackageCall(child, text); found != nil {
			return found
		}
	}

	return nil
}

// findValueArguments finds the value_arguments node within a call_expression.
func findValueArguments(callExpr *treesitter.Node) *treesitter.Node {
	// call_expression > call_suffix > value_arguments
	for i := range callExpr.ChildCount() {
		child := callExpr.Child(i)
		if child == nil {
			continue
		}

		if child.Kind() == "call_suffix" {
			return findChildByKind(child, "value_arguments")
		}
	}

	return nil
}

// findArgByLabel finds a value_argument whose label matches the given name.
func findArgByLabel(argsNode *treesitter.Node, label string, text []byte) *treesitter.Node {
	for i := range argsNode.ChildCount() {
		child := argsNode.Child(i)
		if child == nil || child.Kind() != "value_argument" {
			continue
		}

		lbl := findChildByKind(child, "value_argument_label")
		if lbl == nil {
			continue
		}
		// The label contains a simple_identifier with the label text
		id := findChildByKind(lbl, "simple_identifier")
		if id != nil && id.Utf8Text(text) == label {
			return child
		}
	}

	return nil
}

// findChildByKind returns the first child with the given node kind.
func findChildByKind(node *treesitter.Node, kind string) *treesitter.Node {
	for i := range node.ChildCount() {
		child := node.Child(i)
		if child != nil && child.Kind() == kind {
			return child
		}
	}

	return nil
}

// extractStringLiteral extracts the text content from a value_argument containing
// a line_string_literal. It finds the line_str_text child.
func extractStringLiteral(argNode *treesitter.Node, text []byte) string {
	lit := findDescendantByKind(argNode, "line_string_literal")
	if lit == nil {
		return ""
	}

	return extractStringFromLiteral(lit, text)
}

// extractStringFromLiteral extracts the text from a line_string_literal node.
func extractStringFromLiteral(lit *treesitter.Node, text []byte) string {
	strText := findChildByKind(lit, "line_str_text")
	if strText != nil {
		return strText.Utf8Text(text)
	}

	return ""
}

// findDescendantByKind does a depth-first search for a descendant with the given kind.
func findDescendantByKind(node *treesitter.Node, kind string) *treesitter.Node {
	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil {
			continue
		}

		if child.Kind() == kind {
			return child
		}

		if found := findDescendantByKind(child, kind); found != nil {
			return found
		}
	}

	return nil
}

// targetKinds maps SPM target method names to whether they are test targets.
var targetKinds = map[string]bool{
	"target":           false,
	"executableTarget": false,
	"testTarget":       true,
}

// extractTargets extracts Target structs from the targets array_literal node.
func extractTargets(arrNode *treesitter.Node, text []byte) []Target {
	var targets []Target

	for i := range arrNode.ChildCount() {
		child := arrNode.Child(i)
		if child == nil || child.Kind() != nodeKindCallExpression {
			continue
		}

		// Get the method name from the prefix_expression: .target, .testTarget, etc.
		prefix := findChildByKind(child, "prefix_expression")
		if prefix == nil {
			continue
		}

		methodID := findChildByKind(prefix, "simple_identifier")
		if methodID == nil {
			continue
		}

		methodName := methodID.Utf8Text(text)

		isTest, ok := targetKinds[methodName]
		if !ok {
			continue
		}

		// Get the arguments
		callArgs := findValueArguments(child)
		if callArgs == nil {
			continue
		}

		t := Target{IsTest: isTest}

		// Extract name
		if nameArg := findArgByLabel(callArgs, "name", text); nameArg != nil {
			t.Name = extractStringLiteral(nameArg, text)
		}

		if t.Name == "" {
			continue
		}

		// Extract path
		if pathArg := findArgByLabel(callArgs, "path", text); pathArg != nil {
			t.Path = extractStringLiteral(pathArg, text)
		}

		if t.Path == "" {
			if isTest {
				t.Path = "Tests/" + t.Name
			} else {
				t.Path = "Sources/" + t.Name
			}
		}

		// Extract dependencies
		if depsArg := findArgByLabel(callArgs, "dependencies", text); depsArg != nil {
			depsArr := findChildByKind(depsArg, "array_literal")
			if depsArr != nil {
				t.Dependencies = extractDependencies(depsArr, text)
			}
		}

		targets = append(targets, t)
	}

	return targets
}

// extractDependencies extracts dependency names from a dependencies array_literal.
// Handles both string literal deps ("DepName") and call expression deps
// (.target(name: "X"), .product(name: "X", package: "Y")).
//

func extractDependencies(arrNode *treesitter.Node, text []byte) []string {
	var deps []string

	for i := range arrNode.ChildCount() {
		child := arrNode.Child(i)
		if child == nil {
			continue
		}

		switch child.Kind() {
		case "line_string_literal":
			// Simple string dep: "DepName"
			if s := extractStringFromLiteral(child, text); s != "" {
				deps = append(deps, s)
			}

		case nodeKindCallExpression:
			// .target(name: "X") or .product(name: "X", package: "Y")
			if name, ok := dependencyFromCallExpr(child, text); ok && !contains(deps, name) {
				deps = append(deps, name)
			}
		}
	}

	return deps
}

// dependencyFromCallExpr extracts the dependency name from a .target/.product/.byName
// call expression, returning the name and whether a non-empty one was found.
func dependencyFromCallExpr(child *treesitter.Node, text []byte) (string, bool) {
	prefix := findChildByKind(child, "prefix_expression")
	if prefix == nil {
		return "", false
	}

	methodID := findChildByKind(prefix, "simple_identifier")
	if methodID == nil {
		return "", false
	}

	method := methodID.Utf8Text(text)
	if method != "target" && method != "product" && method != "byName" {
		return "", false
	}

	callArgs := findValueArguments(child)
	if callArgs == nil {
		return "", false
	}

	nameArg := findArgByLabel(callArgs, "name", text)
	if nameArg == nil {
		return "", false
	}

	name := extractStringLiteral(nameArg, text)

	return name, name != ""
}

func contains(ss []string, s string) bool {
	return slices.Contains(ss, s)
}
