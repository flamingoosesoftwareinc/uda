package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/flamingoosesoftwareinc/uda/cmd/ui"
	"github.com/flamingoosesoftwareinc/uda/internal/analysisfs"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/golang"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/python"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/rust"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/swift"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/typescript"
	"github.com/flamingoosesoftwareinc/uda/internal/detect"
	"github.com/flamingoosesoftwareinc/uda/internal/git"
	"github.com/flamingoosesoftwareinc/uda/internal/hotspot"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

var metricsCmd = &cobra.Command{
	Use:   "metrics [path]",
	Short: "Print coupling and instability metrics per package",
	Long: `Analyze packages and print coupling and instability metrics.

Inward coupling is the number of internal packages that depend on a given package.
Outward coupling is the number of packages a given package depends on.
Instability = Outward / (Inward + Outward), ranging from 0 (stable) to 1 (unstable).

Examples:
  uda metrics
  uda metrics ./src`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		path := "."
		if len(args) == 1 {
			path = args[0]
		}

		// Strip Go-style recursive pattern suffix (e.g., ./... -> .)
		path = strings.TrimSuffix(path, "/...")
		path = strings.TrimSuffix(path, "...")
		if path == "" {
			path = "."
		}

		// If --git-root is set, find the repository root from the given path
		if viper.GetBool("metrics.git-root") {
			repo, err := git.OpenRepository(path)
			if err != nil {
				return fmt.Errorf("finding git root: %w", err)
			}
			path = repo.RootPath()
		}

		language, _ := cmd.Flags().GetString("language")
		boundary, _ := cmd.Flags().GetString("boundary")

		dirFS := analysisfs.New(path, language)

		analyzers, err := selectAnalyzers(ctx, dirFS, language, boundary)
		if err != nil {
			return err
		}

		sortStr, _ := cmd.Flags().GetString("sort")
		sortCriteria, err := ui.ParseSort(sortStr)
		if err != nil {
			return err
		}

		var filter *regexp.Regexp
		filterStr, _ := cmd.Flags().GetString("filter")
		if filterStr != "" {
			filter, err = regexp.Compile(filterStr)
			if err != nil {
				return fmt.Errorf("invalid filter regex: %w", err)
			}
		}

		format, _ := cmd.Flags().GetString("format")

		// Parse hotspot options.
		hotspotRange, _ := cmd.Flags().GetString("hotspots")
		curveStr, _ := cmd.Flags().GetString("curve")

		// Interactive mode launches the TUI immediately with a loading screen;
		// analyzers run in the background within the bubbletea event loop.
		if format == FormatInteractive {
			if !term.IsTerminal(int(os.Stdout.Fd())) {
				return errors.New(
					"interactive format requires a terminal; use --format table or --format json",
				)
			}
			absPath, err := filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("resolving absolute path: %w", err)
			}
			var enricher ui.PostAnalysisFunc
			if hotspotRange != "" {
				enricher = func(groups []ui.LanguageMetrics, as []analyzer.Analyzer) error {
					return computeHotspots(groups, as, absPath, hotspotRange, curveStr)
				}
			}

			return ui.RunInteractiveWithLoading(
				ctx,
				analyzers,
				dirFS,
				sortCriteria,
				filter,
				absPath,
				enricher,
			)
		}

		// For json/table, run analysis synchronously.
		var groups []ui.LanguageMetrics
		for _, langAnalyzer := range analyzers {
			metrics, err := langAnalyzer.Analyze(ctx, dirFS)
			if err != nil {
				return err
			}
			groups = append(groups, ui.LanguageMetrics{
				Language: langAnalyzer.Name(),
				Metrics:  metrics,
			})
		}

		// Compute hotspots if requested.
		if hotspotRange != "" {
			if err := computeHotspots(groups, analyzers, path, hotspotRange, curveStr); err != nil {
				return fmt.Errorf("hotspot analysis: %w", err)
			}
		}

		switch format {
		case FormatJSON:
			out, err := ui.MetricsJSON(groups, sortCriteria, filter)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), out)
		case FormatJSONExtended:
			out, err := ui.MetricsJSONExtended(groups, filter)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), out)
		case FormatTable:
			if hotspotRange != "" {
				_, _ = fmt.Fprintln(
					cmd.OutOrStdout(),
					ui.MetricsTableWithHotspots(groups, sortCriteria, filter),
				)
			} else {
				_, _ = fmt.Fprintln(
					cmd.OutOrStdout(),
					ui.MetricsTable(groups, sortCriteria, filter),
				)
			}
		default:
			return fmt.Errorf("unknown format: %s", format)
		}

		return nil
	},
}

func selectAnalyzers(
	ctx context.Context,
	dir fs.FS,
	lang, boundary string,
) ([]analyzer.Analyzer, error) {
	// These build pure option values; the WithBoundaryStrategy closure only
	// runs (and only then warns via context.Background) at analyzer-construction
	// time, where the Option signature carries no caller context.
	//nolint:contextcheck // option builders are pure; the deferred warn has no caller context by design.
	tsOpts := tsOptions(boundary)
	//nolint:contextcheck // see tsOptions above.
	rustOpts := rustOptions(boundary)
	//nolint:contextcheck // see tsOptions above.
	pyOpts := pythonOptions(boundary)

	switch lang {
	case "go":
		return []analyzer.Analyzer{golang.GoAnalyzer()}, nil
	case "python":
		return []analyzer.Analyzer{python.PythonAnalyzer(pyOpts...)}, nil
	case "rust":
		return []analyzer.Analyzer{rust.RustAnalyzer(rustOpts...)}, nil
	case "typescript":
		return []analyzer.Analyzer{typescript.TsAnalyzer(tsOpts...)}, nil
	case "swift":
		return []analyzer.Analyzer{swift.SwiftAnalyzer()}, nil
	case FormatAuto:
		return detectAnalyzers(ctx, dir, tsOpts, rustOpts, pyOpts)
	default:
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}
}

func tsOptions(boundary string) []typescript.Option {
	if boundary == "" {
		return nil
	}

	return []typescript.Option{
		typescript.WithBoundaryStrategy(typescript.BoundaryStrategy(boundary)),
	}
}

func rustOptions(boundary string) []rust.Option {
	if boundary == "" {
		return nil
	}

	return []rust.Option{
		rust.WithBoundaryStrategy(rust.BoundaryStrategy(boundary)),
	}
}

func pythonOptions(boundary string) []python.Option {
	if boundary == "" {
		return nil
	}

	return []python.Option{
		python.WithBoundaryStrategy(python.BoundaryStrategy(boundary)),
	}
}

func detectAnalyzers(
	ctx context.Context,
	dir fs.FS,
	tsOpts []typescript.Option,
	rustOpts []rust.Option,
	pyOpts []python.Option,
) ([]analyzer.Analyzer, error) {
	keys, err := detectLanguageKeys(ctx, dir)
	if err != nil {
		return nil, err
	}

	byKey := map[string]analyzer.Analyzer{
		"go":         golang.GoAnalyzer(),
		"python":     python.PythonAnalyzer(pyOpts...),
		"rust":       rust.RustAnalyzer(rustOpts...),
		"swift":      swift.SwiftAnalyzer(),
		"typescript": typescript.TsAnalyzer(tsOpts...),
	}

	analyzers := make([]analyzer.Analyzer, 0, len(keys))
	for _, key := range keys {
		analyzers = append(analyzers, byKey[key])
	}

	return analyzers, nil
}

// registryLanguageKeys is the stable order of the analyzer registry —
// every language key selectAnalyzers accepts, in deterministic output order.
var registryLanguageKeys = []string{"go", "python", "rust", "swift", "typescript"}

// supportedLanguageKeys filters keys to the analyzer registry, preserving
// registry order. Unknown keys drop out silently — a policy block for an
// unsupported language has never gated anything, and erroring here would
// turn that quiet no-op into a hard failure.
func supportedLanguageKeys(keys []string) []string {
	requested := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		requested[key] = struct{}{}
	}

	supported := make([]string, 0, len(requested))

	for _, key := range registryLanguageKeys {
		if _, ok := requested[key]; ok {
			supported = append(supported, key)
		}
	}

	return supported
}

// detectLanguageKeys returns the internal language keys (go, python, rust,
// swift, typescript) present in dir, in stable registry order. Shared by
// detectAnalyzers and the lint gate, which builds each language's analyzer
// with its own boundary override.
func detectLanguageKeys(ctx context.Context, dir fs.FS) ([]string, error) {
	langs, err := detect.Languages(ctx, dir)
	if err != nil {
		return nil, err
	}

	// Map enry language names to registry keys.
	enryToKey := map[string]string{
		"Go":         "go",
		"Python":     "python",
		"Rust":       "rust",
		"Swift":      "swift",
		"TypeScript": "typescript",
		"TSX":        "typescript",
	}

	detected := make(map[string]struct{})

	for _, lang := range langs {
		if key, ok := enryToKey[lang]; ok {
			detected[key] = struct{}{}
		}
	}

	keys := make([]string, 0, len(detected))

	for _, key := range registryLanguageKeys {
		if _, ok := detected[key]; ok {
			keys = append(keys, key)
		}
	}

	if len(keys) == 0 {
		return nil, errors.New("could not detect project language; use --language flag")
	}

	return keys, nil
}

// computeHotspots enriches groups with hotspot data from git history.
func computeHotspots(
	groups []ui.LanguageMetrics,
	analyzers []analyzer.Analyzer,
	path string,
	rangeStr string,
	curveStr string,
) error {
	repo, err := git.OpenRepository(path)
	if err != nil {
		return fmt.Errorf("opening repository: %w", err)
	}

	commitRange, err := git.ParseCommitRange(repo, rangeStr)
	if err != nil {
		return fmt.Errorf("parsing commit range: %w", err)
	}

	hashes, err := repo.Commits(commitRange)
	if err != nil {
		return fmt.Errorf("getting commits: %w", err)
	}

	commitFiles, err := repo.CommitChangedFiles(hashes)
	if err != nil {
		return fmt.Errorf("getting changed files: %w", err)
	}

	commitDetails, err := repo.CommitDetails(hashes)
	if err != nil {
		return fmt.Errorf("getting commit details: %w", err)
	}

	curve := hotspot.DefaultCurve()
	if curveStr != "" {
		curve, err = parseCurve(curveStr)
		if err != nil {
			return err
		}
	}

	for i := range analyzers {
		if err := computeGroupHotspots(
			&groups[i], analyzers[i], path, commitFiles, commitDetails, curve,
		); err != nil {
			return err
		}
	}

	return nil
}

// computeGroupHotspots computes and attaches hotspot data for a single language group.
func computeGroupHotspots(
	group *ui.LanguageMetrics,
	langAnalyzer analyzer.Analyzer,
	path string,
	commitFiles []git.CommitFiles,
	commitDetails []git.CommitDetail,
	curve *hotspot.CatmullRomCurve,
) error {
	provider, ok := langAnalyzer.(analyzer.BoundaryProvider)
	if !ok {
		return nil
	}

	boundaries, err := provider.Boundaries(
		context.Background(),
		analysisfs.New(path, group.Language),
	)
	if err != nil {
		return fmt.Errorf("boundaries for %s: %w", group.Language, err)
	}

	if len(boundaries) == 0 {
		return nil
	}

	dirToPkgs := boundariesToDirMap(boundaries)
	touchCounts, pkgCommits := attributeCommits(dirToPkgs, commitFiles, commitDetails)

	instabilities := make(map[string]float64, len(group.Metrics))
	for _, m := range group.Metrics {
		instabilities[string(m.Package)] = m.Instability()
	}

	changeFreqs := hotspot.ComputeChangeFrequencies(touchCounts, len(commitFiles))
	scores := hotspot.ComputeScores(instabilities, changeFreqs, curve)

	group.Hotspots = &ui.HotspotData{
		Scores:  scores,
		Commits: pkgCommits,
	}

	return nil
}

// boundariesToDirMap maps each directory to the packages whose boundary covers
// it (multiple packages can share a directory, e.g. Go test packages).
func boundariesToDirMap(boundaries []analyzer.PackageBoundary) map[string][]string {
	dirToPkgs := make(map[string][]string)

	for _, boundary := range boundaries {
		for _, dir := range boundary.Dirs {
			dirToPkgs[dir] = append(dirToPkgs[dir], boundary.Name)
		}
	}

	return dirToPkgs
}

// packagesOwningDir returns the packages whose boundary owns dir, considering
// both the nearest ancestor directory with a boundary and dir exactly.
func packagesOwningDir(dirToPkgs map[string][]string, dir string) []string {
	var owners []string

	for d := dir; d != "." && d != ""; d = filepath.Dir(d) {
		if pkgs, ok := dirToPkgs[d]; ok {
			owners = append(owners, pkgs...)

			break
		}
	}

	if pkgs, ok := dirToPkgs[dir]; ok {
		owners = append(owners, pkgs...)
	}

	return owners
}

// touchedPackages returns the set of packages touched by the given changed files.
func touchedPackages(dirToPkgs map[string][]string, files []string) map[string]struct{} {
	touched := make(map[string]struct{})

	for _, file := range files {
		for _, pkg := range packagesOwningDir(dirToPkgs, filepath.Dir(file)) {
			touched[pkg] = struct{}{}
		}
	}

	return touched
}

// relevantFileChanges returns the deduplicated file changes in files that belong to pkg.
func relevantFileChanges(
	dirToPkgs map[string][]string,
	files []git.FileChange,
	pkg string,
) []ui.FileChangeStat {
	var relevant []ui.FileChangeStat

	seen := make(map[string]struct{})

	for _, file := range files {
		if !slices.Contains(packagesOwningDir(dirToPkgs, filepath.Dir(file.Path)), pkg) {
			continue
		}

		if _, ok := seen[file.Path]; ok {
			continue
		}

		seen[file.Path] = struct{}{}
		relevant = append(relevant, ui.FileChangeStat{
			Path:      file.Path,
			Additions: file.Additions,
			Deletions: file.Deletions,
		})
	}

	return relevant
}

// attributeCommits counts package touches and records per-package commit
// details across the commit range.
func attributeCommits(
	dirToPkgs map[string][]string,
	commitFiles []git.CommitFiles,
	commitDetails []git.CommitDetail,
) (map[string]int, map[string][]ui.CommitTouchInfo) {
	touchCounts := make(map[string]int)
	pkgCommits := make(map[string][]ui.CommitTouchInfo)

	for commitIdx, commit := range commitFiles {
		for pkg := range touchedPackages(dirToPkgs, commit.Files) {
			touchCounts[pkg]++

			if commitIdx >= len(commitDetails) {
				continue
			}

			detail := commitDetails[commitIdx]
			pkgCommits[pkg] = append(pkgCommits[pkg], ui.CommitTouchInfo{
				SHA:       detail.SHA,
				Message:   detail.Message,
				Timestamp: detail.Timestamp,
				Files:     relevantFileChanges(dirToPkgs, detail.Files, pkg),
			})
		}
	}

	return touchCounts, pkgCommits
}

// parseCurve parses a curve string like "0:0.5,0.5:1,1:0.5" into control points.
func parseCurve(s string) (*hotspot.CatmullRomCurve, error) {
	parts := strings.Split(s, ",")

	points := make([]hotspot.ControlPoint, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)

		const xyParts = 2

		coords := strings.SplitN(part, ":", xyParts)
		if len(coords) != xyParts {
			return nil, fmt.Errorf("invalid curve point %q: expected x:y", part)
		}

		var xValue, yValue float64
		if _, err := fmt.Sscanf(coords[0], "%f", &xValue); err != nil {
			return nil, fmt.Errorf("invalid curve X value %q: %w", coords[0], err)
		}

		if _, err := fmt.Sscanf(coords[1], "%f", &yValue); err != nil {
			return nil, fmt.Errorf("invalid curve Y value %q: %w", coords[1], err)
		}

		points = append(points, hotspot.ControlPoint{X: xValue, Y: yValue})
	}

	return hotspot.NewCatmullRomCurve(points)
}

//nolint:gochecknoinits // cobra subcommand flag registration; idiomatic in this codebase per stack.md.
func init() {
	metricsCmd.Flags().
		String("language", "auto", "language to analyze (auto, go, python, rust, swift, typescript)")
	metricsCmd.Flags().
		String("sort", "", "sort order (e.g. package:asc,instability:desc); default: instability:desc,outward:desc,inward:desc")
	metricsCmd.Flags().
		String("filter", "", "regex to filter packages by name")
	metricsCmd.Flags().
		Bool("git-root", false, "analyze from git repository root instead of the given path")
	metricsCmd.Flags().
		String("boundary", "package", "boundary detection strategy (TypeScript: package, barrel, directory; Rust: package, module; Python: module, package, subpackage)")
	metricsCmd.Flags().
		String("hotspots", "", "commit range for hotspot analysis (e.g. HEAD~10..HEAD)")
	metricsCmd.Flags().
		String("curve", "", "custom curve control points (e.g. 0:0.5,0.5:1,1:0.5)")
	_ = viper.BindPFlag("metrics.git-root", metricsCmd.Flags().Lookup("git-root"))
	rootCmd.AddCommand(metricsCmd)
}
