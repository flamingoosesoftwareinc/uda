// Package typescript implements the TypeScript source analyzer (import + member + identifier tracking).
package typescript

import (
	"context"
	"io/fs"
	"log/slog"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/typescript/internal/barrel"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/typescript/internal/directory"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/typescript/internal/packagejson"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/typescript/internal/tscore"
	"github.com/flamingoosesoftwareinc/uda/internal/logschema"
)

// BoundaryStrategy selects the granularity at which TypeScript packages are grouped.
type BoundaryStrategy string

// Boundary strategies select the granularity at which packages are grouped.
const (
	StrategyPackage   BoundaryStrategy = "package"
	StrategyBarrel    BoundaryStrategy = "barrel"
	StrategyDirectory BoundaryStrategy = "directory"
)

// Option configures a tsAnalyzer.
type Option func(*tsAnalyzer)

// WithBoundaryStrategy sets the package-boundary strategy for the analyzer.
func WithBoundaryStrategy(s BoundaryStrategy) Option {
	return func(a *tsAnalyzer) {
		switch s {
		case StrategyPackage, StrategyBarrel, StrategyDirectory:
			a.strategy = s
		default:
			warnUnknownStrategy(string(s))
		}
	}
}

// warnUnknownStrategy logs the unknown-strategy fallback. Called from the
// Option closure at constructor time before any caller context exists.
//

func warnUnknownStrategy(s string) {
	slog.LogAttrs(
		context.Background(),
		slog.LevelWarn,
		"unknown boundary strategy, falling back to default",
		logschema.UdaAnalyzerStrategy(s),
	)
}

type tsAnalyzer struct {
	strategy   BoundaryStrategy
	boundaries []analyzer.PackageBoundary
}

var (
	_ analyzer.Analyzer         = &tsAnalyzer{}
	_ analyzer.BoundaryProvider = &tsAnalyzer{}
)

// TsAnalyzer returns a TypeScript source analyzer (satisfies
// analyzer.Analyzer and analyzer.BoundaryProvider). Concrete pointer return
// so tests can call Boundaries without a type assertion; cmd uses the
// interface upcast.
//
//nolint:revive // unexported-return: the concrete type is intentionally package-private; consumers upcast to analyzer.Analyzer / BoundaryProvider.
func TsAnalyzer(opts ...Option) *tsAnalyzer {
	a := &tsAnalyzer{strategy: StrategyPackage}
	for _, opt := range opts {
		opt(a)
	}

	return a
}

func (t *tsAnalyzer) Name() string { return "TypeScript" }

func (t *tsAnalyzer) Analyze(ctx context.Context, dir fs.FS) ([]analyzer.Metrics, error) {
	factory := t.assignerFactory()

	metrics, boundaries, err := tscore.AnalyzeMetrics(ctx, dir, factory)
	if err != nil {
		return nil, err
	}

	t.boundaries = boundaries

	return metrics, nil
}

func (t *tsAnalyzer) Boundaries(
	ctx context.Context,
	dir fs.FS,
) ([]analyzer.PackageBoundary, error) {
	if t.boundaries == nil {
		if _, err := t.Analyze(ctx, dir); err != nil {
			return nil, err
		}
	}

	return t.boundaries, nil
}

func (t *tsAnalyzer) assignerFactory() tscore.AssignerFactory {
	switch t.strategy {
	case StrategyBarrel:
		return func(tsFiles []string) (tscore.BoundaryAssigner, error) {
			return barrel.NewAssigner(tsFiles)
		}
	case StrategyDirectory:
		return func(_ []string) (tscore.BoundaryAssigner, error) {
			return directory.Assigner{}, nil
		}
	case StrategyPackage:
		return func(_ []string) (tscore.BoundaryAssigner, error) {
			return packagejson.Assigner{}, nil
		}
	default:
		return func(_ []string) (tscore.BoundaryAssigner, error) {
			return packagejson.Assigner{}, nil
		}
	}
}
