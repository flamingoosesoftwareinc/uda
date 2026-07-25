package tscore

// BoundaryAssigner determines which boundary a file belongs to.
type BoundaryAssigner interface {
	AssignBoundary(filePath string, pkgNames map[Directory]PackageName) string
}

// AssignerFactory builds a BoundaryAssigner given the discovered file list.
// This allows assigners like barrel to inspect the file list for index.ts files.
type AssignerFactory func(tsFiles []string) (BoundaryAssigner, error)
