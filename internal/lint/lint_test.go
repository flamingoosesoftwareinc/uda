package lint_test

import (
	"sort"
	"testing"

	"github.com/flamingoosesoftwareinc/rapid"
	"github.com/flamingoosesoftwareinc/uda/internal/lint"
	"github.com/stretchr/testify/require"
)

func TestEvaluate(t *testing.T) {
	t.Parallel()

	edge := func(from, to string) lint.Edge { return lint.Edge{From: from, To: to} }

	tests := map[string]struct {
		edges []lint.Edge
		rules lint.Rules
		want  []lint.Violation
	}{
		"allowed_edge_is_silent": {
			edges: []lint.Edge{edge("cmd", "internal/analyzer")},
			rules: lint.Rules{Allowed: []string{"cmd -> internal/analyzer"}},
			want:  nil,
		},
		"unlisted_edge_fails": {
			edges: []lint.Edge{edge("cmd", "internal/cache")},
			rules: lint.Rules{Allowed: []string{"cmd -> internal/analyzer"}},
			want: []lint.Violation{
				{Kind: lint.KindUnlisted, Edge: edge("cmd", "internal/cache")},
			},
		},
		"forbid_beats_allowed": {
			edges: []lint.Edge{edge("internal/domain", "internal/adapter/http")},
			rules: lint.Rules{
				Allowed: []string{"internal/domain -> internal/adapter/http"},
				Forbid: []lint.ForbidRule{
					{From: "internal/domain", To: "internal/adapter/**"},
				},
			},
			want: []lint.Violation{{
				Kind: lint.KindForbidden,
				Edge: edge("internal/domain", "internal/adapter/http"),
				Rule: "internal/domain -> internal/adapter/**",
			}},
		},
		"pending_edge_stays_red": {
			edges: []lint.Edge{edge("cmd", "internal/cache")},
			rules: lint.Rules{
				Pending: []lint.PendingEdge{{Edge: "cmd -> internal/cache"}},
			},
			want: []lint.Violation{
				{Kind: lint.KindPending, Edge: edge("cmd", "internal/cache")},
			},
		},
		"stable_origin_upgrades_unlisted": {
			edges: []lint.Edge{edge("internal/domain", "internal/cache")},
			rules: lint.Rules{
				Roles: lint.Roles{Stable: []string{"internal/domain"}},
			},
			want: []lint.Violation{{
				Kind: lint.KindStable,
				Edge: edge("internal/domain", "internal/cache"),
				Rule: "internal/domain",
			}},
		},
		"stable_origin_allowed_edge_is_silent": {
			edges: []lint.Edge{edge("internal/domain", "internal/cache")},
			rules: lint.Rules{
				Roles:   lint.Roles{Stable: []string{"internal/domain"}},
				Allowed: []string{"internal/domain -> internal/cache"},
			},
			want: nil,
		},
		"duplicate_edges_report_once": {
			edges: []lint.Edge{
				edge("cmd", "internal/cache"),
				edge("cmd", "internal/cache"),
			},
			rules: lint.Rules{},
			want: []lint.Violation{
				{Kind: lint.KindUnlisted, Edge: edge("cmd", "internal/cache")},
			},
		},
		"severity_orders_output": {
			edges: []lint.Edge{
				edge("cmd", "internal/cache"),
				edge("internal/domain", "internal/adapter/http"),
			},
			rules: lint.Rules{
				Forbid: []lint.ForbidRule{
					{From: "internal/domain", To: "internal/adapter/**"},
				},
			},
			want: []lint.Violation{
				{
					Kind: lint.KindForbidden,
					Edge: edge("internal/domain", "internal/adapter/http"),
					Rule: "internal/domain -> internal/adapter/**",
				},
				{Kind: lint.KindUnlisted, Edge: edge("cmd", "internal/cache")},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, lint.Evaluate(tt.edges, tt.rules))
		})
	}
}

func TestMatchGlob(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		pattern string
		name    string
		want    bool
	}{
		"exact":                     {"internal/domain", "internal/domain", true},
		"exact_mismatch":            {"internal/domain", "internal/cache", false},
		"star_one_segment":          {"internal/*", "internal/domain", true},
		"star_not_across_segments":  {"internal/*", "internal/adapter/http", false},
		"doublestar_many_segments":  {"internal/adapter/**", "internal/adapter/http/v2", true},
		"doublestar_zero_segments":  {"internal/adapter/**", "internal/adapter", true},
		"doublestar_prefix":         {"**/testdata/**", "pkg/testdata/fixture", true},
		"doublestar_requires_match": {"internal/adapter/**", "internal/domain", false},
		"partial_segment_wildcard":  {"internal/ad*", "internal/adapter", true},
		"exact_not_prefix":          {"internal", "internal/domain", false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, lint.MatchGlob(tt.pattern, tt.name))
		})
	}
}

// TestLintStateMachine models the yaml lockfile lifecycle: edges move
// none -> pending -> allowed through init/accept/approve while forbid
// rules dominate every path. The model is three sets; after every command
// the implementation's Evaluate verdict must match the model's.
//
// Kill-claims: forbid checked after allowed (forbid_dominance breaks),
// accept staging forbidden/stable edges (stage guards break), approve
// admitting forbidden pendings (reject path breaks), init leaving stale
// pending behind (reset breaks).
func TestLintStateMachine(t *testing.T) {
	t.Parallel()

	packages := []string{
		"cmd", "internal/domain", "internal/adapter/http", "internal/cache",
	}

	rapid.Check(t, func(t *rapid.T) {
		rules := lint.Rules{
			Roles:  lint.Roles{Stable: []string{"internal/domain"}},
			Forbid: []lint.ForbidRule{{From: "**", To: "internal/adapter/**"}},
		}

		forbidden := func(e lint.Edge) bool {
			return lint.MatchGlob("internal/adapter/**", e.To)
		}
		stable := func(e lint.Edge) bool { return e.From == "internal/domain" }

		var edges []lint.Edge

		model := struct {
			allowed map[lint.Edge]bool
			pending map[lint.Edge]bool
		}{allowed: map[lint.Edge]bool{}, pending: map[lint.Edge]bool{}}

		check := func() {
			got := lint.Evaluate(edges, rules)

			var want []string

			seen := map[lint.Edge]bool{}

			for _, e := range edges {
				if seen[e] {
					continue
				}

				seen[e] = true

				switch {
				case forbidden(e):
					want = append(want, "forbidden "+e.String())
				case model.allowed[e]:
				case model.pending[e]:
					want = append(want, "pending "+e.String())
				case stable(e):
					want = append(want, "stable "+e.String())
				default:
					want = append(want, "unlisted "+e.String())
				}
			}

			gotStrings := make([]string, len(got))
			for i, v := range got {
				gotStrings[i] = string(v.Kind) + " " + v.Edge.String()
			}

			sort.Strings(want)
			sort.Strings(gotStrings)

			if len(want) == 0 {
				require.Empty(t, gotStrings)

				return
			}

			require.Equal(t, want, gotStrings)
		}

		pkg := rapid.SampledFrom(packages)

		t.Repeat(map[string]func(*rapid.T){
			"addEdge": func(t *rapid.T) {
				from := pkg.Draw(t, "from")

				to := pkg.Draw(t, "to")
				if from == to {
					return
				}

				edges = append(edges, lint.Edge{From: from, To: to})

				check()
			},
			"accept": func(t *rapid.T) {
				violations := lint.Evaluate(edges, rules)

				var skipped []lint.Violation

				rules, skipped = lint.Accept(rules, violations, "2026-07-10", "abc1234")

				for _, v := range violations {
					if v.Kind == lint.KindUnlisted {
						model.pending[v.Edge] = true
					}
				}

				for _, v := range skipped {
					require.Contains(t,
						[]lint.ViolationKind{lint.KindForbidden, lint.KindStable}, v.Kind,
						"accept must only refuse forbidden or stable-origin edges")
				}

				check()
			},
			"approve": func(_ *rapid.T) {
				var rejected []lint.Violation

				rules, rejected = lint.Approve(rules)

				for e := range model.pending {
					if !forbidden(e) {
						model.allowed[e] = true
					}
				}

				model.pending = map[lint.Edge]bool{}

				for _, v := range rejected {
					require.True(t, forbidden(v.Edge),
						"approve must only reject forbidden edges")
				}

				check()
			},
			"init": func(_ *rapid.T) {
				fresh := lint.Init(edges)
				rules.Allowed = fresh.Allowed
				rules.Pending = fresh.Pending

				model.allowed = map[lint.Edge]bool{}
				model.pending = map[lint.Edge]bool{}

				for _, e := range edges {
					model.allowed[e] = true
				}

				check()
			},
			"": func(*rapid.T) { check() },
		})

		// Forbid dominance holds at rest: whatever sequence ran, a
		// forbidden edge present in the graph is always reported.
		for _, e := range edges {
			if !forbidden(e) {
				continue
			}

			found := false

			for _, v := range lint.Evaluate(edges, rules) {
				if v.Edge == e && v.Kind == lint.KindForbidden {
					found = true
				}
			}

			require.True(t, found, "forbidden edge %s escaped the gate", e)
		}
	})
}
