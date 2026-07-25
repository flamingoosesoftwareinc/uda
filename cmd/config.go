package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/flamingoosesoftwareinc/pungi"
	"github.com/flamingoosesoftwareinc/xdg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	repoConfigName = ".uda.yaml"
	xdgAppName     = "uda"
	xdgConfigName  = "config.yaml"

	// lintConfigKey is the repo-scoped policy block. It never promotes to
	// user scope: roles, forbids, and the edge lockfile only make sense
	// against the repo they were generated from.
	lintConfigKey = "lint"
)

// Promotion failure sentinels — callers match with errors.Is.
var (
	errNoRepoConfig     = fmt.Errorf("no %s found", repoConfigName)
	errNothingToPromote = errors.New("nothing to promote")
)

//nolint:gochecknoinits // cobra subcommand registration; idiomatic in this codebase per stack.md.
func init() {
	configCmd := pungi.NewCmd(pungi.Options{})
	configCmd.AddCommand(newPromoteCmd())
	rootCmd.AddCommand(configCmd)
}

// findRepoConfig walks from dir toward the filesystem root looking for
// .uda.yaml, stopping after the first directory that contains .git (the
// repo boundary — a config above the repo does not apply to it).
func findRepoConfig(dir string) (string, bool) {
	for {
		candidate := filepath.Join(dir, repoConfigName)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}

		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return "", false
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}

		dir = parent
	}
}

// xdgConfigPath returns the user-scope config path and whether it exists.
// Read-only: never creates the directory.
func xdgConfigPath() (string, bool) {
	base, err := xdg.ConfigHome()
	if err != nil {
		return "", false
	}

	path := filepath.Join(base, xdgAppName, xdgConfigName)

	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return path, false
	}

	return path, true
}

// loadConfig resolves configuration into v. An explicit path wins outright.
// Otherwise user-scope XDG config is read first and the repo-local
// .uda.yaml (found by walking up from cwd) is merged over it — repo values
// win, and ConfigFileUsed() points at the repo file so writes (config
// set/edit) land repo-side by default. Returns the files loaded, in
// precedence order (lowest first).
func loadConfig(v *viper.Viper, explicit, cwd string) ([]string, error) {
	if explicit != "" {
		v.SetConfigFile(explicit)

		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("reading config %s: %w", explicit, err)
		}

		return []string{explicit}, nil
	}

	var loaded []string

	if userCfg, ok := xdgConfigPath(); ok {
		v.SetConfigFile(userCfg)

		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("reading config %s: %w", userCfg, err)
		}

		loaded = append(loaded, userCfg)
	}

	repoCfg, ok := findRepoConfig(cwd)
	if !ok {
		return loaded, nil
	}

	v.SetConfigFile(repoCfg)

	merge := v.ReadInConfig
	if len(loaded) > 0 {
		merge = v.MergeInConfig
	}

	if err := merge(); err != nil {
		return nil, fmt.Errorf("reading config %s: %w", repoCfg, err)
	}

	return append(loaded, repoCfg), nil
}

func newPromoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "promote",
		Short: "Copy general params from the repo .uda.yaml to the user-scope XDG config",
		Long: `Copy the general configuration (analyzer defaults, precision targets,
advisory settings — everything except the repo-scoped lint block) from the
repo-local .uda.yaml into $XDG_CONFIG_HOME/uda/config.yaml so it applies
across repositories. Repo-local values still win when both define a key.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolving working directory: %w", err)
			}

			promoted, dest, err := promoteConfig(cwd)
			if err != nil {
				return err
			}

			cmd.Printf("Promoted %v to %s\n", promoted, dest)

			return nil
		},
	}
}

// promoteConfig copies every top-level key except the lint block from the
// repo-local .uda.yaml into the user-scope XDG config, preserving existing
// user-scope keys. Returns the promoted keys and the destination path.
func promoteConfig(cwd string) ([]string, string, error) {
	repoCfg, ok := findRepoConfig(cwd)
	if !ok {
		return nil, "", fmt.Errorf("%w from %s up to the repo root", errNoRepoConfig, cwd)
	}

	src := viper.New()
	src.SetConfigFile(repoCfg)

	if err := src.ReadInConfig(); err != nil {
		return nil, "", fmt.Errorf("reading config %s: %w", repoCfg, err)
	}

	settings := src.AllSettings()
	delete(settings, lintConfigKey)

	if len(settings) == 0 {
		return nil, "", fmt.Errorf("%w: %s has no general params", errNothingToPromote, repoCfg)
	}

	dir, err := xdg.AppConfig(xdgAppName)
	if err != nil {
		return nil, "", fmt.Errorf("resolving user config directory: %w", err)
	}

	dest := filepath.Join(dir, xdgConfigName)

	dst := viper.New()
	dst.SetConfigFile(dest)

	if err := dst.ReadInConfig(); err != nil && !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("reading config %s: %w", dest, err)
	}

	keys := make([]string, 0, len(settings))

	for key, value := range settings {
		dst.Set(key, value)
		keys = append(keys, key)
	}

	sort.Strings(keys)

	if err := dst.WriteConfigAs(dest); err != nil {
		return nil, "", fmt.Errorf("writing config %s: %w", dest, err)
	}

	return keys, dest, nil
}
