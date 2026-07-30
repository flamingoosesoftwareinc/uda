package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestFindRepoConfig(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		files    []string // relative paths to create (.git created as dir)
		startRel string   // dir to search from, relative to tmp root
		wantRel  string   // expected config path relative to tmp root; "" = not found
	}{
		"found_in_start_dir": {
			files:    []string{".git/", ".uda.yaml"},
			startRel: ".",
			wantRel:  ".uda.yaml",
		},
		"walks_up_to_repo_root": {
			files:    []string{".git/", ".uda.yaml", "internal/deep/pkg/"},
			startRel: "internal/deep/pkg",
			wantRel:  ".uda.yaml",
		},
		"stops_at_git_root": {
			// Config above the repo boundary must not apply to it.
			files:    []string{".uda.yaml", "repo/.git/", "repo/internal/"},
			startRel: "repo/internal",
			wantRel:  "",
		},
		"no_config_no_git_reaches_fs_root": {
			files:    []string{"a/b/"},
			startRel: "a/b",
			wantRel:  "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()

			for _, f := range tt.files {
				abs := filepath.Join(root, filepath.FromSlash(f))
				if f[len(f)-1] == '/' {
					require.NoError(t, os.MkdirAll(abs, 0o755))
				} else {
					writeFile(t, abs, "loglevel: debug\n")
				}
			}

			got, ok := findRepoConfig(filepath.Join(root, filepath.FromSlash(tt.startRel)))

			if tt.wantRel == "" {
				require.False(t, ok, "got %s", got)

				return
			}

			require.True(t, ok)
			require.Equal(t, filepath.Join(root, filepath.FromSlash(tt.wantRel)), got)
		})
	}
}

func TestLoadConfig(t *testing.T) {
	t.Run("explicit_path_wins_outright", func(t *testing.T) {
		xdgHome := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", xdgHome)
		writeFile(t, filepath.Join(xdgHome, "uda", "config.yaml"), "since: 90d\n")

		repo := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))
		writeFile(t, filepath.Join(repo, ".uda.yaml"), "since: 30d\n")

		explicit := filepath.Join(t.TempDir(), "custom.yaml")
		writeFile(t, explicit, "since: 7d\n")

		v := viper.New()
		loaded, err := loadConfig(v, explicit, repo)
		require.NoError(t, err)
		require.Equal(t, []string{explicit}, loaded)
		require.Equal(t, "7d", v.GetString("since"))
	})

	t.Run("repo_local_merges_over_xdg", func(t *testing.T) {
		xdgHome := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", xdgHome)
		writeFile(t, filepath.Join(xdgHome, "uda", "config.yaml"),
			"since: 90d\ntarget-precision: 0.05\n")

		repo := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))
		writeFile(t, filepath.Join(repo, ".uda.yaml"), "since: 30d\n")

		v := viper.New()
		loaded, err := loadConfig(v, "", repo)
		require.NoError(t, err)
		require.Len(t, loaded, 2)

		// Repo value wins on conflict; XDG-only keys survive the merge.
		require.Equal(t, "30d", v.GetString("since"))
		require.Equal(t, "0.05", v.GetString("target-precision"))

		// Writes (config set/edit) must land repo-side when both exist.
		require.Equal(t, filepath.Join(repo, ".uda.yaml"), v.ConfigFileUsed())
	})

	t.Run("xdg_only", func(t *testing.T) {
		xdgHome := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", xdgHome)
		writeFile(t, filepath.Join(xdgHome, "uda", "config.yaml"), "since: 90d\n")

		repo := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))

		v := viper.New()
		loaded, err := loadConfig(v, "", repo)
		require.NoError(t, err)
		require.Len(t, loaded, 1)
		require.Equal(t, "90d", v.GetString("since"))
	})

	t.Run("no_config_anywhere_is_fine", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		repo := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))

		v := viper.New()
		loaded, err := loadConfig(v, "", repo)
		require.NoError(t, err)
		require.Empty(t, loaded)
	})
}

func TestPromoteConfig(t *testing.T) {
	t.Run("promotes_general_params_never_lint", func(t *testing.T) {
		xdgHome := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", xdgHome)
		writeFile(t, filepath.Join(xdgHome, "uda", "config.yaml"), "mi: false\n")

		repo := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))
		writeFile(
			t,
			filepath.Join(repo, ".uda.yaml"),
			"since: 30d\ntarget-precision: 0.1\nlint:\n  go:\n    allowed:\n      - cmd -> internal/analyzer\n",
		)

		keys, dest, err := promoteConfig(repo)
		require.NoError(t, err)
		require.Equal(t, []string{"since", "target-precision"}, keys)

		promoted := viper.New()
		promoted.SetConfigFile(dest)
		require.NoError(t, promoted.ReadInConfig())

		require.Equal(t, "30d", promoted.GetString("since"))
		require.InEpsilon(t, 0.1, promoted.GetFloat64("target-precision"), 1e-9)
		require.False(t, promoted.IsSet(lintConfigKey),
			"lint block is repo-scoped and must never promote")
		require.False(t, promoted.GetBool("mi"),
			"pre-existing user-scope keys must survive promotion")

		// The repo config is a source, not a move: it stays intact.
		src := viper.New()
		src.SetConfigFile(filepath.Join(repo, ".uda.yaml"))
		require.NoError(t, src.ReadInConfig())
		require.True(t, src.IsSet(lintConfigKey))
	})

	t.Run("lint_only_config_has_nothing_to_promote", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		repo := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))
		writeFile(t, filepath.Join(repo, ".uda.yaml"), "lint:\n  go:\n    allowed: []\n")

		_, _, err := promoteConfig(repo)
		require.ErrorIs(t, err, errNothingToPromote)
	})

	t.Run("no_repo_config_errors", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		repo := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))

		_, _, err := promoteConfig(repo)
		require.ErrorIs(t, err, errNoRepoConfig)
	})
}
