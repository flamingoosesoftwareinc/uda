package analyzer_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/all"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// analyzerSubdirs scans internal/analyzer/ for subdirectories that contain
// .go files (excluding hidden dirs, the "internal" dir name, and test-only
// packages whose only exported type doesn't implement Analyzer).
func analyzerSubdirs(t *testing.T) []string {
	t.Helper()

	// Resolve the internal/analyzer directory relative to this test file.
	// os.Getwd() in a test returns the package directory.
	pkgDir, err := os.Getwd()
	require.NoError(t, err)

	var dirs []string

	entries, err := os.ReadDir(pkgDir)
	require.NoError(t, err)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		name := e.Name()
		// Skip hidden dirs, "internal" dirs, and the parent package itself.
		if strings.HasPrefix(name, ".") || name == "internal" || name == "all" {
			continue
		}

		// Check that the subdirectory contains at least one non-test .go file.
		subDir := filepath.Join(pkgDir, name)
		hasGoFile := false
		_ = filepath.WalkDir(subDir, func(_ string, d fs.DirEntry, err error) error {
			if err != nil || hasGoFile {
				return err
			}

			if !d.IsDir() && strings.HasSuffix(d.Name(), ".go") &&
				!strings.HasSuffix(d.Name(), "_test.go") {
				hasGoFile = true
			}

			return nil
		})

		if hasGoFile {
			dirs = append(dirs, name)
		}
	}

	return dirs
}

func TestAll_CoversEverySubpackage(t *testing.T) {
	all := all.Analyzers()

	// Build a set of names returned by All().
	names := make(map[string]struct{}, len(all))
	for _, a := range all {
		names[strings.ToLower(a.Name())] = struct{}{}
	}

	// Map subdirectory names to the Name() values we expect each analyzer to return.
	// Subdirectory "golang" → analyzer Name() "Go", etc.
	dirToName := map[string]string{
		"golang":     "go",
		"python":     "python",
		"rust":       "rust",
		"swift":      "swift",
		"typescript": "typescript",
	}

	subdirs := analyzerSubdirs(t)
	for _, dir := range subdirs {
		expectedName, ok := dirToName[dir]
		if !ok {
			// If a new subdirectory appears that isn't in the map yet, the test
			// fails so the author knows to update both the map and All().
			assert.Fail(t, "subdirectory has no expected Name() entry in dirToName",
				"add %q to dirToName and ensure All() includes its analyzer", dir)

			continue
		}

		assert.Contains(
			t,
			names,
			expectedName,
			"All() is missing the analyzer for subdirectory %q (expected Name() == %q)",
			dir,
			expectedName,
		)
	}
}
