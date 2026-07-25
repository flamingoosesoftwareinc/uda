package analysisfs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analysisfs"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, ".uda.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return path
}

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	t.Run("global and per-language excludes parse", func(t *testing.T) {
		t.Parallel()

		path := writeConfig(t, `analysis:
  exclude: [generated]
  go:
    exclude: [third_party, testdata]
`)

		cfg, err := analysisfs.LoadConfig(path)
		require.NoError(t, err)
		require.Equal(t, []string{"generated"}, cfg.Exclude)
		require.Equal(t, map[string]analysisfs.LanguageRules{
			"go": {Exclude: []string{"third_party", "testdata"}},
		}, cfg.Languages)
	})

	t.Run("missing analysis section is ErrNoConfig", func(t *testing.T) {
		t.Parallel()

		path := writeConfig(t, "lint:\n  go:\n    allowed: []\n")

		_, err := analysisfs.LoadConfig(path)
		require.ErrorIs(t, err, analysisfs.ErrNoConfig)
	})

	t.Run("missing file is ErrNoConfig", func(t *testing.T) {
		t.Parallel()

		_, err := analysisfs.LoadConfig(filepath.Join(t.TempDir(), "absent.yaml"))
		require.ErrorIs(t, err, analysisfs.ErrNoConfig)
	})

	t.Run("malformed yaml is a parse error, not ErrNoConfig", func(t *testing.T) {
		t.Parallel()

		path := writeConfig(t, "analysis: [not-a-map\n")

		_, err := analysisfs.LoadConfig(path)
		require.Error(t, err)
		require.NotErrorIs(t, err, analysisfs.ErrNoConfig)
	})
}
