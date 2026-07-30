// Package evocoupling computes evolutionary coupling between packages via co-change kernel correlation.
package evocoupling

import (
	"math"
	"sort"
	"sync"
	"time"
)

// defaultMIMinSupport is the v1 floor for n_11 below which a pair's MI
// metrics are flagged low-support. Three co-changes is the design doc's
// "this isn't an accident" threshold; raise via Options.MIMinSupport.
const defaultMIMinSupport = 3

// TimedPackageSet represents the packages touched by a single commit.
type TimedPackageSet struct {
	Time     time.Time
	Packages map[string]struct{}
}

// CouplingPair represents the evolutionary coupling between two packages.
//
// Correlation is the kernel-weighted Pearson on continuous event streams
// — the v1 metric, unchanged. The pointer-valued PearsonBinned, NMI, and
// Divergence fields are populated only when Options.MIEnabled is true,
// and are NaN-suppressed: a NaN result (degenerate marginal, < miMinBins
// bins) leaves the pointer nil so the JSON output omits the field rather
// than emitting a non-marshallable NaN. LowSupport flags n_11 below the
// configured minimum.
type CouplingPair struct {
	PackageA      string   `json:"package_a"`
	PackageB      string   `json:"package_b"`
	Correlation   float64  `json:"correlation"`
	PearsonBinned *float64 `json:"pearson_binned,omitempty"`
	NMI           *float64 `json:"nmi,omitempty"`
	Divergence    *float64 `json:"divergence,omitempty"`
	LowSupport    bool     `json:"low_support,omitempty"`
}

// Options configures evolutionary coupling analysis.
type Options struct {
	Sigma   time.Duration
	Kernel  KernelFunc
	MinCorr float64

	// MIEnabled toggles the binned mutual-information metrics. When false
	// (v1 default), Analyze emits only the existing Correlation column
	// and JSON output is byte-stable with pre-MI consumers.
	MIEnabled bool

	// MIMinSupport is the n_11 floor below which a pair is flagged
	// LowSupport. Zero is replaced with defaultMIMinSupport (3) so
	// `Options{MIEnabled: true}` is a sensible call.
	MIMinSupport int

	// WindowStart anchors the presence matrix; commits before it are
	// dropped from the MI computation. Zero value falls back to the
	// earliest commit time in the input — common-case ergonomics.
	WindowStart time.Time
}

// Analyze computes evolutionary coupling between packages from commit history.
//
// The kernel-weighted Pearson pass always runs. When opts.MIEnabled is
// set, an independent binned-MI pass runs in parallel (sigma drives both
// the kernel bandwidth and the bin width — apples-to-apples by design).
// The two passes are merged by package-pair key: a pair surfaced by
// either pass carries both metric families.
func Analyze(commits []TimedPackageSet, opts Options) []CouplingPair {
	kernel := opts.Kernel
	if kernel == nil {
		kernel = Gaussian
	}

	if opts.MIEnabled && opts.MIMinSupport <= 0 {
		opts.MIMinSupport = defaultMIMinSupport
	}

	// Build event lists: package → sorted timestamps.
	events := buildEventLists(commits)

	pkgs := make([]string, 0, len(events))
	for pkg := range events {
		pkgs = append(pkgs, pkg)
	}

	sort.Strings(pkgs)

	var (
		pairs     []CouplingPair
		matrix    *PresenceMatrix
		waitGroup sync.WaitGroup
	)

	waitGroup.Go(func() {
		// Phase 1: auto-correlations (parallelized).
		auto := computeAutoCorrelations(pkgs, events, kernel, opts.Sigma)

		// Phase 2+3: enumerate pairs, compute cross-correlations (parallelized).
		pairs = computePairs(pkgs, events, auto, kernel, opts)
	})

	if opts.MIEnabled {
		waitGroup.Go(func() {
			matrix = BuildPresenceMatrix(commits, miWindowStart(opts, commits), opts.Sigma)
		})
	}

	waitGroup.Wait()

	if opts.MIEnabled {
		pairs = mergeMIMetrics(pairs, matrix, opts.MIMinSupport)
	}

	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Correlation != pairs[j].Correlation {
			return pairs[i].Correlation > pairs[j].Correlation
		}

		return pairs[i].PackageA < pairs[j].PackageA
	})

	return pairs
}

// miWindowStart picks the bin-zero anchor for the presence matrix.
// Explicit Options.WindowStart wins; otherwise fall back to the earliest
// commit time. Falling back avoids a CLI requirement that downstream
// callers haven't been refactored to surface.
func miWindowStart(opts Options, commits []TimedPackageSet) time.Time {
	if !opts.WindowStart.IsZero() {
		return opts.WindowStart
	}

	var earliest time.Time

	for _, commit := range commits {
		if earliest.IsZero() || commit.Time.Before(earliest) {
			earliest = commit.Time
		}
	}

	return earliest
}

// mergeMIMetrics augments existing kernel-Pearson pairs with NMI /
// PearsonBinned / Divergence / LowSupport from the presence matrix, and
// surfaces additional pairs that the binned pass found above the
// min-support floor but the kernel pass filtered out (low MinCorr).
//
// Per the design doc: a pair with n_11 < minSupport is dropped from the
// MI-only output but retained if the kernel pass already surfaced it
// (kernel-Pearson is a separate signal — don't silently lose data).
func mergeMIMetrics(
	pairs []CouplingPair,
	matrix *PresenceMatrix,
	minSupport int,
) []CouplingPair {
	if matrix == nil || len(matrix.Packages) < 2 || len(matrix.Bins) == 0 {
		return pairs
	}

	type key struct{ a, b string }

	byPair := make(map[key]int, len(pairs))
	for i, pair := range pairs {
		byPair[key{pair.PackageA, pair.PackageB}] = i
	}

	for i := range matrix.Packages {
		for j := i + 1; j < len(matrix.Packages); j++ {
			pkgA, pkgB := matrix.Packages[i], matrix.Packages[j]
			seriesA := columnSlice(matrix.Present, i)
			seriesB := columnSlice(matrix.Present, j)
			coOcc := CoOccurrences(seriesA, seriesB)
			lowSupport := coOcc < minSupport

			idx, exists := byPair[key{pkgA, pkgB}]
			if !exists && lowSupport {
				// Pair didn't clear the kernel-Pearson floor and lacks the
				// co-occurrence support to defend a binned-only entry.
				continue
			}

			metrics := computePairMI(seriesA, seriesB)

			if exists {
				applyMIMetrics(&pairs[idx], metrics, lowSupport)

				continue
			}

			pair := CouplingPair{PackageA: pkgA, PackageB: pkgB}
			applyMIMetrics(&pair, metrics, lowSupport)
			pairs = append(pairs, pair)
		}
	}

	return pairs
}

// pairMI bundles the three MI-family scalars for one package pair.
// Each is a sentinel-NaN-when-undefined value; downstream conversion to
// *float64 drops NaN cleanly via floatPtr.
type pairMI struct {
	pearson    float64
	nmi        float64
	divergence float64
}

func computePairMI(seriesA, seriesB []bool) pairMI {
	pearson := PearsonBinned(seriesA, seriesB)
	nmi := NMI(seriesA, seriesB)

	divergence := math.NaN()
	if !math.IsNaN(pearson) && !math.IsNaN(nmi) {
		divergence = nmi - pearson
	}

	return pairMI{pearson: pearson, nmi: nmi, divergence: divergence}
}

func applyMIMetrics(pair *CouplingPair, metrics pairMI, lowSupport bool) {
	pair.PearsonBinned = floatPtr(metrics.pearson)
	pair.NMI = floatPtr(metrics.nmi)
	pair.Divergence = floatPtr(metrics.divergence)
	pair.LowSupport = lowSupport
}

// floatPtr boxes a finite value to a pointer; NaN becomes nil. The pointer
// shape is how CouplingPair distinguishes "MI was not computed for this
// pair" (nil) from "computed as 0.0" — float64 zero-as-omit would lose
// the latter case.
func floatPtr(v float64) *float64 {
	if math.IsNaN(v) {
		return nil
	}

	return &v
}

// columnSlice extracts a single package's presence column from the
// [bin][pkg] matrix as a contiguous slice usable by NMI / PearsonBinned.
func columnSlice(present [][]bool, pkgIdx int) []bool {
	out := make([]bool, len(present))
	for i, row := range present {
		out[i] = row[pkgIdx]
	}

	return out
}

func buildEventLists(commits []TimedPackageSet) map[string][]time.Time {
	events := make(map[string][]time.Time)

	for _, c := range commits {
		for pkg := range c.Packages {
			events[pkg] = append(events[pkg], c.Time)
		}
	}

	for _, times := range events {
		sort.Slice(times, func(i, j int) bool {
			return times[i].Before(times[j])
		})
	}

	return events
}

func computeAutoCorrelations(
	pkgs []string,
	events map[string][]time.Time,
	kernel KernelFunc,
	sigma time.Duration,
) *sync.Map {
	auto := &sync.Map{}

	var waitGroup sync.WaitGroup

	for _, pkg := range pkgs {
		waitGroup.Go(func() {
			val := correlate(events[pkg], events[pkg], kernel, sigma)
			auto.Store(pkg, val)
		})
	}

	waitGroup.Wait()

	return auto
}

func computePairs(
	pkgs []string,
	events map[string][]time.Time,
	auto *sync.Map,
	kernel KernelFunc,
	opts Options,
) []CouplingPair {
	var pairs sync.Map

	var waitGroup sync.WaitGroup

	pairIdx := 0

	for i := range pkgs {
		for j := i + 1; j < len(pkgs); j++ {
			idx := pairIdx
			pairIdx++

			pkgA, pkgB := pkgs[i], pkgs[j]

			waitGroup.Go(func() {
				cross := correlate(events[pkgA], events[pkgB], kernel, opts.Sigma)

				autoA, _ := auto.Load(pkgA)
				autoB, _ := auto.Load(pkgB)

				autoAVal, okA := autoA.(float64)

				autoBVal, okB := autoB.(float64)
				if !okA || !okB {
					return
				}

				denom := math.Sqrt(autoAVal * autoBVal)
				if denom == 0 {
					return
				}

				corr := cross / denom
				if corr < opts.MinCorr {
					return
				}

				pairs.Store(idx, CouplingPair{
					PackageA:    pkgA,
					PackageB:    pkgB,
					Correlation: corr,
				})
			})
		}
	}

	waitGroup.Wait()

	var result []CouplingPair

	pairs.Range(func(_, value any) bool {
		if pair, ok := value.(CouplingPair); ok {
			result = append(result, pair)
		}

		return true
	})

	return result
}

// correlate computes the raw kernel sum between two event lists.
func correlate(timesA, timesB []time.Time, kernel KernelFunc, sigma time.Duration) float64 {
	var sum float64

	for _, tA := range timesA {
		for _, tB := range timesB {
			delta := tA.Sub(tB)
			if delta < 0 {
				delta = -delta
			}

			sum += kernel(delta, sigma)
		}
	}

	return sum
}
