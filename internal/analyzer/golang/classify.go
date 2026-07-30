package golang

//go:generate go run ./cmd/gen-stdlib

import "strings"

// ImportKind classifies a Go import path by where it resolves.
type ImportKind int

// Import kinds classify where a Go import path resolves.
const (
	ImportStdlib   ImportKind = iota // Go standard library
	ImportInternal                   // package within an analyzed module
	ImportExternal                   // third-party dependency
)

// ClassifyImport returns the kind of a Go import path. modulePaths are the
// module paths declared by the analyzed source's go.mod files; an import equal
// to or under one of them is internal. Internal is checked first so a module
// with a dotless path cannot be mistaken for standard library.
func ClassifyImport(importPath string, modulePaths []string) ImportKind {
	for _, mod := range modulePaths {
		if importPath == mod || strings.HasPrefix(importPath, mod+"/") {
			return ImportInternal
		}
	}

	if isStdlib(importPath) {
		return ImportStdlib
	}

	return ImportExternal
}

// isStdlib reports whether importPath is a standard-library package. It matches
// the generated stdlibPackages set first, then falls back to the goimports
// heuristic — a stdlib import path's first segment carries no dot — so a list
// lagging a Go release never leaks stdlib into coupling.
func isStdlib(importPath string) bool {
	if _, ok := stdlibPackages[importPath]; ok {
		return true
	}

	first, _, _ := strings.Cut(importPath, "/")

	return !strings.Contains(first, ".")
}
