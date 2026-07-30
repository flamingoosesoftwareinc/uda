// Package analysisfs is the single sanctioned constructor for the filesystem
// that uda's analyzers walk. It wraps a directory tree so build and vendor
// directories are pruned before any analyzer sees them, keeping generated and
// third-party trees out of coupling metrics. Every command builds its analysis
// fs.FS here so the ignore policy is enforced in one place rather than repeated
// at each walk site.
//
// The built-in per-language defaults can be extended per repo via an analysis
// section in .uda.yaml:
//
//	analysis:
//	  exclude: [generated]        # added for every language
//	  go:
//	    exclude: [third_party]    # added only when analyzing Go
package analysisfs

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/flamingoosesoftwareinc/fsift"
)

// ignoreDirs lists directory names that are never analyzable source, keyed by
// lowercase language name. Scoped per language so a source directory that
// happens to share another ecosystem's build-dir name (a Go package named
// "target") is not pruned when analyzing that language.
var ignoreDirs = map[string][]string{
	"go":         {"vendor"},
	"rust":       {"target"},
	"typescript": {"node_modules", "dist"},
	"python":     {".venv", "__pycache__"},
	"swift":      {".build"},
}

// New wraps the directory tree at root in an fs.FS that hides build and vendor
// directories from every walk. When language names a known language the prune
// set is that language's build dirs; when it is empty or unknown — the
// multi-language detection path — the prune set is the union across all
// languages.
func New(root, language string) fs.FS {
	cfg := loadUserConfig(root)

	// A known language uses that language's prune set; the multi-language
	// detection path (empty or "auto") unions every language's — both defaults
	// and user overrides — so per-language config still applies when the command
	// hasn't resolved a single language yet.
	var base, extra []string
	if defaults, known := ignoreDirs[strings.ToLower(language)]; known {
		base, extra = defaults, cfg.excludesFor(language)
	} else {
		base, extra = unionIgnoreDirs(), cfg.allExcludes()
	}

	patterns := make([]string, 0, len(base)+len(extra))
	patterns = append(patterns, base...)
	patterns = append(patterns, extra...)

	// The one sanctioned os.DirFS call; TestNoRawDirFS keeps every other
	// analysis path routed through here.
	return fsift.Filtered(os.DirFS(root), fsift.SkipGlobs(patterns...))
}

// loadUserConfig reads the repo-local analysis overrides best-effort: a missing
// or malformed config yields the zero Config (defaults-only). LoadConfig
// surfaces the error to callers that need strict validation, and uda lint
// parses the same file and reports syntax errors.
func loadUserConfig(root string) Config {
	cfg, err := LoadConfig(filepath.Join(root, configFileName))
	if err != nil {
		return Config{}
	}

	return cfg
}

// unionIgnoreDirs returns every ignored directory name across all languages,
// sorted so the prune set is deterministic.
func unionIgnoreDirs() []string {
	seen := make(map[string]struct{})

	var all []string

	for _, dirs := range ignoreDirs {
		for _, dir := range dirs {
			if _, exists := seen[dir]; !exists {
				seen[dir] = struct{}{}
				all = append(all, dir)
			}
		}
	}

	slices.Sort(all)

	return all
}
