package analyzer

import (
	"context"
	"io/fs"
)

type Package string

// Import is semantically identical to package except in how it is used
// re-typed here to explicitly communicate the relationship in PackageImports
type Import Package

// PackageImports is expected to contain a mapping of a package and its dependencies
// e.g. {"analyzer":["context","io/fs"]}
type PackageImports map[Package][]Import

/*
* {
*    "github.com/f/uda/internal/analyzer": {
*      "outward": {
*         "context": {
*           "context.Context": 1,
*         },
*         "io/fs": {
*           "fs.FS": 1
*         }
*      },
*      "inward": {
*        "github.com/f/uda/internal/analyzer/golang": {
*          "analyzer.Package": 5,
*          "analyzer.Analyzer": 1,
*          "analyzer.PackageImports": 2,
*        }
*      }
*    }
* }
*
* */
type Metrics struct {
	Package Package
	// The number of packages that depend on this package
	Inward PackageCouplingStats
	// The number of other packages this package depends on
	Outward PackageCouplingStats
}

// PackageCouplingStats is expected to contain a list of outward or inward dependencies
// Outward example:
//
//	"github.com/flamingoosesoftwareinc/uda/internal/analyzer": {
//	  "analyzer.Package": 3
//	}
//
// Inward example:
//
//	"github.com/flamingoosesoftwareinc/uda/internal/analyzer": {
//	  "context.Context": 3
//	}
type PackageCouplingStats map[Package]CouplingStats

// CouplingStats stores granular dependency statistics
// key is a qualified type or selector expression
// e.g. context.Context: Count: 10
// e.g. io.ReadAll: Count: 10
type CouplingStats map[string]struct {
	Count uint
	// Eventually could have each instance with line number and position and length
}

func (m Metrics) InwardCoupling() float64 {
	inwardCouplingCount := 0

	for _, v := range m.Inward {
		inwardCouplingCount += len(v)
	}

	return float64(inwardCouplingCount)
}

func (m Metrics) OutwardCoupling() float64 {
	outwardCouplingCount := 0

	for _, v := range m.Outward {
		outwardCouplingCount += len(v)
	}

	return float64(outwardCouplingCount)
}

// Instability returns the ratio of outward coupling to inward coupling
// It is an indicator of the packages resilience to change
func (m Metrics) Instability() float64 {
	total := m.InwardCoupling() + m.OutwardCoupling()
	return m.OutwardCoupling() / total
}

// Analyzer is expected to walk dir and extract the PackageImports
type Analyzer interface {
	Analyze(ctx context.Context, dir fs.FS) (PackageImports, error)
	AnalyzeV2(ctx context.Context, dir fs.FS) ([]Metrics, error)
}
