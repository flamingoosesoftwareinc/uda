package evocoupling

import (
	"math"
	"slices"
	"time"
)

// Reason codes for AutoSigmaResult.Reason. Stable strings — they appear in
// JSON output and downstream consumers may branch on them.
const (
	ReasonOK                  = "ok"
	ReasonInsufficientCommits = "insufficient_commits"
	ReasonSparseWindow        = "sparse_window"
	ReasonDensityFallback     = "density_fallback"
)

// Defaults for AutoSigmaOptions. Calibration is open per the design doc;
// these are the v1 baseline.
const (
	defaultTargetStddev       = 0.05
	defaultCoveragePercentile = 50
	defaultEvalPoints         = 20
	minEvalPoints             = 3
	minPercentile             = 1
	maxPercentile             = 99
	sigmaLowBound             = time.Minute
	binarySearchTolerance     = 0.05
	binarySearchMaxIter       = 40
	sparseWindowDivisor       = 3
	gridMidpointDivisor       = 2
	binarySplitDivisor        = 2
	p25Percentile             = 25

	// Density-aware calibration. fallbackRequiredESS is the K=30 rule of
	// thumb (stddev ≈ 1/√30 ≈ 0.18) — the floor tried when the user's
	// precision target is unreachable, before giving up to window/3.
	// sessionGapFactor: an inter-event gap larger than factor × median gap
	// is a session boundary. minEventsForCap / minSessionsForCap keep the
	// cap from firing on repos too small to have session structure.
	// sessionDurationPercentile picks the cap from the session-duration
	// distribution: p75 rather than the median because package-filtered
	// commit sets fragment bursts into many short sessions — the median
	// lands at minutes (ESS ~3, correlations pure noise) while p75 tracks
	// the substantive work sessions (hours, ESS ~20+ on the agent-cadence
	// repos this calibration was tuned against).
	fallbackRequiredESS       = 30.0
	sessionGapFactor          = 20
	minEventsForCap           = 12
	minSessionsForCap         = 3
	medianPercentile          = 50
	sessionDurationPercentile = 75
)

// AutoSigmaOptions configures DeriveSigma. Zero values are replaced with
// defaults — passing AutoSigmaOptions{WindowStart: ..., WindowEnd: ...} is
// the common case.
type AutoSigmaOptions struct {
	// TargetStddev is the desired correlation-estimator standard deviation.
	// Default 0.05 ("two decimal places of stability"). Drives required
	// ESS via required = 1 / TargetStddev². Smaller stddev → larger sigma.
	TargetStddev float64

	// CoveragePercentile selects which point on the ESS distribution
	// across the evaluation grid must meet required ESS. 50 = median
	// ("typical density"), 25 = stricter coverage ("honest in sparse
	// regions"). Range [1, 99]; default 50.
	CoveragePercentile float64

	// EvalPoints is the number of evaluation centerAt timestamps spread
	// uniformly across [WindowStart, WindowEnd]. Default 20; min 3 so the
	// percentile has at least a start/middle/end to interpolate.
	EvalPoints int

	// WindowStart and WindowEnd bound the analysis window. The binary
	// search picks σ in [1 minute, WindowEnd - WindowStart].
	WindowStart time.Time
	WindowEnd   time.Time
}

// AutoSigmaResult is the picker's output. Sigma is always populated (the
// fallback path returns window/3); LowConfidence flags whether the chosen
// sigma actually achieves the target precision. DensityCap is the
// session-resolution ceiling applied to the sigma search (0 = uncapped —
// the cadence has no session structure).
type AutoSigmaResult struct {
	Sigma             time.Duration
	RequiredESS       float64
	AchievedESSMedian float64
	AchievedESSP25    float64
	LowConfidence     bool
	Reason            string
	DensityCap        time.Duration
}

// DeriveSigma picks the smallest Gaussian-kernel bandwidth that delivers
// the user's target correlation precision across the analysis window.
//
// Algorithm (per the sigma auto-picker design doc, density-aware revision):
//  1. required_ess := 1 / target_stddev²
//  2. Build EvalPoints grid at quantiles of the event times — precision is
//     evaluated where estimates are made, not where the calendar is empty.
//  3. Cap the search's upper bound at the session-resolution ceiling
//     (densityCap): a σ wider than a work session blends distinct sessions
//     into one blob, which on burst-cadence repos turns every pair into a
//     precise estimate of a meaningless quantity.
//  4. Binary search σ in [1 min, min(window, cap)] for required ESS;
//     converge to within 5% relative tolerance.
//  5. If required ESS is unreachable at the upper bound, retry with the
//     ESS floor (30 ≈ stddev 0.18). If even that is unreachable but
//     session structure exists, keep σ at the session ceiling — precision
//     is lost either way, so resolution wins over a precise estimate of a
//     smeared quantity. Both return reason density_fallback. Only cadence
//     with no session structure falls back to σ = window/3 with
//     insufficient_commits. All fallbacks mark LowConfidence.
func DeriveSigma(events []time.Time, opts AutoSigmaOptions) AutoSigmaResult {
	opts = withDefaults(opts)

	window := opts.WindowEnd.Sub(opts.WindowStart)
	requiredESS := 1.0 / (opts.TargetStddev * opts.TargetStddev)

	if window <= sigmaLowBound || len(events) == 0 {
		return fallbackResult(window, requiredESS, ReasonInsufficientCommits)
	}

	sorted := make([]time.Time, len(events))
	copy(sorted, events)
	slices.SortFunc(sorted, time.Time.Compare)

	grid := buildEvalGrid(sorted, opts.EvalPoints)
	densityCap := sessionResolutionCap(sorted)

	sigmaHi := window
	if densityCap > 0 && densityCap < sigmaHi {
		sigmaHi = densityCap
	}

	targets := []struct {
		ess     float64
		reason  string
		lowConf bool
	}{
		{ess: requiredESS, reason: ReasonOK, lowConf: false},
		{ess: fallbackRequiredESS, reason: ReasonDensityFallback, lowConf: true},
	}

	for _, target := range targets {
		if target.ess >= requiredESS && target.reason != ReasonOK {
			continue // the floor is no looser than the user's target
		}

		if percentileESS(sorted, grid, sigmaHi, opts.CoveragePercentile) < target.ess {
			continue
		}

		sigma := binarySearchSigma(
			sorted,
			grid,
			sigmaLowBound,
			sigmaHi,
			target.ess,
			opts.CoveragePercentile,
		)
		summary := summariseAchieved(sorted, grid, sigma)

		return AutoSigmaResult{
			Sigma:             sigma,
			RequiredESS:       requiredESS,
			AchievedESSMedian: summary.Median,
			AchievedESSP25:    summary.P25,
			LowConfidence:     target.lowConf,
			Reason:            target.reason,
			DensityCap:        densityCap,
		}
	}

	sigma := window / sparseWindowDivisor
	reason := ReasonInsufficientCommits

	if densityCap > 0 {
		sigma = sigmaHi
		reason = ReasonDensityFallback
	}

	summary := summariseAchieved(sorted, grid, sigma)

	return AutoSigmaResult{
		Sigma:             sigma,
		RequiredESS:       requiredESS,
		AchievedESSMedian: summary.Median,
		AchievedESSP25:    summary.P25,
		LowConfidence:     true,
		Reason:            reason,
		DensityCap:        densityCap,
	}
}

func withDefaults(opts AutoSigmaOptions) AutoSigmaOptions {
	if opts.TargetStddev <= 0 {
		opts.TargetStddev = defaultTargetStddev
	}

	if opts.CoveragePercentile <= 0 {
		opts.CoveragePercentile = defaultCoveragePercentile
	}

	if opts.CoveragePercentile < minPercentile {
		opts.CoveragePercentile = minPercentile
	}

	if opts.CoveragePercentile > maxPercentile {
		opts.CoveragePercentile = maxPercentile
	}

	if opts.EvalPoints < minEvalPoints {
		if opts.EvalPoints == 0 {
			opts.EvalPoints = defaultEvalPoints
		} else {
			opts.EvalPoints = minEvalPoints
		}
	}

	return opts
}

func fallbackResult(window time.Duration, requiredESS float64, reason string) AutoSigmaResult {
	sigma := max(window/sparseWindowDivisor, sigmaLowBound)

	return AutoSigmaResult{
		Sigma:         sigma,
		RequiredESS:   requiredESS,
		LowConfidence: true,
		Reason:        reason,
	}
}

// buildEvalGrid places n evaluation points at quantiles of the (sorted)
// event times. Correlations are estimated at commit times, so precision is
// measured where the data lives — on burst-cadence repos a wall-clock grid
// would put most points in dead zones and force σ wide to reach the
// nearest burst. For uniform cadence the quantile grid IS the uniform grid.
func buildEvalGrid(sorted []time.Time, n int) []time.Time {
	grid := make([]time.Time, n)

	if n == 1 || len(sorted) == 1 {
		mid := sorted[(len(sorted)-1)/gridMidpointDivisor]
		for i := range grid {
			grid[i] = mid
		}

		return grid
	}

	for i := range n {
		rank := float64(i) / float64(n-1) * float64(len(sorted)-1)
		lower := int(math.Floor(rank))
		upper := int(math.Ceil(rank))
		frac := rank - float64(lower)

		gap := sorted[upper].Sub(sorted[lower])
		grid[i] = sorted[lower].Add(time.Duration(float64(gap) * frac))
	}

	return grid
}

// sessionResolutionCap derives a bandwidth ceiling from the event cadence:
// the p75 duration of a work session, where sessions split on gaps larger
// than sessionGapFactor × the median inter-event gap. Returns 0 (no cap)
// when the cadence is unimodal — too few events, no gap large enough to
// be a session boundary, or too few sessions to trust the split.
func sessionResolutionCap(sorted []time.Time) time.Duration {
	if len(sorted) < minEventsForCap {
		return 0
	}

	gaps := make([]float64, len(sorted)-1)
	for i := 1; i < len(sorted); i++ {
		gaps[i-1] = float64(sorted[i].Sub(sorted[i-1]))
	}

	medianGap := time.Duration(essPercentile(gaps, medianPercentile))
	threshold := max(medianGap, sigmaLowBound) * sessionGapFactor

	var durations []float64

	sessionStart := sorted[0]
	prev := sorted[0]

	for _, t := range sorted[1:] {
		if t.Sub(prev) > threshold {
			durations = append(durations, float64(prev.Sub(sessionStart)))
			sessionStart = t
		}

		prev = t
	}

	durations = append(durations, float64(prev.Sub(sessionStart)))

	if len(durations) < minSessionsForCap {
		return 0
	}

	ceiling := time.Duration(essPercentile(durations, sessionDurationPercentile))

	return max(ceiling, sigmaLowBound)
}

func percentileESS(events, grid []time.Time, sigma time.Duration, percentile float64) float64 {
	values := make([]float64, len(grid))
	for i, center := range grid {
		values[i] = EffectiveSampleSize(events, center, sigma)
	}

	return essPercentile(values, percentile)
}

// achievedSummary captures the ESS distribution across the eval grid at a
// chosen sigma — the user-facing diagnostics for whether the picker hit
// its precision target across typical and sparser regions.
type achievedSummary struct {
	Median float64
	P25    float64
}

func summariseAchieved(events, grid []time.Time, sigma time.Duration) achievedSummary {
	values := make([]float64, len(grid))
	for i, center := range grid {
		values[i] = EffectiveSampleSize(events, center, sigma)
	}

	return achievedSummary{
		Median: essPercentile(values, defaultCoveragePercentile),
		P25:    essPercentile(values, p25Percentile),
	}
}

// binarySearchSigma finds the smallest σ in [low, high] such that the
// configured-percentile ESS meets requiredESS. Converges to within
// binarySearchTolerance relative to the upper bound (so subsequent
// invocations on similar inputs land on similar sigmas).
//
// Precondition: percentileESS(events, grid, high, percentile) >= requiredESS.
// The caller verifies this before invoking — DeriveSigma's underpowered
// check covers the only place this function is reachable.
func binarySearchSigma(
	events, grid []time.Time,
	low, high time.Duration,
	requiredESS float64,
	percentile float64,
) time.Duration {
	for range binarySearchMaxIter {
		if float64(high-low)/float64(high) < binarySearchTolerance {
			break
		}

		mid := low + (high-low)/binarySplitDivisor

		got := percentileESS(events, grid, mid, percentile)
		if got >= requiredESS {
			high = mid
		} else {
			low = mid
		}
	}

	return high
}

// percentileMax is the upper bound of valid percentile inputs (full-scale,
// inclusive). Below percentileMin clamps to the minimum sample; at or above
// percentileMax clamps to the maximum.
const (
	percentileMin = 0
	percentileMax = 100
)

// essPercentile returns the linear-interpolation percentile of values.
// percentile is on a 0–100 scale. values is copied internally before
// sorting so the caller's slice is not mutated.
func essPercentile(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)

	// Simple insertion sort — grids are O(20) elements; allocating a
	// closure for sort.Slice costs more than the comparison budget.
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j-1] > sorted[j]; j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}

	if percentile <= percentileMin {
		return sorted[0]
	}

	if percentile >= percentileMax {
		return sorted[len(sorted)-1]
	}

	rank := (percentile / percentileMax) * float64(len(sorted)-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))

	if lower == upper {
		return sorted[lower]
	}

	frac := rank - float64(lower)

	return sorted[lower]*(1-frac) + sorted[upper]*frac
}
