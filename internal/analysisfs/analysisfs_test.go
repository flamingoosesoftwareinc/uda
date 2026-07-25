package analysisfs_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/flamingoosesoftwareinc/fsift"
	"github.com/flamingoosesoftwareinc/uda/internal/analysisfs"
	"github.com/stretchr/testify/require"
)

func writeTree(t *testing.T, files ...string) string {
	t.Helper()

	root := t.TempDir()

	for _, rel := range files {
		full := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
		require.NoError(t, os.WriteFile(full, []byte("x"), 0o600))
	}

	return root
}

func TestNew(t *testing.T) {
	t.Parallel()

	files := []string{
		"main.go",
		"vendor/dep.go",
		"node_modules/lib.js",
		"target/bin.rs",
		"src/app.py",
	}

	tests := map[string]struct {
		language string
		want     []string
	}{
		"go prunes only vendor": {
			language: "go",
			want:     []string{"main.go", "node_modules/lib.js", "target/bin.rs", "src/app.py"},
		},
		"rust prunes only target": {
			language: "rust",
			want:     []string{"main.go", "vendor/dep.go", "node_modules/lib.js", "src/app.py"},
		},
		"unknown language prunes the union": {
			language: "",
			want:     []string{"main.go", "src/app.py"},
		},
	}

	ctx := context.Background()

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := writeTree(t, files...)

			got, err := fsift.ListFiles(ctx, analysisfs.New(root, testCase.language))
			require.NoError(t, err)
			require.ElementsMatch(t, testCase.want, got)
		})
	}
}

func TestNewAppliesUserExcludes(t *testing.T) {
	t.Parallel()

	root := writeTree(t,
		"main.go",
		"vendor/dep.go",
		"third_party/lib.go",
		"generated/out.go",
	)
	require.NoError(t, os.WriteFile(
		filepath.Join(root, ".uda.yaml"),
		[]byte("analysis:\n  exclude: [generated]\n  go:\n    exclude: [third_party]\n"),
		0o600,
	))

	// vendor (Go default), generated (global override), and third_party (Go
	// override) are all pruned; only main.go remains. The auto path unions
	// every language's overrides, so it prunes third_party too even though the
	// override is keyed under go.
	for _, language := range []string{"go", "auto"} {
		got, err := fsift.ListFiles(
			context.Background(),
			analysisfs.New(root, language),
			fsift.SkipHiddenFiles(),
		)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"main.go"}, got, "language=%s", language)
	}
}
