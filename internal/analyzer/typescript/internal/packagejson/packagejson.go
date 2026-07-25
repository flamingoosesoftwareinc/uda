// Package packagejson implements the TypeScript per-package.json boundary strategy.
package packagejson

import (
	"path/filepath"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/typescript/internal/tscore"
)

// Assigner assigns files to package boundaries at per-package.json granularity.
type Assigner struct{}

// AssignBoundary returns the boundary name for filePath at per-package.json granularity.
func (Assigner) AssignBoundary(
	filePath string,
	pkgNames map[tscore.Directory]tscore.PackageName,
) string {
	fileDir := filepath.Dir(filePath)

	for dir := fileDir; ; dir = filepath.Dir(dir) {
		pkgName, ok := pkgNames[tscore.Directory(dir)]
		if ok {
			return string(pkgName)
		}

		if dir == "." || dir == filepath.Dir(dir) {
			break
		}
	}

	// No package.json found — fall back to the file's directory
	return fileDir
}
