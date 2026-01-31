package files

import (
	"context"
	"io/fs"
	"log/slog"
	"path/filepath"
)

type FileFilter func(path string, d fs.DirEntry) bool

func ListFiles(ctx context.Context, dirFS fs.FS, filters ...FileFilter) ([]string, error) {
	files := []string{}

	if err := fs.WalkDir(dirFS, ".", func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			shouldSkip := applyFilters(d.Name(), d, filters)
			if shouldSkip {
				slog.DebugContext(ctx, "skipping directory", "name", d.Name())
				return fs.SkipDir
			}
			return nil
		}

		shouldIgnoreFile := applyFilters(path, d, filters)
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

func applyFilters(path string, d fs.DirEntry, filters []FileFilter) bool {
	for _, filter := range filters {
		if filter(path, d) {
			return true
		}
	}

	return false
}

func SkipHiddenDirs() FileFilter {
	return func(path string, d fs.DirEntry) bool {
		if !d.IsDir() {
			return false
		}
		base := filepath.Base(path)
		isHidden, err := isHiddenFile(base)
		if err != nil {
			return true
		}

		return isHidden
	}
}

func SkipHiddenFiles() FileFilter {
	return func(path string, d fs.DirEntry) bool {
		base := filepath.Base(path)
		isHidden, err := isHiddenFile(base)
		if err != nil {
			return true
		}

		return isHidden
	}
}
