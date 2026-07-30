// Package analyzer is the language-agnostic dependency-graph + metrics API surface.
package analyzer

import (
	"cmp"
	"context"
	"io/fs"
	"log/slog"
	"slices"
)

// Package identifies an analyzed package by its canonical name.
type Package string

// Import is semantically identical to package except in how it is used
// re-typed here to explicitly communicate the relationship in PackageImports.
type Import Package

// PackageImports is expected to contain a mapping of a package and its dependencies
// e.g. {"analyzer":["context","io/fs"]}.
type PackageImports map[Package][]Import

// Metrics is the coupling snapshot for a single package — the inward
// dependents and the outward dependencies, each carrying per-package
// CouplingStats. Marshalled JSON layout:
//
//	{
//	  "github.com/f/uda/internal/analyzer": {
//	    "outward": {
//	      "context":  {"context.Context": 1},
//	      "io/fs":    {"fs.FS": 1}
//	    },
//	    "inward": {
//	      "github.com/f/uda/internal/analyzer/golang": {
//	        "analyzer.Package":        5,
//	        "analyzer.Analyzer":       1,
//	        "analyzer.PackageImports": 2
//	      }
//	    }
//	  }
//	}
type Metrics struct {
	Package Package `json:"package"`
	// The number of packages that depend on this package
	Inward PackageCouplingStats `json:"inward"`
	// The number of other packages this package depends on
	Outward PackageCouplingStats `json:"outward"`
}

// PackageCouplingStats is expected to contain a list of outward or inward dependencies
// Inward example:
//
//	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/golang": {
//	  "analyzer.Package": 3
//	}
//
// Outward example:
//
//	"github.com/flamingoosesoftwareinc/uda/internal/analyzer": {
//	  "context.Context": 3
//	}
type PackageCouplingStats map[Package]CouplingStats

// CouplingStats stores granular dependency statistics
// key is a qualified type or selector expression
// e.g. context.Context: Count: 10
// e.g. io.ReadAll: Count: 10.
type CouplingStats map[string]CouplingStat

// Position records where a coupling reference occurs in source code.
type Position struct {
	File     string `json:"file"`
	Line     uint   `json:"line"`
	ColStart uint   `json:"col_start"`
	ColEnd   uint   `json:"col_end"`
}

// CouplingStat records the occurrence count and source positions for a single coupling.
type CouplingStat struct {
	Count     uint       `json:"count"`
	Positions []Position `json:"positions,omitempty"`
}

// MetricsSummary is the simplified output format matching the table view.
type MetricsSummary struct {
	Package     string  `json:"package"`
	Inward      int     `json:"inward"`
	Outward     int     `json:"outward"`
	Instability float64 `json:"instability"`
}

// LanguageMetricsSummary pairs a language with simplified metrics.
type LanguageMetricsSummary struct {
	Language string           `json:"language"`
	Metrics  []MetricsSummary `json:"metrics"`
}

// InwardCoupling returns the number of packages that depend on this package.
func (m Metrics) InwardCoupling() float64 {
	return float64(len(m.Inward))
}

// OutwardCoupling returns the number of packages this package depends on.
func (m Metrics) OutwardCoupling() float64 {
	return float64(len(m.Outward))
}

// Instability returns the ratio of outward coupling to inward coupling
// It is an indicator of the packages resilience to change.
func (m Metrics) Instability() float64 {
	total := m.InwardCoupling() + m.OutwardCoupling()
	if total == 0 {
		return 0
	}

	return m.OutwardCoupling() / total
}

// Analyzer is expected to walk dir and extract the PackageImports.
type Analyzer interface {
	Name() string
	Analyze(ctx context.Context, dir fs.FS) ([]Metrics, error)
}

// PackageBoundary describes the filesystem scope of a package.
type PackageBoundary struct {
	Name string   `json:"Name"` // Package identifier (matches Package string value)
	Dirs []string `json:"Dirs"` // Directories constituting this package (relative to analysis root)
}

// BoundaryProvider is optionally implemented by analyzers that can report package boundaries.
// Boundaries() lazily invokes Analyze() if boundaries have not been populated yet.
type BoundaryProvider interface {
	Boundaries(ctx context.Context, dir fs.FS) ([]PackageBoundary, error)
}

// PackageAnalysisTree indexes PackageAnalysis nodes by package name.
type PackageAnalysisTree struct {
	pas map[string]*PackageAnalysis
}

// NewPackageAnalysisTree returns an empty PackageAnalysisTree.
func NewPackageAnalysisTree() *PackageAnalysisTree {
	return &PackageAnalysisTree{
		pas: make(map[string]*PackageAnalysis),
	}
}

// Get returns the PackageAnalysis for packageName and whether it exists.
func (p *PackageAnalysisTree) Get(packageName string) (*PackageAnalysis, bool) {
	pa, exists := p.pas[packageName]

	return pa, exists
}

// GetRootNodes returns every PackageAnalysis node in the tree.
func (p *PackageAnalysisTree) GetRootNodes() []*PackageAnalysis {
	rootNodes := make([]*PackageAnalysis, 0, len(p.pas))

	for _, pa := range p.pas {
		rootNodes = append(rootNodes, pa)
	}

	return rootNodes
}

// Add creates and stores a new PackageAnalysis for packageName and returns it.
func (p *PackageAnalysisTree) Add(packageName string) *PackageAnalysis {
	analysis := &PackageAnalysis{
		Package: packageName,
		Out:     newPackageImportInfo(),
		In:      newPackageImportInfo(),
		Aliases: make(map[string]string),
	}

	p.pas[packageName] = analysis

	return analysis
}

// PackageAnalysis holds the inward and outward import information for one package.
type PackageAnalysis struct {
	Package string
	Out     *PackageImportInfo
	In      *PackageImportInfo
	Aliases map[string]string
}

// Key returns the package name identifying this analysis.
func (p *PackageAnalysis) Key() string {
	return p.Package
}

// PackageImportInfo maps imported package names to their ImportInfo.
type PackageImportInfo struct {
	pii map[string]*ImportInfo
}

func newPackageImportInfo() *PackageImportInfo {
	return &PackageImportInfo{
		pii: make(map[string]*ImportInfo),
	}
}

// Get returns the ImportInfo for pkg and whether it exists.
func (pi *PackageImportInfo) Get(pkg string) (*ImportInfo, bool) {
	i, exists := pi.pii[pkg]

	return i, exists
}

// Add returns the ImportInfo for pkg, creating it if absent.
func (pi *PackageImportInfo) Add(pkg string) *ImportInfo {
	i, exists := pi.pii[pkg]
	if !exists {
		i = NewImportInfo(pkg)
		pi.pii[pkg] = i
	}

	return i
}

// GetChildren returns every ImportInfo tracked by this PackageImportInfo.
func (pi *PackageImportInfo) GetChildren() []*ImportInfo {
	children := make([]*ImportInfo, 0, len(pi.pii))
	for _, i := range pi.pii {
		children = append(children, i)
	}

	return children
}

// ImportInfo accumulates per-symbol coupling statistics for one imported package.
type ImportInfo struct {
	Package string
	is      map[string]*importStats
}

// NewImportInfo returns an empty ImportInfo for pkg.
func NewImportInfo(pkg string) *ImportInfo {
	return &ImportInfo{
		Package: pkg,
		is:      make(map[string]*importStats),
	}
}

// Key returns the imported package name.
func (ii *ImportInfo) Key() string {
	return ii.Package
}

// CouplingStats returns the accumulated coupling statistics with positions sorted deterministically.
func (ii *ImportInfo) CouplingStats() CouplingStats {
	stats := make(CouplingStats, len(ii.is))
	for k, v := range ii.is {
		positions := make([]Position, len(v.Positions))
		copy(positions, v.Positions)
		slices.SortFunc(positions, func(left, right Position) int {
			if c := cmp.Compare(left.File, right.File); c != 0 {
				return c
			}

			if c := cmp.Compare(left.Line, right.Line); c != 0 {
				return c
			}

			return cmp.Compare(left.ColStart, right.ColStart)
		})
		stats[k] = CouplingStat{Count: v.Occurrences, Positions: positions}
	}

	return stats
}

// AddWithCount records a coupling for key with an explicit occurrence count and positions.
func (ii *ImportInfo) AddWithCount(key, expr string, count uint, positions []Position) {
	ii.is[key] = &importStats{Expr: expr, Occurrences: count, Positions: positions}
}

// Add records a single coupling occurrence for key at pos.
func (ii *ImportInfo) Add(key, expr string, pos Position) {
	isi, exists := ii.is[key]
	if !exists {
		isi = &importStats{
			Expr:        expr,
			Occurrences: 0,
		}
		ii.is[key] = isi
	}

	isi.Occurrences++
	isi.Positions = append(isi.Positions, pos)
}

type importStats struct {
	Expr        string
	Occurrences uint
	Positions   []Position
}

// ResolveInwardDependencies populates each package's inward coupling from the
// outward edges recorded in depGraph.
func ResolveInwardDependencies(depGraph *PackageAnalysisTree) {
	pkgNodes := depGraph.GetRootNodes()

	for _, pkgNode := range pkgNodes {
		outNode := pkgNode.Out

		imports := outNode.GetChildren()
		for _, imp := range imports {
			importedPkg, exists := depGraph.Get(imp.Key())
			if !exists {
				continue
			}

			inNode := importedPkg.In

			inPkg := inNode.Add(pkgNode.Key())
			for k, v := range imp.CouplingStats() {
				inPkg.AddWithCount(k, k, v.Count, v.Positions)
			}
		}
	}
}

// GetCouplingStats converts a PackageImportInfo into PackageCouplingStats.
func GetCouplingStats(ctx context.Context, pii *PackageImportInfo) PackageCouplingStats {
	if pii == nil {
		slog.DebugContext(ctx, "GetCouplingStats nil node")

		return make(PackageCouplingStats, 0)
	}

	couplings := pii.GetChildren()
	packageCouplingStats := make(PackageCouplingStats, len(couplings))

	slog.DebugContext(ctx, "GetCouplingStats", "couplings", couplings)

	for _, coupling := range couplings {
		packageCouplingStats[Package(coupling.Key())] = coupling.CouplingStats()
	}

	return packageCouplingStats
}

// StringInterner deduplicates strings so that identical strings share
// the same underlying memory. This reduces memory usage when the same
// file path appears in many Position structs.
type StringInterner struct {
	strings map[string]string
}

// NewStringInterner creates a new string interner.
func NewStringInterner() *StringInterner {
	return &StringInterner{strings: make(map[string]string)}
}

// Intern returns a canonical version of the string. If the string has been
// seen before, the previously stored string is returned (sharing memory).
// Otherwise, the string is stored and returned.
func (i *StringInterner) Intern(s string) string {
	if existing, ok := i.strings[s]; ok {
		return existing
	}

	i.strings[s] = s

	return s
}
