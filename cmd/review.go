package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/flamingoosesoftwareinc/uda/cmd/ui"
	"github.com/flamingoosesoftwareinc/uda/internal/analysisfs"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/cache"
	"github.com/flamingoosesoftwareinc/uda/internal/diff"
	"github.com/flamingoosesoftwareinc/uda/internal/evocoupling"
	"github.com/flamingoosesoftwareinc/uda/internal/git"
	"github.com/flamingoosesoftwareinc/uda/internal/history"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

var reviewCmd = &cobra.Command{
	Use:   "review [<base>[..<head>]]",
	Short: "Show architectural diff between two snapshots",
	Long: `Compare coupling and dependency changes between two code snapshots.

Without arguments, compares the working tree against HEAD.
With a single commit, compares that commit against its parent.
With a range, compares base against head.

Examples:
  uda review                  # working tree vs HEAD
  uda review HEAD~1           # HEAD~1 vs its parent
  uda review HEAD~3..HEAD     # compare range`,
	Args: cobra.MaximumNArgs(1),
	RunE: runReview,
}

//nolint:gochecknoinits // cobra subcommand flag registration; idiomatic in this codebase per stack.md.
func init() {
	reviewCmd.Flags().
		String("language", "auto", "language to analyze (auto, go, rust, typescript)")
	reviewCmd.Flags().
		String("cache-dir", "", "cache directory (default: $HOME/.cache/uda)")
	reviewCmd.Flags().
		Bool("advisory", true, "emit co-change advisories (informational, never affects exit code)")

	if err := viper.BindPFlag(
		"review.cache-dir",
		reviewCmd.Flags().Lookup("cache-dir"),
	); err != nil {
		fmt.Fprintf(os.Stderr, "failed to bind: %v\n", err)
	}

	rootCmd.AddCommand(reviewCmd)
}

func runReview(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	language, _ := cmd.Flags().GetString("language")
	format, _ := cmd.Flags().GetString("format")

	// Resolve "auto" format: interactive if terminal, table otherwise.
	switch format {
	case FormatAuto:
		if term.IsTerminal(int(os.Stdout.Fd())) {
			format = FormatInteractive
		} else {
			format = FormatTable
		}
	case FormatInteractive:
		if !term.IsTerminal(int(os.Stdout.Fd())) {
			return errors.New(
				"interactive format requires a terminal; use --format table or --format json",
			)
		}
	}

	repo, err := git.OpenRepository(".")
	if err != nil {
		return fmt.Errorf("not a git repository: %w", err)
	}

	cacheDir := viper.GetString("review.cache-dir")
	c := cache.NewCache(cache.Config{BaseDir: cacheDir})
	histAnalyzer := history.NewAnalyzer(repo, c, language)

	snapshot, err := resolveReviewSnapshot(ctx, histAnalyzer, repo, args, language)
	if err != nil {
		return err
	}

	diffs := diff.AllPackages(snapshot.prevMetrics, snapshot.currMetrics)

	result := ui.ReviewResult{
		BaseLabel: snapshot.baseLabel,
		HeadLabel: snapshot.headLabel,
		Diffs:     diffs,
		PrevAll:   snapshot.prevMetrics,
		CurrAll:   snapshot.currMetrics,
	}

	// The advisory channel is informational: a failure here degrades to a
	// stderr note, never a failed review.
	if enabled, _ := cmd.Flags().GetBool("advisory"); enabled {
		advisories, err := reviewAdvisories(ctx, repo, snapshot, language)
		if err != nil {
			fmt.Fprintf(os.Stderr, "co-change advisory skipped: %v\n", err)
		} else {
			result.Advisories = advisories
		}
	}

	switch format {
	case FormatInteractive:
		return ui.RunReviewInteractive(result)
	case FormatJSON:
		output, err := ui.ReviewJSON(result)
		if err != nil {
			return err
		}

		_, _ = fmt.Fprintln(cmd.OutOrStdout(), output)
	case FormatTable:
		_, _ = fmt.Fprint(cmd.OutOrStdout(), ui.ReviewTable(result))
	default:
		_, _ = fmt.Fprint(cmd.OutOrStdout(), ui.ReviewText(result))
	}

	return nil
}

// reviewSnapshot bundles the two metric snapshots, their display labels,
// and what the compared change actually touched — the advisory channel
// checks the touched set against the co-change model. headTime anchors
// that model's history window at the head snapshot (falling back to the
// wall clock for the working tree) so range reviews are reproducible.
type reviewSnapshot struct {
	prevMetrics  []analyzer.Metrics
	currMetrics  []analyzer.Metrics
	baseLabel    string
	headLabel    string
	touchedFiles []string
	headTime     time.Time
}

// resolveReviewSnapshot picks the comparison mode from args: no args compares
// the working tree against HEAD, a range compares base against head, and a
// single commit compares against its parent.
func resolveReviewSnapshot(
	ctx context.Context,
	histAnalyzer *history.Analyzer,
	repo *git.Repository,
	args []string,
	language string,
) (reviewSnapshot, error) {
	if len(args) == 0 {
		return reviewWorkingTree(ctx, histAnalyzer, repo, language)
	}

	rangeStr := args[0]
	if isRange(rangeStr) {
		return reviewRange(ctx, histAnalyzer, repo, rangeStr)
	}

	return reviewSingleCommit(ctx, histAnalyzer, repo, rangeStr)
}

// reviewWorkingTree compares the working tree against HEAD.
func reviewWorkingTree(
	ctx context.Context,
	histAnalyzer *history.Analyzer,
	repo *git.Repository,
	language string,
) (reviewSnapshot, error) {
	prevMetrics, err := analyzeCommitFull(ctx, histAnalyzer, repo, "HEAD")
	if err != nil {
		return reviewSnapshot{}, fmt.Errorf("analyzing HEAD: %w", err)
	}

	currMetrics, err := analyzeWorkingTree(ctx, language)
	if err != nil {
		return reviewSnapshot{}, fmt.Errorf("analyzing working tree: %w", err)
	}

	touched, err := repo.WorkingTreeChangedFiles()
	if err != nil {
		return reviewSnapshot{}, fmt.Errorf("resolving working tree changes: %w", err)
	}

	return reviewSnapshot{
		prevMetrics:  prevMetrics,
		currMetrics:  currMetrics,
		baseLabel:    "HEAD",
		headLabel:    "working tree",
		touchedFiles: touched,
	}, nil
}

// reviewRange compares the base and head commits of a base..head range.
func reviewRange(
	ctx context.Context,
	histAnalyzer *history.Analyzer,
	repo *git.Repository,
	rangeStr string,
) (reviewSnapshot, error) {
	commitRange, err := git.ParseCommitRange(repo, rangeStr)
	if err != nil {
		return reviewSnapshot{}, fmt.Errorf("invalid commit range: %w", err)
	}

	prevMetrics, err := analyzeCommitFull(ctx, histAnalyzer, repo, commitRange.From.String())
	if err != nil {
		return reviewSnapshot{}, fmt.Errorf("analyzing base: %w", err)
	}

	currMetrics, err := analyzeCommitFull(ctx, histAnalyzer, repo, commitRange.To.String())
	if err != nil {
		return reviewSnapshot{}, fmt.Errorf("analyzing head: %w", err)
	}

	touched, headTime, err := rangeTouchedFiles(repo, commitRange)
	if err != nil {
		return reviewSnapshot{}, fmt.Errorf("resolving range changes: %w", err)
	}

	return reviewSnapshot{
		prevMetrics:  prevMetrics,
		currMetrics:  currMetrics,
		baseLabel:    commitRange.From.String()[:7],
		headLabel:    commitRange.To.String()[:7],
		touchedFiles: touched,
		headTime:     headTime,
	}, nil
}

// rangeTouchedFiles collects the union of files changed by the commits in
// the range, plus the head commit's timestamp for anchoring the advisory
// history window.
func rangeTouchedFiles(
	repo *git.Repository,
	commitRange *git.CommitRange,
) ([]string, time.Time, error) {
	hashes, err := repo.Commits(commitRange)
	if err != nil {
		return nil, time.Time{}, err
	}

	commitFiles, err := repo.CommitChangedFiles(hashes)
	if err != nil {
		return nil, time.Time{}, err
	}

	seen := make(map[string]bool)

	var touched []string

	for _, cf := range commitFiles {
		for _, file := range cf.Files {
			if !seen[file] {
				seen[file] = true

				touched = append(touched, file)
			}
		}
	}

	head, err := repo.Repo().CommitObject(commitRange.To)
	if err != nil {
		return nil, time.Time{}, err
	}

	return touched, head.Author.When, nil
}

// reviewSingleCommit compares a single commit against its parent (or "(none)"
// when the commit is a root).
func reviewSingleCommit(
	ctx context.Context,
	histAnalyzer *history.Analyzer,
	repo *git.Repository,
	rangeStr string,
) (reviewSnapshot, error) {
	sha, err := repo.ResolveCommit(rangeStr)
	if err != nil {
		return reviewSnapshot{}, fmt.Errorf("resolving commit: %w", err)
	}

	commit, err := repo.Repo().CommitObject(sha)
	if err != nil {
		return reviewSnapshot{}, fmt.Errorf("getting commit: %w", err)
	}

	currMetrics, err := analyzeCommitFull(ctx, histAnalyzer, repo, sha.String())
	if err != nil {
		return reviewSnapshot{}, fmt.Errorf("analyzing commit: %w", err)
	}

	touched, err := repo.CommitChangedFiles([]plumbing.Hash{sha})
	if err != nil {
		return reviewSnapshot{}, fmt.Errorf("resolving commit changes: %w", err)
	}

	snapshot := reviewSnapshot{
		currMetrics: currMetrics,
		headLabel:   sha.String()[:7],
		baseLabel:   "(none)",
		headTime:    commit.Author.When,
	}

	if len(touched) > 0 {
		snapshot.touchedFiles = touched[0].Files
	}

	if commit.NumParents() == 0 {
		return snapshot, nil
	}

	parent, err := commit.Parents().Next()
	if err != nil {
		return reviewSnapshot{}, fmt.Errorf("getting parent commit: %w", err)
	}

	snapshot.baseLabel = parent.Hash.String()[:7]

	prevMetrics, err := analyzeCommitFull(ctx, histAnalyzer, repo, parent.Hash.String())
	if err != nil {
		return reviewSnapshot{}, fmt.Errorf("analyzing parent: %w", err)
	}

	snapshot.prevMetrics = prevMetrics

	return snapshot, nil
}

func isRange(s string) bool {
	for i := range len(s) - 1 {
		if s[i] == '.' && s[i+1] == '.' {
			return true
		}
	}

	return false
}

// analyzeCommitFull gets full metrics for a commit via the history analyzer.
func analyzeCommitFull(
	ctx context.Context,
	h *history.Analyzer,
	repo *git.Repository,
	commitish string,
) ([]analyzer.Metrics, error) {
	sha, err := repo.ResolveCommit(commitish)
	if err != nil {
		return nil, err
	}

	langMetrics, err := h.AnalyzeCommitFull(ctx, sha.String())
	if err != nil {
		return nil, err
	}

	return flattenLangMetrics(langMetrics), nil
}

// analyzeWorkingTree runs analysis on the current working directory.
func analyzeWorkingTree(ctx context.Context, language string) ([]analyzer.Metrics, error) {
	dirFS := analysisfs.New(".", language)

	analyzers, err := selectAnalyzers(ctx, dirFS, language, "")
	if err != nil {
		return nil, err
	}

	var all []analyzer.Metrics

	for _, a := range analyzers {
		metrics, err := a.Analyze(ctx, dirFS)
		if err != nil {
			return nil, err
		}

		all = append(all, metrics...)
	}

	return all, nil
}

// flattenLangMetrics flattens language-grouped metrics into a single slice.
func flattenLangMetrics(langMetrics []history.LanguageMetrics) []analyzer.Metrics {
	total := 0
	for _, l := range langMetrics {
		total += len(l.Metrics)
	}

	all := make([]analyzer.Metrics, 0, total)
	for _, l := range langMetrics {
		all = append(all, l.Metrics...)
	}

	return all
}

// Advisory config defaults. All overridable via the advisory.coupling
// block of .uda.yaml (or user-scope config) — shared with any future
// advisory consumer.
const (
	defaultAdvisorySince   = "90d"
	defaultAdvisoryMinCorr = 0.6
)

// reviewAdvisories builds the co-change model from history before the
// snapshot head and compares it against what the review's change set
// touched: a package that historically changes with a touched one but was
// left alone this time earns an advisory.
func reviewAdvisories(
	ctx context.Context,
	repo *git.Repository,
	snapshot reviewSnapshot,
	language string,
) ([]evocoupling.Advisory, error) {
	sinceStr := viper.GetString("advisory.coupling.since")
	if sinceStr == "" {
		sinceStr = defaultAdvisorySince
	}

	since, err := parseSinceDuration(sinceStr)
	if err != nil {
		return nil, fmt.Errorf("parsing advisory.coupling.since: %w", err)
	}

	minCorr := viper.GetFloat64("advisory.coupling.min-correlation")
	if minCorr == 0 {
		minCorr = defaultAdvisoryMinCorr
	}

	end := snapshot.headTime
	if end.IsZero() {
		end = time.Now()
	}

	hashes, err := repo.CommitsWithin(since, end)
	if err != nil {
		return nil, fmt.Errorf("collecting history window: %w", err)
	}

	if len(hashes) == 0 {
		return nil, nil
	}

	commits, resolver, err := timedPackageSets(
		ctx, analysisfs.New(repo.RootPath(), language), repo, hashes, language, "package")
	if err != nil {
		return nil, err
	}

	if len(commits) == 0 || resolver == nil {
		return nil, nil
	}

	sigma, err := advisorySigma(commits, since, end)
	if err != nil {
		return nil, err
	}

	pairs := evocoupling.Analyze(commits, evocoupling.Options{
		Sigma:       sigma,
		MinCorr:     minCorr,
		WindowStart: end.Add(-since),
	})

	touched := make(map[string]bool)
	for pkg := range resolver.ResolveCommit(snapshot.touchedFiles) {
		touched[pkg] = true
	}

	return evocoupling.Advise(pairs, touched, evocoupling.AdviseOptions{
		MinCorrelation:    minCorr,
		IncludeLowSupport: viper.GetBool("advisory.coupling.include-low-support"),
	}), nil
}

// advisorySigma honors an explicit advisory.coupling.sigma, otherwise
// derives one from the history window (density-aware, anchored at the
// snapshot head — not the wall clock — so range reviews reproduce).
func advisorySigma(
	commits []evocoupling.TimedPackageSet,
	since time.Duration,
	end time.Time,
) (time.Duration, error) {
	if manual := viper.GetString("advisory.coupling.sigma"); manual != "" {
		sigma, err := parseSinceDuration(manual)
		if err != nil {
			return 0, fmt.Errorf("parsing advisory.coupling.sigma: %w", err)
		}

		return sigma, nil
	}

	result := evocoupling.DeriveSigma(commitTimes(commits), evocoupling.AutoSigmaOptions{
		WindowStart: end.Add(-since),
		WindowEnd:   end,
	})

	return result.Sigma, nil
}
