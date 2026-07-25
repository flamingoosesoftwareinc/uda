package ui_test

import (
	"testing"

	"github.com/flamingoosesoftwareinc/uda/cmd/ui"
	"github.com/flamingoosesoftwareinc/uda/internal/evocoupling"
	"github.com/sebdah/goldie/v2"
	"github.com/stretchr/testify/require"
)

func lintTestReport() ui.LintReport {
	return ui.LintReport{
		Languages: []ui.LintLanguageReport{
			{
				Language: "go",
				Violations: []ui.LintViolation{
					{
						Kind: "forbidden",
						Edge: "internal/domain -> internal/adapter/http",
						Rule: "internal/domain -> internal/adapter/**",
					},
					{
						Kind: "stable",
						Edge: "internal/domain -> internal/cache",
						Rule: "internal/domain",
					},
					{Kind: "unlisted", Edge: "cmd -> internal/cache"},
					{Kind: "pending", Edge: "cmd -> internal/lint"},
				},
			},
			{
				Language:   "typescript",
				Violations: []ui.LintViolation{{Kind: "unlisted", Edge: "src/api -> src/core"}},
			},
		},
	}
}

func TestLintText(t *testing.T) {
	t.Parallel()

	g := goldie.New(t)
	g.Assert(t, "lint_text", []byte(ui.LintText(lintTestReport())))
}

func TestLintText_emptyReportIsSilent(t *testing.T) {
	t.Parallel()

	require.Empty(t, ui.LintText(ui.LintReport{}))
}

func TestLintJSON(t *testing.T) {
	t.Parallel()

	out, err := ui.LintJSON(lintTestReport())
	require.NoError(t, err)

	g := goldie.New(t)
	g.Assert(t, "lint_json", []byte(out))
}

// TestReviewText_advisories pins the informational co-change section —
// rendered even when the structural diff is empty.
func TestReviewText_advisories(t *testing.T) {
	t.Parallel()

	result := ui.ReviewResult{
		BaseLabel: "abc1234",
		HeadLabel: "def5678",
		Advisories: []evocoupling.Advisory{
			{Touched: "internal/a", Expected: "internal/b", Correlation: 0.83},
			{Touched: "internal/a", Expected: "internal/c", Correlation: 0.61},
		},
	}

	g := goldie.New(t)
	g.Assert(t, "review_advisories_text", []byte(ui.ReviewText(result)))
}
