// Package detect identifies source languages from a filesystem using enry heuristics.
package detect

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"

	files "github.com/flamingoosesoftwareinc/fsift"
	enry "github.com/go-enry/go-enry/v2"
)

// ErrNoLanguageDetected is returned when enry cannot identify a file's language.
var ErrNoLanguageDetected = errors.New("no language detected")

// enryHeadBytes is the head-of-file byte budget for enry language detection;
// 8 KiB is sufficient for shebangs, common file headers, and the first few
// statements that enry uses for ambiguous-extension disambiguation.
const enryHeadBytes = 8192

// Detect returns the language of a single file at path within dirFS, or
// ErrNoLanguageDetected if none can be identified.
func Detect(_ context.Context, dirFS fs.FS, path string) (string, error) {
	content, err := readFileHead(dirFS, path, enryHeadBytes)
	if err != nil {
		return "", ErrNoLanguageDetected
	}

	lang := enry.GetLanguage(filepath.Base(path), content)
	if lang == "" {
		return "", ErrNoLanguageDetected
	}

	return lang, nil
}

// Languages walks the directory and returns the set of programming
// languages detected using enry. It samples file content for ambiguous
// extensions (e.g., .rs can be Rust or RenderScript).
func Languages(ctx context.Context, dirFS fs.FS) ([]string, error) {
	paths, err := files.ListFiles(
		ctx,
		dirFS,
		files.SkipHiddenDirs(),
		files.SkipHiddenFiles(),
		skipNodeModules(),
	)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})

	for _, path := range paths {
		// Extension lookup first: it is a map probe, while IsVendor and
		// IsGenerated run large regex alternations. Filtering the (typically
		// dominant) extension-less and unknown-extension files here keeps the
		// regexes off the hot path; the surviving set is identical.
		langs := enry.GetLanguagesByExtension(path, nil, nil)
		if len(langs) == 0 {
			continue
		}

		if enry.IsVendor(path) || enry.IsGenerated(path, nil) {
			continue
		}

		var lang string
		if len(langs) == 1 {
			// Unambiguous extension
			lang = langs[0]
		} else {
			// Ambiguous extension (e.g., .rs -> Rust or RenderScript)
			// Read content to disambiguate
			content, err := readFileHead(dirFS, path, enryHeadBytes)
			if err == nil {
				lang = enry.GetLanguage(filepath.Base(path), content)
			}
		}

		if lang != "" {
			seen[lang] = struct{}{}
		}
	}

	langs := make([]string, 0, len(seen))
	for lang := range seen {
		langs = append(langs, lang)
	}

	return langs, nil
}

func skipNodeModules() files.FileFilter {
	return func(_ string, d fs.DirEntry) bool {
		return d.IsDir() && d.Name() == "node_modules"
	}
}

func readFileHead(dirFS fs.FS, path string, maxBytes int64) ([]byte, error) {
	f, err := dirFS.Open(path)
	if err != nil {
		return nil, err
	}

	defer func() { _ = f.Close() }()

	limitReader := io.LimitReader(f, maxBytes)

	return io.ReadAll(limitReader)
}
