package tscore

import (
	"path/filepath"
	"strings"
)

// ResolveImportToFile resolves an import source to a concrete file path within
// allFilePaths, honoring relative paths and tsconfig path aliases. It returns ""
// for bare (external) imports that resolve to no local file.
func ResolveImportToFile(
	importSource string,
	currentFilePath string,
	aliases []PathAlias,
	allFilePaths map[string]struct{},
) string {
	if strings.HasPrefix(importSource, "./") || strings.HasPrefix(importSource, "../") {
		currentDir := filepath.Dir(currentFilePath)
		resolved := filepath.Join(currentDir, importSource)
		resolved = filepath.Clean(resolved)

		if found := TryResolveFile(resolved, allFilePaths); found != "" {
			return found
		}

		return ""
	}

	for _, alias := range aliases {
		if after, ok := strings.CutPrefix(importSource, alias.Prefix); ok {
			remainder := after
			for _, target := range alias.Targets {
				candidate := filepath.Join(target, remainder)

				candidate = filepath.Clean(candidate)
				if found := TryResolveFile(candidate, allFilePaths); found != "" {
					return found
				}
			}
		}
	}

	// Bare import (external dependency) — no file resolution
	return ""
}

// TryResolveFile returns the file in allFilePaths that basePath resolves to,
// trying the common TypeScript extension and index-file conventions, or "" if none.
func TryResolveFile(basePath string, allFilePaths map[string]struct{}) string {
	if before, ok := strings.CutSuffix(basePath, ".js"); ok {
		tsPath := before + ".ts"
		if _, ok := allFilePaths[tsPath]; ok {
			return tsPath
		}

		tsxPath := strings.TrimSuffix(basePath, ".js") + ".tsx"
		if _, ok := allFilePaths[tsxPath]; ok {
			return tsxPath
		}
	}

	if before, ok := strings.CutSuffix(basePath, ".jsx"); ok {
		tsxPath := before + ".tsx"
		if _, ok := allFilePaths[tsxPath]; ok {
			return tsxPath
		}
	}

	extensions := []string{".ts", ".tsx", ".js", ".jsx"}
	for _, ext := range extensions {
		candidate := basePath + ext
		if _, ok := allFilePaths[candidate]; ok {
			return candidate
		}
	}

	if _, ok := allFilePaths[basePath]; ok {
		return basePath
	}

	for _, ext := range extensions {
		candidate := filepath.Join(basePath, "index"+ext)
		if _, ok := allFilePaths[candidate]; ok {
			return candidate
		}
	}

	return ""
}
