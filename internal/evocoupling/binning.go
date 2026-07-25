package evocoupling

import (
	"sort"
	"time"
)

// PresenceMatrix is a [n_bins][n_packages] binary matrix indicating which
// packages had at least one commit in each fixed-width time bin.
//
// The matrix is binary: multiple commits in a bin collapse to a single
// presence flag. Frequency information is the job of the kernel-weighted
// Pearson pipeline; this representation feeds the mutual-information
// estimator, which only cares whether the event happened.
//
// Shape is preserved for forward-compatibility with conditional mutual
// information (v2 of the MI extension): Bins + Packages exposed as the
// stable axes, Present accessed by [binIdx][pkgIdx] so a third
// conditioning axis can be added without refactor.
type PresenceMatrix struct {
	// Bins lists bin start times in chronological order, length n_bins.
	Bins []time.Time
	// Packages lists package names in lexicographic order, length n_packages.
	Packages []string
	// Present is indexed [binIdx][pkgIdx]; true iff Packages[pkgIdx]
	// appeared in any commit during the bin [Bins[binIdx], Bins[binIdx]+binWidth).
	Present [][]bool
}

// BuildPresenceMatrix bins commits by binWidth into a PresenceMatrix.
//
// windowStart anchors the first bin; subsequent bins start at
// windowStart + k·binWidth. A commit whose Time falls in
// [windowStart + k·binWidth, windowStart + (k+1)·binWidth) marks every
// package it touches as present in bin k. Multiple commits in the same
// bin collapse to a single presence flag.
//
// The number of bins is derived from the commit time span: ceil((maxTime -
// windowStart + 1) / binWidth) bins, capacity for the latest commit
// inclusive. If commits is empty or binWidth is non-positive, returns a
// matrix with no bins and no packages.
//
// Packages absent from every commit are still listed in Packages with all-
// false rows iff they appear in any commit; packages never touched are
// not represented at all. Edge commits at windowStart land in bin 0;
// commits before windowStart are dropped.
func BuildPresenceMatrix(
	commits []TimedPackageSet,
	windowStart time.Time,
	binWidth time.Duration,
) *PresenceMatrix {
	if binWidth <= 0 || len(commits) == 0 {
		return &PresenceMatrix{}
	}

	pkgIdx, pkgs := collectPackages(commits)

	nBins := countBins(commits, windowStart, binWidth)
	if nBins == 0 {
		return &PresenceMatrix{Packages: pkgs}
	}

	bins := make([]time.Time, nBins)
	for k := range nBins {
		bins[k] = windowStart.Add(time.Duration(k) * binWidth)
	}

	present := make([][]bool, nBins)
	for k := range nBins {
		present[k] = make([]bool, len(pkgs))
	}

	for _, commit := range commits {
		if commit.Time.Before(windowStart) {
			continue
		}

		offset := commit.Time.Sub(windowStart)

		binIdx := int(offset / binWidth)
		if binIdx < 0 || binIdx >= nBins {
			continue
		}

		for pkg := range commit.Packages {
			idx, ok := pkgIdx[pkg]
			if !ok {
				continue
			}

			present[binIdx][idx] = true
		}
	}

	return &PresenceMatrix{
		Bins:     bins,
		Packages: pkgs,
		Present:  present,
	}
}

// collectPackages returns the sorted unique package list and a lookup map
// from package name to its index. Packages with no commits in the window
// are excluded — they would only contribute all-false columns.
func collectPackages(commits []TimedPackageSet) (map[string]int, []string) {
	seen := make(map[string]struct{})

	for _, commit := range commits {
		for pkg := range commit.Packages {
			seen[pkg] = struct{}{}
		}
	}

	pkgs := make([]string, 0, len(seen))
	for pkg := range seen {
		pkgs = append(pkgs, pkg)
	}

	sort.Strings(pkgs)

	idx := make(map[string]int, len(pkgs))
	for i, pkg := range pkgs {
		idx[pkg] = i
	}

	return idx, pkgs
}

// countBins returns the number of bins required to span from windowStart
// through the latest in-window commit time, inclusive. Commits before
// windowStart are ignored.
func countBins(commits []TimedPackageSet, windowStart time.Time, binWidth time.Duration) int {
	var maxOffset time.Duration

	haveAny := false

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

	// +1 so the latest commit's bin is included (offset / binWidth is
	// the zero-based bin index).
	return int(maxOffset/binWidth) + 1
}
