// Package lint evaluates a repository's dependency graph against the
// coupling policy declared in the repo's .uda.yaml lint block. The policy
// is a lockfile: every internal edge is enumerated, new edges fail until a
// human moves them from pending to allowed, and forbid rules can never be
// accepted at all.
package lint

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// edgeSeparator joins the two ends of an edge in its yaml representation.
const edgeSeparator = " -> "

// Edge is a directed dependency between two internal packages, identified
// by their repo-relative names.
type Edge struct {
	From string
	To   string
}

// String renders the yaml lockfile form: "from -> to".
func (e Edge) String() string {
	return e.From + edgeSeparator + e.To
}

// ParseEdge parses the yaml lockfile form produced by Edge.String.
func ParseEdge(s string) (Edge, error) {
	from, to, ok := strings.Cut(s, "->")
	if !ok {
		return Edge{}, fmt.Errorf("invalid edge %q: want \"from -> to\"", s)
	}

	edge := Edge{From: strings.TrimSpace(from), To: strings.TrimSpace(to)}
	if edge.From == "" || edge.To == "" {
		return Edge{}, fmt.Errorf("invalid edge %q: empty endpoint", s)
	}

	return edge, nil
}

// ForbidRule bans edges matching a pair of glob patterns (see MatchGlob).
type ForbidRule struct {
	From string `mapstructure:"from" yaml:"from"`
	To   string `mapstructure:"to"   yaml:"to"`
}

// Matches reports whether the rule bans the given edge.
func (r ForbidRule) Matches(e Edge) bool {
	return MatchGlob(r.From, e.From) && MatchGlob(r.To, e.To)
}

// PendingEdge is an edge staged by accept but not yet human-approved.
// Lint stays red while any exist — pending is a visible-in-diff staging
// area, not a bypass.
type PendingEdge struct {
	Edge  string `mapstructure:"edge"  yaml:"edge"`
	Added string `mapstructure:"added" yaml:"added,omitempty"`
	By    string `mapstructure:"by"    yaml:"by,omitempty"`
}

// Roles declares package intent. Stable packages expect dependents:
// their new outbound edges are never auto-acceptable — accept skips them
// so only a deliberate yaml edit can allow the coupling.
type Roles struct {
	Stable []string `mapstructure:"stable" yaml:"stable,omitempty"`
}

// Rules is one language's policy block from the .uda.yaml lint section.
type Rules struct {
	// Boundary overrides the package-boundary granularity for this
	// language's analyzer (e.g. "module" for Python so intra-package
	// imports aren't collapsed into one node). Empty means the analyzer
	// default ("package"). Only honored for languages whose analyzer
	// supports it (python, rust, typescript).
	Boundary string        `mapstructure:"boundary" yaml:"boundary,omitempty"`
	Roles    Roles         `mapstructure:"roles"    yaml:"roles,omitempty"`
	Forbid   []ForbidRule  `mapstructure:"forbid"   yaml:"forbid,omitempty"`
	Allowed  []string      `mapstructure:"allowed"  yaml:"allowed"`
	Pending  []PendingEdge `mapstructure:"pending"  yaml:"pending"`
}

// ViolationKind classifies why an edge fails the gate. Stable strings —
// they appear in JSON output.
type ViolationKind string

// Violation kinds, ordered by severity.
const (
	// KindForbidden: the edge matches a forbid rule. Never acceptable.
	KindForbidden ViolationKind = "forbidden"
	// KindStable: the edge originates from a declared-stable package and
	// is not allowed. Accept refuses to stage it; allowing it is a
	// deliberate yaml edit.
	KindStable ViolationKind = "stable"
	// KindUnlisted: the edge exists in the graph but not in the lockfile.
	KindUnlisted ViolationKind = "unlisted"
	// KindPending: the edge is staged but awaiting approval.
	KindPending ViolationKind = "pending"
)

// Violation is one gate failure.
type Violation struct {
	Kind ViolationKind `json:"kind"`
	Edge Edge          `json:"-"`
	Rule string        `json:"rule,omitempty"` // forbid pattern or stable package that fired
}

// kindSeverity orders violation kinds most-severe-first for output.
var kindSeverity = [...]ViolationKind{KindForbidden, KindStable, KindUnlisted, KindPending}

func kindRank(kind ViolationKind) int {
	for rank, k := range kindSeverity {
		if k == kind {
			return rank
		}
	}

	return len(kindSeverity)
}

// Evaluate compares the current graph against the rules. Precedence per
// edge: forbid beats allowed beats pending beats unlisted; a stable origin
// upgrades unlisted. The result is sorted severity-first, then by edge.
func Evaluate(edges []Edge, rules Rules) []Violation {
	allowed := make(map[Edge]bool, len(rules.Allowed))

	for _, s := range rules.Allowed {
		if e, err := ParseEdge(s); err == nil {
			allowed[e] = true
		}
	}

	pending := make(map[Edge]bool, len(rules.Pending))

	for _, p := range rules.Pending {
		if e, err := ParseEdge(p.Edge); err == nil {
			pending[e] = true
		}
	}

	violations := make([]Violation, 0, len(edges))

	for _, edge := range dedupe(edges) {
		violations = append(violations, classify(edge, rules, allowed, pending)...)
	}

	if len(violations) == 0 {
		return nil
	}

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Kind != violations[j].Kind {
			return kindRank(violations[i].Kind) < kindRank(violations[j].Kind)
		}

		return violations[i].Edge.String() < violations[j].Edge.String()
	})

	return violations
}

func classify(edge Edge, rules Rules, allowed, pending map[Edge]bool) []Violation {
	for _, rule := range rules.Forbid {
		if rule.Matches(edge) {
			return []Violation{{
				Kind: KindForbidden,
				Edge: edge,
				Rule: rule.From + edgeSeparator + rule.To,
			}}
		}
	}

	if allowed[edge] {
		return nil
	}

	if pending[edge] {
		return []Violation{{Kind: KindPending, Edge: edge}}
	}

	for _, stable := range rules.Roles.Stable {
		if MatchGlob(stable, edge.From) {
			return []Violation{{Kind: KindStable, Edge: edge, Rule: stable}}
		}
	}

	return []Violation{{Kind: KindUnlisted, Edge: edge}}
}

// Init snapshots the current graph into a fresh allowed list — adopting
// lint on an existing repo starts green. Forbid/roles are authored by
// humans afterwards; Init never invents policy.
func Init(edges []Edge) Rules {
	deduped := dedupe(edges)
	allowed := make([]string, len(deduped))

	for i, e := range deduped {
		allowed[i] = e.String()
	}

	return Rules{Allowed: allowed, Pending: []PendingEdge{}}
}

// Accept stages every unlisted violation as pending, attributed with the
// added/by metadata. Forbidden and stable-origin edges are never staged —
// they come back as skipped so the caller can surface why. Lint stays red
// until approval.
func Accept(rules Rules, violations []Violation, added, addedBy string) (Rules, []Violation) {
	var skipped []Violation

	for _, v := range violations {
		switch v.Kind {
		case KindUnlisted:
			rules.Pending = append(rules.Pending, PendingEdge{
				Edge:  v.Edge.String(),
				Added: added,
				By:    addedBy,
			})
		case KindForbidden, KindStable:
			skipped = append(skipped, v)
		case KindPending:
			// Already staged — nothing to do.
		}
	}

	sort.Slice(rules.Pending, func(i, j int) bool {
		return rules.Pending[i].Edge < rules.Pending[j].Edge
	})

	return rules, skipped
}

// Approve moves every pending edge into allowed. A pending entry that
// matches a forbid rule is dropped and reported — forbid wins over every
// path into the lockfile, including a hand-edited pending block.
func Approve(rules Rules) (Rules, []Violation) {
	var rejected []Violation

	for _, p := range rules.Pending {
		edge, err := ParseEdge(p.Edge)
		if err != nil {
			continue
		}

		if v := forbidViolation(edge, rules.Forbid); v != nil {
			rejected = append(rejected, *v)

			continue
		}

		rules.Allowed = append(rules.Allowed, edge.String())
	}

	rules.Pending = []PendingEdge{}
	rules.Allowed = dedupeStrings(rules.Allowed)

	return rules, rejected
}

func forbidViolation(edge Edge, forbid []ForbidRule) *Violation {
	for _, rule := range forbid {
		if rule.Matches(edge) {
			return &Violation{
				Kind: KindForbidden,
				Edge: edge,
				Rule: rule.From + edgeSeparator + rule.To,
			}
		}
	}

	return nil
}

// MatchGlob matches a package path against a pattern with path semantics:
// "*" matches within one segment, "**" matches any number of segments
// (including zero), and a pattern without wildcards must match exactly.
func MatchGlob(pattern, name string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchSegments(pattern, name []string) bool {
	if len(pattern) == 0 {
		return len(name) == 0
	}

	if pattern[0] == "**" {
		for skip := 0; skip <= len(name); skip++ {
			if matchSegments(pattern[1:], name[skip:]) {
				return true
			}
		}

		return false
	}

	if len(name) == 0 {
		return false
	}

	if ok, err := path.Match(pattern[0], name[0]); err != nil || !ok {
		return false
	}

	return matchSegments(pattern[1:], name[1:])
}

func dedupe(edges []Edge) []Edge {
	seen := make(map[Edge]bool, len(edges))
	out := make([]Edge, 0, len(edges))

	for _, e := range edges {
		if !seen[e] {
			seen[e] = true

			out = append(out, e)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })

	return out
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))

	for _, v := range values {
		if !seen[v] {
			seen[v] = true

			out = append(out, v)
		}
	}

	sort.Strings(out)

	return out
}
