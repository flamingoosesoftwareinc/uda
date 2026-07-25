package cmd

import (
	"sort"
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/lint"
	"github.com/stretchr/testify/require"
)

// TestLintGraphs pins the graph-building contract on a fixture module:
// package names collapse to repo-relative boundary dirs, stdlib targets
// drop out (no boundary), test packages are excluded, and edges are the
// internal package-to-package couplings only.
func TestLintGraphs(t *testing.T) {
	t.Parallel()

	graphs, err := lintGraphs(t.Context(), ".testdata/lintfixture", nil, nil, nil)
	require.NoError(t, err)

	edges := make([]string, 0, len(graphs["go"]))
	for _, e := range graphs["go"] {
		edges = append(edges, e.String())
	}

	sort.Strings(edges)

	require.Equal(t, []string{
		"adapter -> domain",
		"app -> adapter",
		"app -> domain",
	}, edges)
}

// TestLintGraphs_exclude verifies config excludes remove packages from
// both ends of the graph.
func TestLintGraphs_exclude(t *testing.T) {
	t.Parallel()

	graphs, err := lintGraphs(t.Context(), ".testdata/lintfixture", nil, []string{"adapter"}, nil)
	require.NoError(t, err)

	edges := make([]string, 0, len(graphs["go"]))
	for _, e := range graphs["go"] {
		edges = append(edges, e.String())
	}

	require.Equal(t, []string{"app -> domain"}, edges)
}

// TestLintGraphs_configuredLanguages pins the detection bypass: when the
// policy declares its languages, only those analyzers run — the same edges
// come back for a declared language, other languages never enter the graph
// map, and unsupported keys drop out silently instead of failing the gate.
func TestLintGraphs_configuredLanguages(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		languages []string
		wantKeys  []string
		wantEdges []string
	}{
		"declared language matches detection": {
			languages: []string{"go"},
			wantKeys:  []string{"go"},
			wantEdges: []string{"adapter -> domain", "app -> adapter", "app -> domain"},
		},
		"undeclared language is not analyzed": {
			languages: []string{"rust"},
			wantKeys:  []string{"rust"},
			wantEdges: []string{},
		},
		"unsupported key drops out silently": {
			languages: []string{"kotlin", "go"},
			wantKeys:  []string{"go"},
			wantEdges: []string{"adapter -> domain", "app -> adapter", "app -> domain"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			graphs, err := lintGraphs(t.Context(), ".testdata/lintfixture", tc.languages, nil, nil)
			require.NoError(t, err)

			keys := make([]string, 0, len(graphs))
			for key := range graphs {
				keys = append(keys, key)
			}

			require.ElementsMatch(t, tc.wantKeys, keys)

			edges := make([]string, 0, len(tc.wantEdges))

			for _, key := range tc.wantKeys {
				for _, e := range graphs[key] {
					edges = append(edges, e.String())
				}
			}

			sort.Strings(edges)
			require.Equal(t, tc.wantEdges, edges)
		})
	}
}

// TestLintGraphs_evaluateRoundTrip is the adoption contract: a lockfile
// initialized from the current graph lints green.
func TestLintGraphs_evaluateRoundTrip(t *testing.T) {
	t.Parallel()

	graphs, err := lintGraphs(t.Context(), ".testdata/lintfixture", nil, nil, nil)
	require.NoError(t, err)

	rules := lint.Init(graphs["go"])
	require.Empty(t, lint.Evaluate(graphs["go"], rules))
}

// TestLintGraphs_boundaryOverride proves the per-language boundary
// override changes graph granularity: pkg.a imports pkg.b, which is a
// self-edge (dropped) at package granularity but a real edge at module
// granularity — the exact mapforge python case.
func TestLintGraphs_boundaryOverride(t *testing.T) {
	t.Parallel()

	edgesFor := func(t *testing.T, boundaries map[string]string) []string {
		t.Helper()

		graphs, err := lintGraphs(t.Context(), ".testdata/pyboundary", nil, nil, boundaries)
		require.NoError(t, err)

		edges := make([]string, 0, len(graphs["python"]))
		for _, e := range graphs["python"] {
			edges = append(edges, e.String())
		}

		return edges
	}

	// Default (package) collapses pkg.a and pkg.b into one node.
	require.Empty(t, edgesFor(t, nil))

	// module granularity keeps them distinct.
	require.Equal(t, []string{"pkg/a -> pkg/b"}, edgesFor(t, map[string]string{"python": "module"}))
}

// TestLintGraphs_rustHyphenatedCrates pins Rust name resolution across the
// lint graph: Cargo package names ("iso-model") are imported under Cargo's
// normalized crate name ("iso_model") or an explicit [lib] name ("renamed"),
// and every internal edge must survive at both granularities. External
// dependencies (serde) never appear.
func TestLintGraphs_rustHyphenatedCrates(t *testing.T) {
	t.Parallel()

	edgesFor := func(t *testing.T, boundaries map[string]string) []string {
		t.Helper()

		graphs, err := lintGraphs(t.Context(), ".testdata/rustboundary", nil, nil, boundaries)
		require.NoError(t, err)

		edges := make([]string, 0, len(graphs["rust"]))
		for _, e := range graphs["rust"] {
			edges = append(edges, e.String())
		}

		sort.Strings(edges)

		return edges
	}

	require.Equal(t, []string{
		"iso-cli/src -> casesplit/src",
		"iso-cli/src -> iso-model/src",
		"iso-cli/src -> renamed-lib/src",
		"iso-model/src -> casesplit/src",
	}, edgesFor(t, nil))

	require.Equal(t, []string{
		"iso-cli/src -> casesplit/tokens",
		"iso-cli/src -> iso-model/model",
		"iso-cli/src -> renamed-lib/report",
		"iso-model/model -> casesplit/tokens",
	}, edgesFor(t, map[string]string{"rust": "module"}))
}

func TestLintBoundaries(t *testing.T) {
	t.Parallel()

	cfg := lint.Config{
		Languages: map[string]lint.Rules{
			"python":     {Boundary: "module"},
			"go":         {}, // no override
			"typescript": {Boundary: "barrel"},
		},
	}

	require.Equal(t, map[string]string{
		"python":     "module",
		"typescript": "barrel",
	}, lintBoundaries(cfg))
}
