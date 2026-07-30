package evocoupling

import (
	"path/filepath"
	"strings"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
)

// PackageResolver maps file paths to package names via a prefix trie.
type PackageResolver struct {
	root *trieNode
}

type trieNode struct {
	children map[string]*trieNode
	packages []string
}

// NewPackageResolver builds a resolver from analyzer boundaries.
func NewPackageResolver(boundaries []analyzer.PackageBoundary) *PackageResolver {
	root := &trieNode{children: make(map[string]*trieNode)}

	for _, b := range boundaries {
		for _, dir := range b.Dirs {
			segments := splitPath(dir)
			node := root

			for _, seg := range segments {
				child, ok := node.children[seg]
				if !ok {
					child = &trieNode{children: make(map[string]*trieNode)}
					node.children[seg] = child
				}

				node = child
			}

			node.packages = append(node.packages, b.Name)
		}
	}

	return &PackageResolver{root: root}
}

// Resolve returns the package names for a file path.
// Returns the deepest matching prefix. Returns nil if no match.
func (r *PackageResolver) Resolve(filePath string) []string {
	segments := splitPath(filepath.Dir(filePath))
	node := r.root

	var match []string

	for _, seg := range segments {
		child, ok := node.children[seg]
		if !ok {
			break
		}

		node = child

		if len(node.packages) > 0 {
			match = node.packages
		}
	}

	return match
}

// ResolveCommit returns the set of packages touched by a commit's changed files.
func (r *PackageResolver) ResolveCommit(files []string) map[string]struct{} {
	pkgs := make(map[string]struct{})

	for _, f := range files {
		for _, pkg := range r.Resolve(f) {
			pkgs[pkg] = struct{}{}
		}
	}

	return pkgs
}

func splitPath(p string) []string {
	p = filepath.Clean(p)
	if p == "." {
		return nil
	}

	return strings.Split(p, string(filepath.Separator))
}
