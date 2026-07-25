// Package directory implements the TypeScript per-directory boundary strategy.
package directory

import (
	"path/filepath"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/typescript/internal/tscore"
)

// Assigner assigns files to package boundaries at per-directory granularity.
type Assigner struct{}

// AssignBoundary returns the boundary name for filePath at per-directory granularity.
func (Assigner) AssignBoundary(
	filePath string,
	pkgNames map[tscore.Directory]tscore.PackageName,
) string {
	fileDir := filepath.Dir(filePath)

	for dir := fileDir; ; dir = filepath.Dir(dir) {
		pkgName, ok := pkgNames[tscore.Directory(dir)]
		if ok {
			rel, _ := filepath.Rel(dir, fileDir)
			if rel == "." {
				return string(pkgName)
			}

			return filepath.Join(string(pkgName), rel)
		}

		if dir == "." || dir == filepath.Dir(dir) {
			break
		}
	}

	return fileDir
}
