package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/flamingoosesoftwareinc/uda/cmd/ui"
	"github.com/flamingoosesoftwareinc/uda/internal/analysisfs"
	"github.com/flamingoosesoftwareinc/uda/internal/evocoupling"
	"github.com/flamingoosesoftwareinc/uda/internal/git"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var couplingCmd = &cobra.Command{
	Use:   "coupling [path]",
	Short: "Detect evolutionary coupling between packages via git history",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "."
		if len(args) == 1 {
			path = args[0]
		}

		repo, err := git.OpenRepository(path)
		if err != nil {
			return fmt.Errorf("opening repository: %w", err)
		}

		path = repo.RootPath()

		// Parse --since into duration.
		sinceStr, _ := cmd.Flags().GetString("since")

		since, err := parseSinceDuration(sinceStr)
		if err != nil {
			return fmt.Errorf("parsing --since: %w", err)
		}

		// Get commits in time window.
		hashes, err := repo.CommitsSince(since)
		if err != nil {
			return fmt.Errorf("getting commits: %w", err)
		}

		if len(hashes) == 0 {
			fmt.Fprintln(os.Stderr, "no commits found in time window")

			return nil
		}

		// Map commit history onto package boundaries.
		language, _ := cmd.Flags().GetString("language")
		boundary, _ := cmd.Flags().GetString("boundary")

		commits, _, err := timedPackageSets(
			cmd.Context(), analysisfs.New(path, language), repo, hashes, language, boundary)
		if err != nil {
			return err
		}

		if len(commits) == 0 {
			fmt.Fprintln(os.Stderr, "no commits matched any package boundaries")

			return nil
		}

		// Resolve sigma — either user-supplied (manual override) or
		// auto-derived from a precision target. The auto picker also
		// runs in manual mode (with the manual sigma as input) so the
		// JSON output can report the achieved ESS at the chosen sigma.
		sigmaStr, _ := cmd.Flags().GetString("sigma")
		targetPrecision, _ := cmd.Flags().GetFloat64("target-precision")
		coveragePercentile, _ := cmd.Flags().GetInt("ess-coverage-percentile")
		evalPoints, _ := cmd.Flags().GetInt("ess-eval-points")

		selection, err := resolveSigma(sigmaSelectionInput{
			SigmaFlag:          sigmaStr,
			TargetPrecision:    targetPrecision,
			CoveragePercentile: coveragePercentile,
			EvalPoints:         evalPoints,
			Commits:            commits,
			WindowStart:        time.Now().Add(-since),
			WindowEnd:          time.Now(),
		})
		if err != nil {
			return err
		}

		minCorr, _ := cmd.Flags().GetFloat64("min-correlation")
		miEnabled, _ := cmd.Flags().GetBool("mi")
		miMinSupport, _ := cmd.Flags().GetInt("mi-min-support")
		sortBy, _ := cmd.Flags().GetString("sort")

		var filter *regexp.Regexp
		filterStr, _ := cmd.Flags().GetString("filter")
		if filterStr != "" {
			filter, err = regexp.Compile(filterStr)
			if err != nil {
				return fmt.Errorf("invalid filter regex: %w", err)
			}
		}

		sigmaDur := time.Duration(selection.SigmaSeconds * float64(time.Second))

		pairs := evocoupling.Analyze(commits, evocoupling.Options{
			Sigma:        sigmaDur,
			MinCorr:      minCorr,
			MIEnabled:    miEnabled,
			MIMinSupport: miMinSupport,
			WindowStart:  time.Now().Add(-since),
		})

		if miEnabled {
			pairs = ui.SortCouplingPairs(pairs, sortBy)
		}

		report := ui.CouplingReport{
			SigmaSelection: selection,
			Pairs:          pairs,
		}

		if miEnabled {
			report.MISelection = buildMISelection(
				sigmaDur,
				miMinSupport,
				pairs,
				commits,
				time.Now().Add(-since),
			)
		}

		format := viper.GetString("format")

		switch format {
		case FormatJSON:
			out, err := ui.CouplingJSON(report, filter)
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), out)
		case FormatTable, FormatInteractive:
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), ui.CouplingTable(report, filter))
		default:
			return fmt.Errorf("unknown format: %s", format)
		}

		return nil
	},
}

// sigmaSelectionInput aggregates the inputs that drive sigma resolution.
// Kept as a struct so resolveSigma stays under cyclop without functional
// options overhead — only one caller, single CLI command.
type sigmaSelectionInput struct {
	SigmaFlag          string
	TargetPrecision    float64
	CoveragePercentile int
	EvalPoints         int
	Commits            []evocoupling.TimedPackageSet
	WindowStart        time.Time
	WindowEnd          time.Time
}

// resolveSigma chooses the kernel bandwidth for Analyze and builds the
// user-facing SigmaSelection metadata. When sigmaFlag is set, the manual
// value drives Analyze; the auto-derivation fields are still populated as
// reference (so reviewers see what the picker would have chosen).
func resolveSigma(input sigmaSelectionInput) (ui.SigmaSelection, error) {
	events := commitTimes(input.Commits)
	autoResult := evocoupling.DeriveSigma(events, evocoupling.AutoSigmaOptions{
		TargetStddev:       input.TargetPrecision,
		CoveragePercentile: float64(input.CoveragePercentile),
		EvalPoints:         input.EvalPoints,
		WindowStart:        input.WindowStart,
		WindowEnd:          input.WindowEnd,
	})

	target := input.TargetPrecision
	if target <= 0 {
		target = defaultTargetPrecision
	}

	if input.SigmaFlag != "" {
		manual, err := parseSinceDuration(input.SigmaFlag)
		if err != nil {
			return ui.SigmaSelection{}, fmt.Errorf("parsing --sigma: %w", err)
		}

		return ui.SigmaSelection{
			Mode:              ui.SigmaModeManual,
			SigmaSeconds:      manual.Seconds(),
			SigmaHuman:        humanDuration(manual),
			TargetStddev:      target,
			RequiredESS:       autoResult.RequiredESS,
			AchievedESSMedian: autoResult.AchievedESSMedian,
			AchievedESSP25:    autoResult.AchievedESSP25,
			LowConfidence:     false,
		}, nil
	}

	sel := ui.SigmaSelection{
		Mode:              ui.SigmaModeAuto,
		SigmaSeconds:      autoResult.Sigma.Seconds(),
		SigmaHuman:        humanDuration(autoResult.Sigma),
		TargetStddev:      target,
		RequiredESS:       autoResult.RequiredESS,
		AchievedESSMedian: autoResult.AchievedESSMedian,
		AchievedESSP25:    autoResult.AchievedESSP25,
		LowConfidence:     autoResult.LowConfidence,
		Reason:            autoResult.Reason,
	}

	if autoResult.DensityCap > 0 {
		sel.DensityCapSeconds = autoResult.DensityCap.Seconds()
		sel.DensityCapHuman = humanDuration(autoResult.DensityCap)
	}

	return sel, nil
}

func commitTimes(commits []evocoupling.TimedPackageSet) []time.Time {
	times := make([]time.Time, len(commits))
	for i, c := range commits {
		times[i] = c.Time
	}

	return times
}

// humanDuration renders a duration in the d/h shorthand the --sigma flag
// accepts. Picks the largest unit that yields a non-trivial number — keeps
// the displayed string compact (14d, not 336h0m0s).
func humanDuration(d time.Duration) string {
	const (
		hoursPerDay   = 24
		hoursDuration = time.Hour
	)

	if d <= 0 {
		return "0"
	}

	days := int(d / (hoursDuration * hoursPerDay))
	hours := int((d % (hoursDuration * hoursPerDay)) / hoursDuration)

	if days > 0 && hours == 0 {
		return fmt.Sprintf("%dd", days)
	}

	if days > 0 {
		return fmt.Sprintf("%dd%dh", days, hours)
	}

	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}

	return d.String()
}

// durationMinLen is the minimum valid duration string length: 1 digit + 1 unit char.
const durationMinLen = 2

// parseSinceDuration parses "90d", "6m", "1y", "24h" into time.Duration.
func parseSinceDuration(s string) (time.Duration, error) {
	if len(s) < durationMinLen {
		return 0, fmt.Errorf("invalid duration: %s", s)
	}

	unit := s[len(s)-1]
	numStr := s[:len(s)-1]

	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid duration number: %s", s)
	}

	switch unit {
	case 'h':
		return time.Duration(num * float64(time.Hour)), nil
	case 'd':
		return time.Duration(num * 24 * float64(time.Hour)), nil
	case 'w':
		return time.Duration(num * 7 * 24 * float64(time.Hour)), nil
	case 'm':
		return time.Duration(num * 30 * 24 * float64(time.Hour)), nil
	case 'y':
		return time.Duration(num * 365 * 24 * float64(time.Hour)), nil
	default:
		return 0, fmt.Errorf("unknown duration unit '%c' in %s (use h/d/w/m/y)", unit, s)
	}
}

// buildMISelection populates the top-level mi_selection JSON block from
// the resolved sigma and the in-window commit span. The bin count is
// computed by reproducing the BuildPresenceMatrix anchor logic — same
// math, run once for the metadata block. Cheap (single pass over
// commits) and keeps the metadata honest with what Analyze actually saw.
func buildMISelection(
	sigma time.Duration,
	minSupport int,
	pairs []evocoupling.CouplingPair,
	commits []evocoupling.TimedPackageSet,
	windowStart time.Time,
) *ui.MISelection {
	support := minSupport
	if support <= 0 {
		support = defaultMIMinSupport
	}

	nBins := miBinCount(commits, windowStart, sigma)

	return &ui.MISelection{
		Enabled:         true,
		BinWidthSeconds: sigma.Seconds(),
		NBins:           nBins,
		MinSupport:      support,
		BiasCorrection:  ui.MIBiasCorrection,
		LowConfidence:   miLowConfidence(pairs, nBins),
	}
}

// miBinCount mirrors BuildPresenceMatrix's bin enumeration to populate
// the metadata block without re-running the matrix construction. Returns
// 0 when no commits fall in-window — surfacing that the MI pass had no
// signal to operate on rather than reporting a misleading positive bin
// count.
func miBinCount(
	commits []evocoupling.TimedPackageSet,
	windowStart time.Time,
	binWidth time.Duration,
) int {
	if binWidth <= 0 || len(commits) == 0 {
		return 0
	}

	var (
		maxOffset time.Duration
		haveAny   bool
	)

	for _, commit := range commits {
		if commit.Time.Before(windowStart) {
			continue
		}

		offset := commit.Time.Sub(windowStart)
		if !haveAny || offset > maxOffset {
			maxOffset = offset
			haveAny = true
		}
	}

	if !haveAny {
		return 0
	}

	return int(maxOffset/binWidth) + 1
}

// miMinBinsForConfidence matches the internal miMinBins floor in
// evocoupling/mi.go — below 8 bins, NMI is suppressed pair-wise.
// Surfacing low_confidence at the report level lets consumers spot it
// without scanning every pair.
const miMinBinsForConfidence = 8

func miLowConfidence(pairs []evocoupling.CouplingPair, nBins int) bool {
	if nBins < miMinBinsForConfidence {
		return true
	}

	// Spot-check: if every pair has nil NMI, surfacing low_confidence
	// flags that the binned pass didn't produce usable signal even though
	// the bin count looks adequate.
	for _, pair := range pairs {
		if pair.NMI != nil {
			return false
		}
	}

	return true
}

// defaultMinCorrelation is the default Pearson correlation floor for
// reporting co-changed package pairs. Tuned for typical 90-day windows.
const defaultMinCorrelation = 0.1

// defaultMIMinSupport is the v1 floor for n_11 surfaced as the --mi-min-
// support default. Matches the design doc — three co-changes is the
// "this isn't an accident" threshold.
const defaultMIMinSupport = 3

// defaultTargetPrecision is the auto-picker's default correlation-stddev
// target — two decimal places of stability. Calibration is open; revisit
// against real repos before promoting to a stable knob.
const defaultTargetPrecision = 0.05

// defaultCoveragePercentile is the auto-picker's default ESS percentile
// across the evaluation grid. 50 (median) = "typical density gets target
// precision"; 25 is stricter, gates against sparse regions.
const defaultCoveragePercentile = 50

// defaultEvalPoints is the auto-picker's default evaluation-grid size.
// 20 points span the analysis window densely enough for the percentile to
// be stable without driving binary-search cost into noticeable territory.
const defaultEvalPoints = 20

//nolint:gochecknoinits // cobra subcommand flag registration; idiomatic in this codebase per stack.md.
func init() {
	couplingCmd.Flags().
		String("since", "90d", "how far back to analyze (e.g. 90d, 6m, 1y)")
	couplingCmd.Flags().
		String("sigma", "", "kernel bandwidth manual override (e.g. 7d, 14d, 30d); default is auto-derived from --target-precision")
	couplingCmd.Flags().
		Float64("target-precision", defaultTargetPrecision,
			"target correlation standard deviation for auto sigma (smaller = wider sigma)")
	couplingCmd.Flags().
		Int("ess-coverage-percentile", defaultCoveragePercentile, "")
	couplingCmd.Flags().
		Int("ess-eval-points", defaultEvalPoints, "")
	couplingCmd.Flags().
		Float64("min-correlation", defaultMinCorrelation, "minimum correlation to report")
	couplingCmd.Flags().
		Bool("mi", true, "compute binned mutual information (NMI + divergence) alongside kernel-weighted Pearson; --mi=false to disable")
	couplingCmd.Flags().
		Int("mi-min-support", defaultMIMinSupport,
			"co-occurrence floor for MI pairs (n_11); below this LowSupport=true")
	couplingCmd.Flags().
		String("sort", ui.SortByCorrelation,
			"sort key: correlation | divergence | nmi")
	couplingCmd.Flags().
		String("language", "auto", "language to analyze (auto, go, python, rust, swift, typescript)")
	couplingCmd.Flags().
		String("filter", "", "regex to filter packages by name")

	// Advanced ESS knobs are hidden from --help; users discover them via
	// the design doc rather than the basic flag listing.
	_ = couplingCmd.Flags().MarkHidden("ess-coverage-percentile")
	_ = couplingCmd.Flags().MarkHidden("ess-eval-points")

	rootCmd.AddCommand(couplingCmd)
}
