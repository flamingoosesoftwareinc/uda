// Package cmd wires the uda CLI (cobra root + subcommands).
//
// Copyright © 2026 Flamingoose Software Inc <eng@flamingoose.ca>
package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/charmbracelet/fang"
	"github.com/flamingoosesoftwareinc/uda/internal/logschema"
	"github.com/njayp/ophis"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

// Output format identifiers shared by the format/--format flag across
// subcommands. Constants live here so each subcommand switch references
// the canonical token rather than a duplicate string literal.
const (
	FormatAuto         = "auto"
	FormatInteractive  = "interactive"
	FormatJSON         = "json"
	FormatJSONExtended = "json-extended"
	FormatTable        = "table"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "uda",
	Short: "Universal dependency analyzer",
	Long: `uda statically analyzes codebases to extract structural coupling: which
packages import which, how many import a given package, how many it imports,
and the ratio between them.

It also reads git history to extract evolutionary coupling: packages that keep
changing in the same commits, whether or not an import connects them.

The import graph can be pinned in .uda.yaml, which turns it into policy. From
there uda enforces dependency direction and dependency constraints, and a new
edge fails the gate until someone accepts it.

Static analysis is tree-sitter based. Go, TypeScript, Rust, Python and Swift.`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	// Run: func(cmd *cobra.Command, args []string) { },
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		logLevelString := viper.GetString("loglevel")
		logLevel, ok := slogLevel[logLevelString]
		if !ok {
			slog.LogAttrs(cmd.Context(), slog.LevelError, "invalid log-level, defaulting to error",
				slog.String("loglevel", logLevelString),
			)
			slog.SetLogLoggerLevel(slog.LevelError)
		} else {
			slog.SetLogLoggerLevel(logLevel)
		}

		format := viper.GetString("format")
		if format == FormatAuto {
			if term.IsTerminal(int(os.Stdout.Fd())) {
				format = FormatInteractive
			} else {
				format = FormatJSON
			}
			_ = cmd.Flags().Set("format", format)
		}

		return nil
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	ctx := context.Background()

	rootCmd.AddCommand(ophis.Command(nil))

	err := fang.Execute(ctx, rootCmd, fang.WithNotifySignal(os.Interrupt))
	if err != nil {
		os.Exit(1)
	}
}

var slogLevel = map[string]slog.Level{
	"info":  slog.LevelInfo,
	"warn":  slog.LevelWarn,
	"error": slog.LevelError,
	"debug": slog.LevelDebug,
}

//nolint:gochecknoinits // cobra subcommand flag registration; idiomatic in this codebase per stack.md.
func init() {
	cobra.OnInitialize(initConfig)
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	rootCmd.PersistentFlags().
		StringVar(&cfgFile, "config", "",
			"config file (default: repo-local .uda.yaml, merged over $XDG_CONFIG_HOME/uda/config.yaml)")

	rootCmd.PersistentFlags().
		String("loglevel", "error", "logging level")

	if err := viper.BindPFlag(
		"loglevel",
		rootCmd.PersistentFlags().Lookup("loglevel"),
	); err != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "failed to bind",
			logschema.UdaErrorMessage(err.Error()),
		)
	}

	rootCmd.PersistentFlags().
		String("format", "auto", "output format (auto, json, json-extended, table, interactive)")

	if err := viper.BindPFlag(
		"format",
		rootCmd.PersistentFlags().Lookup("format"),
	); err != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "failed to bind",
			logschema.UdaErrorMessage(err.Error()),
		)
	}

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

// initConfig resolves configuration: an explicit --config path wins;
// otherwise the repo-local .uda.yaml (walking up from the working
// directory) is merged over the user-scope XDG config.
func initConfig() {
	cwd, err := os.Getwd()
	cobra.CheckErr(err)

	_, err = loadConfig(viper.GetViper(), cfgFile, cwd)
	cobra.CheckErr(err)

	warnLegacyConfig()

	viper.AutomaticEnv() // read in environment variables that match
}

// warnLegacyConfig flags the retired $HOME/.uda.yaml location so existing
// setups learn where their config went instead of being silently ignored.
func warnLegacyConfig() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	legacy := filepath.Join(home, ".uda.yaml")
	if _, err := os.Stat(legacy); err != nil {
		return
	}

	fmt.Fprintf(os.Stderr,
		"warning: %s is no longer read; move it to a repo-local %s or $XDG_CONFIG_HOME/uda/%s\n",
		legacy, repoConfigName, xdgConfigName)
}
