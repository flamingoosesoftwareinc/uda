package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/flamingoosesoftwareinc/uda/cmd/ui"
	"github.com/flamingoosesoftwareinc/uda/internal/analysisfs"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/git"
	"github.com/flamingoosesoftwareinc/uda/internal/lint"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// defaultLintExcludes filters graph noise no policy should have to
// enumerate: test packages and test fixture trees.
var defaultLintExcludes = []string{"**/testdata/**", "testdata/**"}

const shortShaLen = 7

// defaultLintBoundary is the package-boundary granularity used when a
// language block declares no override.
const defaultLintBoundary = "package"

var lintCmd = &cobra.Command{
	Use:   "lint [path]",
	Short: "Enforce the coupling policy declared in .uda.yaml",
	Long: `Compare the repository's current dependency graph against the lockfile-style
policy in the repo-local .uda.yaml lint section.

Every internal edge must be listed: a new dependency fails the gate until a
human moves it from pending to allowed ("uda lint accept" stages it,
"uda lint approve" admits it). Forbid rules can never be accepted. Edges to
the standard library and third-party modules are out of scope — only
package-to-package edges inside the repository are policed.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root := lintRoot(args)

		cfg, err := lint.LoadConfig(lintConfigPath(root))
		if err != nil {
			return lintConfigErr(err)
		}

		report, err := lintReport(cmd.Context(), root, cfg)
		if err != nil {
			return err
		}

		if err := renderLint(cmd, report); err != nil {
			return err
		}

		if total := report.Total(); total > 0 {
			return fmt.Errorf("lint: %d violation(s)", total)
		}

		return nil
	},
}

var lintInitCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Snapshot the current dependency graph into the allowed list",
	Long: `Bootstrap the lint policy: write every internal edge the analyzers see into
the allowed list of the repo-local .uda.yaml, one block per detected
language. Adoption starts green; roles and forbid rules are yours to author
on top.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root := lintRoot(args)
		configPath := lintConfigPath(root)

		// Init respects excludes and boundary overrides already declared
		// (re-running after adding e.g. `boundary: module` to a language
		// block regenerates the lockfile at that granularity).
		var (
			exclude    []string
			boundaries map[string]string
		)

		if cfg, err := lint.LoadConfig(configPath); err == nil {
			exclude = cfg.Exclude
			boundaries = lintBoundaries(cfg)
		}

		graphs, err := lintGraphs(cmd.Context(), root, nil, exclude, boundaries)
		if err != nil {
			return err
		}

		for _, language := range sortedKeys(graphs) {
			rules := lint.Init(graphs[language])
			if err := lint.WriteRules(configPath, language, rules); err != nil {
				return err
			}

			cmd.Printf("%s: %d edge(s) allowed\n", language, len(rules.Allowed))
		}

		cmd.Printf("Wrote %s\n", configPath)

		return nil
	},
}

var lintAcceptCmd = &cobra.Command{
	Use:   "accept [path]",
	Short: "Stage unlisted edges as pending (lint stays red until approved)",
	Long: `Write every unlisted edge into the pending list of the repo-local .uda.yaml,
attributed with today's date and the current commit. Pending is a
visible-in-diff staging area, not a bypass: lint stays red until
"uda lint approve" moves the edges to allowed. Forbidden edges and new
outbound edges from declared-stable packages are never staged.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root := lintRoot(args)
		configPath := lintConfigPath(root)

		cfg, err := lint.LoadConfig(configPath)
		if err != nil {
			return lintConfigErr(err)
		}

		graphs, err := lintGraphs(
			cmd.Context(), root, sortedKeys(cfg.Languages), cfg.Exclude, lintBoundaries(cfg))
		if err != nil {
			return err
		}

		added := time.Now().Format(time.DateOnly)
		commit := headShortSha(root)
		skippedTotal := 0

		for _, language := range sortedKeys(cfg.Languages) {
			violations := lint.Evaluate(graphs[language], cfg.Languages[language])

			rules, skipped := lint.Accept(cfg.Languages[language], violations, added, commit)
			if err := lint.WriteRules(configPath, language, rules); err != nil {
				return err
			}

			cmd.Printf("%s: %d pending\n", language, len(rules.Pending))

			for _, v := range skipped {
				cmd.Printf("%s: refused %s %s (%s)\n", language, v.Kind, v.Edge, v.Rule)
			}

			skippedTotal += len(skipped)
		}

		if skippedTotal > 0 {
			return fmt.Errorf("lint accept: %d edge(s) refused", skippedTotal)
		}

		return nil
	},
}

var lintApproveCmd = &cobra.Command{
	Use:   "approve [path]",
	Short: "Move all pending edges into the allowed list",
	Long: `Admit every staged edge into the lockfile. An edge matching a forbid rule is
rejected even from pending — forbid wins over every path into allowed.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root := lintRoot(args)
		configPath := lintConfigPath(root)

		cfg, err := lint.LoadConfig(configPath)
		if err != nil {
			return lintConfigErr(err)
		}

		rejectedTotal := 0

		for _, language := range sortedKeys(cfg.Languages) {
			rules, rejected := lint.Approve(cfg.Languages[language])
			if err := lint.WriteRules(configPath, language, rules); err != nil {
				return err
			}

			cmd.Printf("%s: %d edge(s) allowed\n", language, len(rules.Allowed))

			for _, v := range rejected {
				cmd.Printf("%s: rejected %s (%s)\n", language, v.Edge, v.Rule)
			}

			rejectedTotal += len(rejected)
		}

		if rejectedTotal > 0 {
			return fmt.Errorf("lint approve: %d forbidden edge(s) rejected", rejectedTotal)
		}

		return nil
	},
}

//nolint:gochecknoinits // cobra subcommand registration; idiomatic in this codebase per stack.md.
func init() {
	lintCmd.AddCommand(lintInitCmd, lintAcceptCmd, lintApproveCmd)
	rootCmd.AddCommand(lintCmd)
}

func lintRoot(args []string) string {
	if len(args) > 0 {
		return args[0]
	}

	return "."
}

func lintConfigPath(root string) string {
	return filepath.Join(root, repoConfigName)
}

func lintConfigErr(err error) error {
	return fmt.Errorf("%w — run \"uda lint init\" to bootstrap the policy", err)
}

// lintReport evaluates every configured language block against the graph.
func lintReport(ctx context.Context, root string, cfg lint.Config) (ui.LintReport, error) {
	graphs, err := lintGraphs(
		ctx,
		root,
		sortedKeys(cfg.Languages),
		cfg.Exclude,
		lintBoundaries(cfg),
	)
	if err != nil {
		return ui.LintReport{}, err
	}

	var report ui.LintReport

	for _, language := range sortedKeys(cfg.Languages) {
		violations := lint.Evaluate(graphs[language], cfg.Languages[language])

		langReport := ui.LintLanguageReport{
			Language:   language,
			Violations: make([]ui.LintViolation, 0, len(violations)),
		}

		for _, v := range violations {
			langReport.Violations = append(langReport.Violations, ui.LintViolation{
				Kind: string(v.Kind),
				Edge: v.Edge.String(),
				Rule: v.Rule,
			})
		}

		report.Languages = append(report.Languages, langReport)
	}

	return report, nil
}

// lintGraphs builds the internal dependency graph per language: package
// names are mapped to repo-relative boundary directories, and only edges
// between two repository packages survive — stdlib and third-party targets
// have no boundary and drop out.
//
// languages selects which analyzers run. Empty means auto-detect (the init
// bootstrap path); non-empty skips detection entirely — the policy already
// declares its languages, and enry detection is a full-tree content scan
// that dwarfs analysis cost on repos with large build/fixture trees.
func lintGraphs(
	ctx context.Context,
	root string,
	languages []string,
	exclude []string,
	boundaries map[string]string,
) (map[string][]lint.Edge, error) {
	dirFS := analysisfs.New(root, "")

	keys := supportedLanguageKeys(languages)

	if len(keys) == 0 {
		detected, err := detectLanguageKeys(ctx, dirFS)
		if err != nil {
			return nil, err
		}

		keys = detected
	}

	graphs := make(map[string][]lint.Edge, len(keys))

	for _, key := range keys {
		boundary := boundaries[key]
		if boundary == "" {
			boundary = defaultLintBoundary
		}

		langFS := analysisfs.New(root, key)

		// selectAnalyzers with a concrete language returns exactly that
		// analyzer, built with the per-language boundary override.
		analyzers, err := selectAnalyzers(ctx, langFS, key, boundary)
		if err != nil {
			return nil, err
		}

		langAnalyzer := analyzers[0]

		metrics, err := langAnalyzer.Analyze(ctx, langFS)
		if err != nil {
			return nil, fmt.Errorf("analyzing %s: %w", key, err)
		}

		provider, ok := langAnalyzer.(analyzer.BoundaryProvider)
		if !ok {
			continue
		}

		pkgBoundaries, err := provider.Boundaries(ctx, dirFS)
		if err != nil {
			return nil, fmt.Errorf("resolving %s boundaries: %w", key, err)
		}

		graphs[key] = languageEdges(metrics, pkgBoundaries, exclude)
	}

	return graphs, nil
}

// lintBoundaries extracts the per-language boundary overrides from the
// config so init/lint/accept build graphs at the configured granularity.
func lintBoundaries(cfg lint.Config) map[string]string {
	boundaries := make(map[string]string, len(cfg.Languages))
	for language, rules := range cfg.Languages {
		if rules.Boundary != "" {
			boundaries[language] = rules.Boundary
		}
	}

	return boundaries
}

func languageEdges(
	metrics []analyzer.Metrics,
	boundaries []analyzer.PackageBoundary,
	exclude []string,
) []lint.Edge {
	names := boundaryNames(boundaries)

	var edges []lint.Edge

	for _, metric := range metrics {
		from, ok := names[string(metric.Package)]
		if !ok || excludedPackage(string(metric.Package), from, exclude) {
			continue
		}

		for target := range metric.Outward {
			to, ok := names[string(target)]
			if !ok || to == from || excludedPackage(string(target), to, exclude) {
				continue
			}

			edges = append(edges, lint.Edge{From: from, To: to})
		}
	}

	return edges
}

// boundaryNames maps each package to its repo-relative name: the shortest
// boundary directory, which reads naturally in a lockfile ("cmd",
// "internal/analyzer") and is language-neutral.
func boundaryNames(boundaries []analyzer.PackageBoundary) map[string]string {
	names := make(map[string]string, len(boundaries))
	dirUsers := make(map[string]int, len(boundaries))

	for _, b := range boundaries {
		// Test packages are excluded from the graph downstream; leaving
		// them out of collision detection keeps a Go pkg + its _test
		// sibling (same directory) from tripping the disambiguation path.
		if strings.HasSuffix(b.Name, testPackageSuffix) {
			continue
		}

		dir := shortestDir(b)
		names[b.Name] = dir
		dirUsers[dir]++
	}

	// A single directory holding several boundaries (e.g. Python module
	// granularity, where pkg.a and pkg.b share pkg/) would collapse to one
	// node and silently drop intra-directory edges. When that happens fall
	// back to the boundary's own name, normalized to slash-separated path
	// form, which is unique by construction.
	for _, b := range boundaries {
		if dirUsers[names[b.Name]] > 1 {
			names[b.Name] = normalizePackageName(b.Name)
		}
	}

	return names
}

// testPackageSuffix marks Go external test packages (import path suffix).
const testPackageSuffix = "_test"

// shortestDir returns the boundary's shortest directory (the readable
// repo-relative form), or the boundary name when it declares no dirs.
func shortestDir(b analyzer.PackageBoundary) string {
	if len(b.Dirs) == 0 {
		return b.Name
	}

	shortest := b.Dirs[0]
	for _, d := range b.Dirs[1:] {
		if len(d) < len(shortest) {
			shortest = d
		}
	}

	return shortest
}

// normalizePackageName rewrites a language's package separators (Python
// ".", Rust "::") to "/" so a module-granularity name reads as a path.
func normalizePackageName(name string) string {
	return strings.NewReplacer("::", "/", ".", "/").Replace(name)
}

func excludedPackage(pkgName, repoRel string, exclude []string) bool {
	if strings.HasSuffix(pkgName, testPackageSuffix) {
		return true
	}

	for _, pattern := range defaultLintExcludes {
		if lint.MatchGlob(pattern, repoRel) {
			return true
		}
	}

	for _, pattern := range exclude {
		if lint.MatchGlob(pattern, repoRel) {
			return true
		}
	}

	return false
}

func renderLint(cmd *cobra.Command, report ui.LintReport) error {
	switch viper.GetString("format") {
	case FormatJSON, FormatJSONExtended:
		out, err := ui.LintJSON(report)
		if err != nil {
			return err
		}

		_, _ = fmt.Fprintln(cmd.OutOrStdout(), out)
	default:
		_, _ = fmt.Fprint(cmd.OutOrStdout(), ui.LintText(report))
	}

	return nil
}

// headShortSha attributes an accept to the commit it was staged on; empty
// when the tree is not a git repository.
func headShortSha(root string) string {
	repo, err := git.OpenRepository(root)
	if err != nil {
		return ""
	}

	hash, err := repo.ResolveCommit("HEAD")
	if err != nil {
		return ""
	}

	return hash.String()[:shortShaLen]
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}
