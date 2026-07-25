package analysisfs_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNoRawDirFS enforces that analysis code builds its filesystem through
// analysisfs.New rather than calling os.DirFS directly, so a new command cannot
// silently bypass the vendor/build ignore policy. Enforcing this in a test that
// reads the code — rather than with runtime magic — keeps the coupling visible.
func TestNoRawDirFS(t *testing.T) {
	t.Parallel()

	var offenders []string

	for _, dir := range []string{"../../cmd", "../../internal"} {
		err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			if entry.IsDir() || !strings.HasSuffix(path, ".go") ||
				strings.HasSuffix(path, "_test.go") {
				return nil
			}

			// analysisfs is the one sanctioned home for os.DirFS.
			if strings.Contains(filepath.ToSlash(path), "internal/analysisfs/") {
				return nil
			}

			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			if strings.Contains(string(src), "os.DirFS(") {
				offenders = append(offenders, path)
			}

			return nil
		})
		require.NoError(t, err)
	}

	require.Empty(t, offenders, "os.DirFS used outside analysisfs; route through analysisfs.New")
}
