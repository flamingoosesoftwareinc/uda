// Package diff computes package/dependency/symbol/position diffs between two analyzer snapshots.
package diff

import (
	"slices"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
)

// Type indicates whether an element was added, removed, or unchanged.
type Type int

// Diff kinds classify each element relative to the previous snapshot.
const (
	Unchanged Type = iota
	Added
	Removed
)

// PackageDiff represents the diff for a single package between two snapshots.
type PackageDiff struct {
	Package     string           `json:"Package"`
	DiffType    Type             `json:"DiffType"` // Added/Removed/Unchanged at the package level
	InwardDiff  []DependencyDiff `json:"InwardDiff"`
	OutwardDiff []DependencyDiff `json:"OutwardDiff"`
}

// DependencyDiff represents a dependency edge diff between two packages.
type DependencyDiff struct {
	Package  string       `json:"Package"`
	DiffType Type         `json:"DiffType"`
	Symbols  []SymbolDiff `json:"Symbols"`
}

// SymbolDiff represents a symbol-level diff within a dependency edge.
type SymbolDiff struct {
	Name      string         `json:"Name"`
	DiffType  Type           `json:"DiffType"`
	PrevCount uint           `json:"PrevCount"`
	CurrCount uint           `json:"CurrCount"`
	Positions []PositionDiff `json:"Positions"`
}

// PositionDiff represents a position-level diff.
type PositionDiff struct {
	Position analyzer.Position `json:"Position"`
	DiffType Type              `json:"DiffType"`
}

// AllPackages compares two full snapshots (all packages across all languages)
// and returns diffs for every package that changed.
func AllPackages(prev, curr []analyzer.Metrics) []PackageDiff {
	prevMap := make(map[analyzer.Package]*analyzer.Metrics, len(prev))
	for i := range prev {
		prevMap[prev[i].Package] = &prev[i]
	}

	currMap := make(map[analyzer.Package]*analyzer.Metrics, len(curr))
	for i := range curr {
		currMap[curr[i].Package] = &curr[i]
	}

	allPkgs := make(map[analyzer.Package]bool, len(prev)+len(curr))
	for _, m := range prev {
		allPkgs[m.Package] = true
	}

	for _, m := range curr {
		allPkgs[m.Package] = true
	}

	pkgList := make([]analyzer.Package, 0, len(allPkgs))
	for pkg := range allPkgs {
		pkgList = append(pkgList, pkg)
	}

	slices.Sort(pkgList)

	if len(pkgList) == 0 {
		return nil
	}

	diffs := make([]PackageDiff, 0, len(pkgList))

	for _, pkg := range pkgList {
		d := PackageMetrics(prevMap[pkg], currMap[pkg])
		diffs = append(diffs, *d)
	}

	return diffs
}

// PackageMetrics compares two full Metrics snapshots for a single package.
// Either prev or curr may be nil (indicating the package was added or removed).
func PackageMetrics(prev, curr *analyzer.Metrics) *PackageDiff {
	var (
		pkgName                                          string
		diffType                                         Type
		prevInward, prevOutward, currInward, currOutward analyzer.PackageCouplingStats
	)

	switch {
	case prev == nil && curr == nil:
		return &PackageDiff{}
	case prev == nil:
		pkgName = string(curr.Package)
		diffType = Added
		currInward = curr.Inward
		currOutward = curr.Outward
	case curr == nil:
		pkgName = string(prev.Package)
		diffType = Removed
		prevInward = prev.Inward
		prevOutward = prev.Outward
	default:
		pkgName = string(curr.Package)
		diffType = Unchanged
		prevInward = prev.Inward
		prevOutward = prev.Outward
		currInward = curr.Inward
		currOutward = curr.Outward
	}

	return &PackageDiff{
		Package:     pkgName,
		DiffType:    diffType,
		InwardDiff:  CouplingStats(prevInward, currInward),
		OutwardDiff: CouplingStats(prevOutward, currOutward),
	}
}

// CouplingStats compares two PackageCouplingStats and returns dependency-level diffs.
func CouplingStats(prev, curr analyzer.PackageCouplingStats) []DependencyDiff {
	if prev == nil {
		prev = make(analyzer.PackageCouplingStats)
	}

	if curr == nil {
		curr = make(analyzer.PackageCouplingStats)
	}

	allPkgs := make(map[analyzer.Package]bool)
	for pkg := range prev {
		allPkgs[pkg] = true
	}

	for pkg := range curr {
		allPkgs[pkg] = true
	}

	pkgList := make([]analyzer.Package, 0, len(allPkgs))
	for pkg := range allPkgs {
		pkgList = append(pkgList, pkg)
	}

	slices.Sort(pkgList)

	if len(pkgList) == 0 {
		return nil
	}

	diffs := make([]DependencyDiff, 0, len(pkgList))

	for _, pkg := range pkgList {
		prevStats := prev[pkg]
		currStats := curr[pkg]

		var diffType Type

		switch {
		case prevStats == nil:
			diffType = Added
		case currStats == nil:
			diffType = Removed
		default:
			diffType = Unchanged
		}

		diffs = append(diffs, DependencyDiff{
			Package:  string(pkg),
			DiffType: diffType,
			Symbols:  Symbols(prevStats, currStats),
		})
	}

	return diffs
}

// Symbols compares two CouplingStats and returns symbol-level diffs.
func Symbols(prev, curr analyzer.CouplingStats) []SymbolDiff {
	if prev == nil {
		prev = make(analyzer.CouplingStats)
	}

	if curr == nil {
		curr = make(analyzer.CouplingStats)
	}

	allSyms := make(map[string]struct{})
	for sym := range prev {
		allSyms[sym] = struct{}{}
	}

	for sym := range curr {
		allSyms[sym] = struct{}{}
	}

	symList := make([]string, 0, len(allSyms))
	for sym := range allSyms {
		symList = append(symList, sym)
	}

	slices.Sort(symList)

	if len(symList) == 0 {
		return nil
	}

	diffs := make([]SymbolDiff, 0, len(symList))

	for _, sym := range symList {
		prevStat, hadPrev := prev[sym]
		currStat, hasCurr := curr[sym]

		var symbolDiff SymbolDiff

		symbolDiff.Name = sym

		switch {
		case !hadPrev && hasCurr:
			symbolDiff.DiffType = Added
			symbolDiff.CurrCount = currStat.Count
			symbolDiff.Positions = Positions(nil, currStat.Positions)
		case hadPrev && !hasCurr:
			symbolDiff.DiffType = Removed
			symbolDiff.PrevCount = prevStat.Count
			symbolDiff.Positions = Positions(prevStat.Positions, nil)
		default:
			symbolDiff.DiffType = Unchanged
			symbolDiff.PrevCount = prevStat.Count
			symbolDiff.CurrCount = currStat.Count
			symbolDiff.Positions = Positions(prevStat.Positions, currStat.Positions)
		}

		diffs = append(diffs, symbolDiff)
	}

	return diffs
}

// Positions compares two position slices and returns position-level diffs.
// Positions are matched by file:line (columns are ignored for matching).
func Positions(prev, curr []analyzer.Position) []PositionDiff {
	prevMap := make(map[string]int, len(prev))
	for i, p := range prev {
		prevMap[positionKey(p)] = i
	}

	seen := make(map[string]struct{})

	var diffs []PositionDiff

	// Current positions: added or unchanged
	for _, p := range curr {
		key := positionKey(p)
		seen[key] = struct{}{}

		diffType := Added
		if _, exists := prevMap[key]; exists {
			diffType = Unchanged
		}

		diffs = append(diffs, PositionDiff{
			Position: p,
			DiffType: diffType,
		})
	}

	// Removed positions: only in prev
	for _, p := range prev {
		if _, ok := seen[positionKey(p)]; ok {
			continue
		}

		diffs = append(diffs, PositionDiff{
			Position: p,
			DiffType: Removed,
		})
	}

	return diffs
}

// positionKey creates a unique key for a position (file:line).
// Columns are ignored since they may shift between commits.
func positionKey(p analyzer.Position) string {
	return p.File + ":" + uitoa(p.Line)
}

// uitoa converts a uint to a string without importing strconv.
func uitoa(val uint) string {
	if val == 0 {
		return "0"
	}

	var buf [20]byte

	i := len(buf)
	for val > 0 {
		i--
		buf[i] = byte('0' + val%10)
		val /= 10
	}

	return string(buf[i:])
}
