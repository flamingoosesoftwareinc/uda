package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/template"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/diff"
	"github.com/flamingoosesoftwareinc/uda/internal/evocoupling"
)

// ReviewResult holds everything needed to render a review.
type ReviewResult struct {
	BaseLabel  string
	HeadLabel  string
	Diffs      []diff.PackageDiff
	PrevAll    []analyzer.Metrics
	CurrAll    []analyzer.Metrics
	Advisories []evocoupling.Advisory
}

// reviewEdge is one outward dependency edge with its symbol-level diffs.
type reviewEdge struct {
	from string
	to   string
	syms []diff.SymbolDiff
}

// reviewCategories groups a review's package and dependency changes by kind.
type reviewCategories struct {
	newDeps     []reviewEdge
	removedDeps []reviewEdge
	changedDeps []reviewEdge
	newPkgs     []string
	removedPkgs []string
}

func categorizeReviewDiffs(diffs []diff.PackageDiff) reviewCategories {
	var cats reviewCategories

	for _, pkgDiff := range diffs {
		switch pkgDiff.DiffType {
		case diff.Added:
			cats.newPkgs = append(cats.newPkgs, pkgDiff.Package)
		case diff.Removed:
			cats.removedPkgs = append(cats.removedPkgs, pkgDiff.Package)
		case diff.Unchanged:
			// nothing to record at the package level
		}

		categorizeEdges(&cats, pkgDiff)
	}

	return cats
}

func categorizeEdges(cats *reviewCategories, pkgDiff diff.PackageDiff) {
	for _, dep := range pkgDiff.OutwardDiff {
		edge := reviewEdge{from: pkgDiff.Package, to: dep.Package, syms: dep.Symbols}

		switch dep.DiffType {
		case diff.Added:
			cats.newDeps = append(cats.newDeps, edge)
		case diff.Removed:
			cats.removedDeps = append(cats.removedDeps, edge)
		case diff.Unchanged:
			if hasSymbolChanges(dep.Symbols) {
				cats.changedDeps = append(cats.changedDeps, edge)
			}
		}
	}
}

func (c reviewCategories) hasChanges() bool {
	return len(c.newDeps) > 0 || len(c.removedDeps) > 0 || len(c.changedDeps) > 0 ||
		len(c.newPkgs) > 0 || len(c.removedPkgs) > 0
}

func writeEdgeSection(b *strings.Builder, title string, edges []reviewEdge) {
	if len(edges) == 0 {
		return
	}

	fmt.Fprintf(b, "\n%s:\n", title)

	for _, e := range edges {
		fmt.Fprintf(b, "  %s → %s\n", e.from, e.to)
		writeSymbolLines(b, e.syms)
	}
}

// ReviewText renders a ReviewResult as plain text.
// Only packages with actual changes are shown; unchanged packages are omitted.
func ReviewText(r ReviewResult) string {
	cats := categorizeReviewDiffs(r.Diffs)
	if !cats.hasChanges() {
		return advisorySection(r.Advisories)
	}

	var b strings.Builder

	fmt.Fprintf(&b, "Comparing %s → %s\n", r.BaseLabel, r.HeadLabel)

	writeEdgeSection(&b, "New dependencies", cats.newDeps)
	writeEdgeSection(&b, "Removed dependencies", cats.removedDeps)
	writeEdgeSection(&b, "Changed dependencies", cats.changedDeps)

	if len(cats.newPkgs) > 0 {
		b.WriteString("\nNew packages:\n")

		for _, pkg := range cats.newPkgs {
			curr := findMetrics(r.CurrAll, pkg)
			if curr != nil {
				fmt.Fprintf(&b, "  %s  inward %d  outward %d\n",
					pkg, len(curr.Inward), len(curr.Outward))
			} else {
				fmt.Fprintf(&b, "  %s\n", pkg)
			}
		}
	}

	if len(cats.removedPkgs) > 0 {
		b.WriteString("\nRemoved packages:\n")

		for _, pkg := range cats.removedPkgs {
			fmt.Fprintf(&b, "  %s\n", pkg)
		}
	}

	// Summary table
	b.WriteString("\n")
	b.WriteString(reviewSummaryTable(r))
	b.WriteString(advisorySection(r.Advisories))

	return b.String()
}

// ReviewJSON renders a ReviewResult as JSON: the changed packages plus
// any co-change advisories.
func ReviewJSON(r ReviewResult) (string, error) {
	report := struct {
		Changes    []diff.PackageDiff     `json:"changes"`
		Advisories []evocoupling.Advisory `json:"advisories,omitempty"`
	}{
		Changes:    filterChangedDiffs(r.Diffs),
		Advisories: r.Advisories,
	}

	output, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}

	return string(output), nil
}

// filterChangedDiffs returns only diffs that have actual changes.
func filterChangedDiffs(diffs []diff.PackageDiff) []diff.PackageDiff {
	var result []diff.PackageDiff

	for _, pd := range diffs {
		if pd.DiffType != diff.Unchanged || packageHasChanges(pd) {
			result = append(result, pd)
		}
	}

	return result
}

func writeSymbolLines(b *strings.Builder, syms []diff.SymbolDiff) {
	for _, s := range syms {
		switch s.DiffType {
		case diff.Added:
			fmt.Fprintf(b, "    + %s\n", s.Name)

			for _, p := range s.Positions {
				fmt.Fprintf(b, "        %s:%d\n", p.Position.File, p.Position.Line)
			}
		case diff.Removed:
			fmt.Fprintf(b, "    - %s\n", s.Name)

			for _, p := range s.Positions {
				fmt.Fprintf(b, "        %s:%d\n", p.Position.File, p.Position.Line)
			}
		case diff.Unchanged:
			// Only show if positions changed
			var added, removed []diff.PositionDiff

			for _, p := range s.Positions {
				switch p.DiffType {
				case diff.Added:
					added = append(added, p)
				case diff.Removed:
					removed = append(removed, p)
				case diff.Unchanged:
					// position unchanged — nothing to record
				}
			}

			if len(added) > 0 || len(removed) > 0 {
				fmt.Fprintf(b, "    ~ %s\n", s.Name)

				for _, p := range added {
					fmt.Fprintf(b, "        + %s:%d\n", p.Position.File, p.Position.Line)
				}

				for _, p := range removed {
					fmt.Fprintf(b, "        - %s:%d\n", p.Position.File, p.Position.Line)
				}
			}
		}
	}
}

func hasSymbolChanges(syms []diff.SymbolDiff) bool {
	for _, s := range syms {
		if s.DiffType != diff.Unchanged {
			return true
		}

		if s.PrevCount != s.CurrCount {
			return true
		}

		for _, p := range s.Positions {
			if p.DiffType != diff.Unchanged {
				return true
			}
		}
	}

	return false
}

func packageHasChanges(pkgDiff diff.PackageDiff) bool {
	for _, dep := range pkgDiff.OutwardDiff {
		if dep.DiffType != diff.Unchanged {
			return true
		}

		if hasSymbolChanges(dep.Symbols) {
			return true
		}
	}

	for _, dep := range pkgDiff.InwardDiff {
		if dep.DiffType != diff.Unchanged {
			return true
		}

		if hasSymbolChanges(dep.Symbols) {
			return true
		}
	}

	return false
}

func findMetrics(all []analyzer.Metrics, pkg string) *analyzer.Metrics {
	for i := range all {
		if string(all[i].Package) == pkg {
			return &all[i]
		}
	}

	return nil
}

func metricsInstability(m *analyzer.Metrics) float64 {
	if m == nil {
		return 0
	}

	return m.Instability()
}

func metricsInward(m *analyzer.Metrics) int {
	if m == nil {
		return 0
	}

	return len(m.Inward)
}

func metricsOutward(m *analyzer.Metrics) int {
	if m == nil {
		return 0
	}

	return len(m.Outward)
}

// reviewSummaryTable renders the summary section as a lipgloss table.
func reviewSummaryTable(r ReviewResult) string {
	var rows [][]string

	for _, pkgDiff := range r.Diffs {
		prev := findMetrics(r.PrevAll, pkgDiff.Package)
		curr := findMetrics(r.CurrAll, pkgDiff.Package)

		if pkgDiff.DiffType == diff.Added {
			rows = append(rows, []string{
				"+ " + pkgDiff.Package,
				fmt.Sprintf("%.2f", metricsInstability(curr)),
				strconv.Itoa(metricsInward(curr)),
				strconv.Itoa(metricsOutward(curr)),
			})

			continue
		}

		if pkgDiff.DiffType == diff.Removed {
			rows = append(rows, []string{
				"- " + pkgDiff.Package,
				"(removed)",
				"",
				"",
			})

			continue
		}

		if !packageHasChanges(pkgDiff) {
			continue
		}

		prevI := metricsInstability(prev)
		currI := metricsInstability(curr)
		rows = append(rows, []string{
			"~ " + pkgDiff.Package,
			fmt.Sprintf("%.2f → %.2f", prevI, currI),
			fmt.Sprintf("%d → %d", metricsInward(prev), metricsInward(curr)),
			fmt.Sprintf("%d → %d", metricsOutward(prev), metricsOutward(curr)),
		})
	}

	if len(rows) == 0 {
		return ""
	}

	lightDark := lipgloss.LightDark(lipgloss.HasDarkBackground(os.Stdin, os.Stdout))
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lightDark(lipgloss.Color("0"), lipgloss.Color("15")))
	headerStyle := lipgloss.NewStyle().Bold(true).Align(lipgloss.Center)
	cellStyle := lipgloss.NewStyle().Padding(0, 1)

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle.Padding(0, 1)
			}

			return cellStyle
		}).
		Headers("PACKAGE", "INSTABILITY", "INWARD", "OUTWARD").
		Rows(rows...)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Summary"))
	b.WriteString("\n")
	b.WriteString(t.Render())
	b.WriteString("\n")

	return b.String()
}

// ReviewTable renders a ReviewResult as a table (dependency changes as text + summary as table).
func ReviewTable(r ReviewResult) string {
	return ReviewText(r)
}

// advisoryTemplate renders the informational co-change section:
//
//	Co-change advisories (informational):
//	  internal/a changed without internal/b (correlation 0.83)
var advisoryTemplate = template.Must(template.New("advisories").Parse(
	`{{- if . }}
Co-change advisories (informational):
{{- range . }}
  {{ .Touched }} changed without {{ .Expected }} (correlation {{ printf "%.2f" .Correlation }})
{{- end }}
{{ end -}}`))

// advisorySection renders the advisory block, empty when there are none.
func advisorySection(advisories []evocoupling.Advisory) string {
	var b strings.Builder

	// The template only touches Advisory fields; execution cannot fail.
	_ = advisoryTemplate.Execute(&b, advisories)

	return b.String()
}
