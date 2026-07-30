// Package barrel implements the TypeScript barrel-file boundary strategy.
package barrel

import (
	"path/filepath"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/typescript/internal/tscore"
)

// Assigner assigns files to package boundaries using barrel (index.ts) directories.
type Assigner struct {
	BarrelDirs map[string]struct{}
}

// NewAssigner builds an Assigner from the set of barrel directories in tsFiles.
func NewAssigner(tsFiles []string) (*Assigner, error) {
	barrelDirs := make(map[string]struct{})

	for _, f := range tsFiles {
		base := filepath.Base(f)
		if base == "index.ts" || base == "index.tsx" {
			barrelDirs[filepath.Dir(f)] = struct{}{}
		}
	}

	return &Assigner{BarrelDirs: barrelDirs}, nil
}

// AssignBoundary returns the boundary name for filePath based on the nearest barrel directory.
func (a *Assigner) AssignBoundary(
	filePath string,
	pkgNames map[tscore.Directory]tscore.PackageName,
) string {
	fileDir := filepath.Dir(filePath)

	// Find the package root for this file
	var (
		pkgRoot string
		pkgName string
	)

	for dir := fileDir; ; dir = filepath.Dir(dir) {
		if name, ok := pkgNames[tscore.Directory(dir)]; ok {
			pkgRoot = dir
			pkgName = string(name)

			break
		}

		if dir == "." || dir == filepath.Dir(dir) {
			break
		}
	}

	if pkgName == "" {
		pkgName = fileDir
		pkgRoot = "."
	}

	// Walk up from the file's directory to find the nearest barrel dir
	for dir := fileDir; ; dir = filepath.Dir(dir) {
		if _, ok := a.BarrelDirs[dir]; ok {
			rel, _ := filepath.Rel(pkgRoot, dir)
			if rel == "." {
				return pkgName
			}

			return filepath.Join(pkgName, rel)
		}

		if dir == pkgRoot || dir == "." || dir == filepath.Dir(dir) {
			break
		}
	}

	// No barrel found — collapse to package root
	return pkgName
}
