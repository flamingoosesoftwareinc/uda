package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/flamingoosesoftwareinc/uda/cmd/ui"
	"github.com/flamingoosesoftwareinc/uda/internal/cache"
	"github.com/flamingoosesoftwareinc/uda/internal/git"
	"github.com/flamingoosesoftwareinc/uda/internal/history"
	"github.com/flamingoosesoftwareinc/uda/internal/logschema"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

var historyCmd = &cobra.Command{
	Use:   "history <commit-ish>[..<commit-ish>]",
	Short: "Capture coupling/instability metrics for git commits",
	Long: `Analyze git commits and capture coupling/instability metrics.

Supports single commits or commit ranges:
  uda history HEAD          # Analyze current commit
  uda history abc123        # Analyze specific commit
  uda history HEAD~3..HEAD  # Analyze range of commits

Results are cached at $HOME/.cache/uda/<repo>/<sha>/metrics.json.

Output formats (--format flag):
  auto        Detect terminal and choose interactive or json (default)
  json        JSON output
  interactive Interactive TUI with charts
  table       Non-interactive chart output`,
	Args: cobra.ExactArgs(1),
	RunE: runHistory,
}

//nolint:gochecknoinits // cobra subcommand flag registration; idiomatic in this codebase per stack.md.
func init() {
	historyCmd.Flags().
		String("language", "auto", "language to analyze (auto, go, rust, typescript)")
	historyCmd.Flags().
		String("cache-dir", "", "cache directory (default: $HOME/.cache/uda)")
	// Note: --format flag is inherited from root command (auto, json, table, interactive)
	if err := viper.BindPFlag(
		"history.cache-dir",
		historyCmd.Flags().Lookup("cache-dir"),
	); err != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "failed to bind",
			logschema.UdaErrorMessage(err.Error()),
		)
	}

	rootCmd.AddCommand(historyCmd)
}

func runHistory(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	rangeStr := args[0]

	// Open git repository
	repo, err := git.OpenRepository(".")
	if err != nil {
		return fmt.Errorf("not a git repository: %w", err)
	}

	// Parse commit range
	commitRange, err := git.ParseCommitRange(repo, rangeStr)
	if err != nil {
		return fmt.Errorf("invalid commit range: %w", err)
	}

	// Get cache configuration
	cacheDir := viper.GetString("history.cache-dir")
	c := cache.NewCache(cache.Config{BaseDir: cacheDir})

	// Get language flag
	language, _ := cmd.Flags().GetString("language")

	// Get format flag
	format, _ := cmd.Flags().GetString("format")

	// Create analyzer and run
	histAnalyzer := history.NewAnalyzer(repo, c, language)

	results, err := histAnalyzer.AnalyzeRange(ctx, commitRange)
	if err != nil {
		return err
	}

	// Output based on format
	switch format {
	case FormatInteractive:
		if !term.IsTerminal(int(os.Stdout.Fd())) {
			return errors.New(
				"interactive format requires a terminal; use --format table or --format json",
			)
		}
		// Create a fetcher function for detailed metrics on demand
		fetcher := func(ctx context.Context, sha string) ([]history.LanguageMetrics, error) {
			return histAnalyzer.AnalyzeCommitFull(ctx, sha)
		}
		// Create workspace checkout function for opening files from specific commits
		workspaceCheckout := func(sha string) (string, error) {
			return histAnalyzer.CheckoutWorkspace(sha)
		}

		return ui.RunHistoryInteractive(results, fetcher, workspaceCheckout)
	case FormatTable:
		ui.PrintHistoryTable(cmd.OutOrStdout(), results)
	case FormatJSON:
		output, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling output: %w", err)
		}

		_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(output))
	default:
		return fmt.Errorf("unknown format: %s (valid: json, interactive, table)", format)
	}

	return nil
}
