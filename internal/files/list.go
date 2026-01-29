package files

import (
	"context"
	"io/fs"
	"log/slog"
	"path/filepath"
)

type FileFilter func(path string) bool

func ListFiles(ctx context.Context, dirFS fs.FS, filters ...FileFilter) ([]string, error) {
	files := []string{}

	if err := fs.WalkDir(dirFS, ".", func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			shouldSkip := applyFilters(d.Name(), filters)
			if shouldSkip {
				slog.DebugContext(ctx, "skipping directory", "name", d.Name())
				return fs.SkipDir
			}
			return nil
		}

		shouldIgnoreFile := applyFilters(path, filters)
		if shouldIgnoreFile {
			slog.DebugContext(ctx, "skipping file", "path", path)
			return nil
		}

		files = append(files, path)

		return nil
	}); err != nil {
		return files, err
	}

	return files, nil
}

func applyFilters(path string, filters []FileFilter) bool {
	for _, filter := range filters {
		if filter(path) {
			return true
		}
	}

	return false
}

func SkipHidden() FileFilter {
	return func(path string) bool {
		base := filepath.Base(path)
		isHidden, err := isHiddenFile(base)
		if err != nil {
			return true
		}

		return isHidden
	}
}
