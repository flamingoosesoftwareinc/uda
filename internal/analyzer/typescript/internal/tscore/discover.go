package tscore

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"

	files "github.com/flamingoosesoftwareinc/fsift"
)

// TypeScript file extensions used by both discovery and resolver passes.
// Hoisted so the literal appears once at the extension registry.
const (
	ExtTS  = ".ts"
	ExtTSX = ".tsx"
)

var buildDirs = map[string]struct{}{
	"dist":     {},
	"build":    {},
	"out":      {},
	"output":   {},
	"coverage": {},
	".next":    {},
	".nuxt":    {},
	".turbo":   {},
}

// ListPackageJSONFiles returns every package.json path under dir, skipping node_modules.
func ListPackageJSONFiles(ctx context.Context, dir fs.FS) ([]string, error) {
	return files.ListFiles(
		ctx,
		dir,
		files.SkipHiddenDirs(),
		files.SkipHiddenFiles(),
		SkipNodeModules(),
		packageJSONFileFilter(),
	)
}

// ListTsconfigFiles returns every tsconfig.json path under dir, skipping node_modules.
func ListTsconfigFiles(ctx context.Context, dir fs.FS) ([]string, error) {
	return files.ListFiles(
		ctx,
		dir,
		files.SkipHiddenDirs(),
		files.SkipHiddenFiles(),
		SkipNodeModules(),
		tsconfigFileFilter(),
	)
}

// ListTsFiles returns every .ts/.tsx path under dir, skipping node_modules, build
// directories, and any directory matching excludePatterns.
func ListTsFiles(ctx context.Context, dir fs.FS, excludePatterns []string) ([]string, error) {
	return files.ListFiles(
		ctx,
		dir,
		files.SkipHiddenDirs(),
		files.SkipHiddenFiles(),
		SkipNodeModules(),
		skipBuildDirs(),
		skipExcludePatterns(excludePatterns),
		TsFileFilter(),
	)
}

// SkipNodeModules returns a filter that skips node_modules directories.
func SkipNodeModules() files.FileFilter {
	return func(_ string, d fs.DirEntry) bool {
		if !d.IsDir() {
			return false
		}

		return d.Name() == "node_modules"
	}
}

func skipBuildDirs() files.FileFilter {
	return func(_ string, d fs.DirEntry) bool {
		if !d.IsDir() {
			return false
		}

		_, ok := buildDirs[d.Name()]

		return ok
	}
}

func skipExcludePatterns(patterns []string) files.FileFilter {
	return func(path string, d fs.DirEntry) bool {
		if !d.IsDir() {
			return false
		}

		for _, pat := range patterns {
			if matchesExclude(path, pat) {
				return true
			}
		}

		return false
	}
}

func matchesExclude(path, pattern string) bool {
	pattern = strings.TrimRight(pattern, "/*")
	if path == pattern {
		return true
	}

	return strings.HasPrefix(path, pattern+"/")
}

func packageJSONFileFilter() files.FileFilter {
	return func(path string, d fs.DirEntry) bool {
		if d.IsDir() {
			return false
		}

		return filepath.Base(path) != "package.json"
	}
}

func tsconfigFileFilter() files.FileFilter {
	return func(path string, d fs.DirEntry) bool {
		if d.IsDir() {
			return false
		}

		return filepath.Base(path) != "tsconfig.json"
	}
}

// TsFileFilter returns a filter that keeps only .ts and .tsx files.
func TsFileFilter() files.FileFilter {
	return func(path string, d fs.DirEntry) bool {
		if d.IsDir() {
			return false
		}

		ext := filepath.Ext(path)

		return ext != ExtTS && ext != ExtTSX
	}
}
