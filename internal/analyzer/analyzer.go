package analyzer

import (
	"context"
	"io/fs"
)

type Package string

// Import is semantically identical to package except in how it is used
// re-typed here to explicitly communicate the relationship in PackageImports
type Import Package

// PackageImports is expected to contain a mapping of a package and its dependencies
// e.g. {"analyzer":["context","io/fs"]}
type PackageImports map[Package][]Import

// Analyzer is expected to walk dir and extract the PackageImports
type Analyzer interface {
	Analyze(ctx context.Context, dir fs.FS) (PackageImports, error)
}
