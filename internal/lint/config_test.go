package lint_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/lint"
	"github.com/sebdah/goldie/v2"
	"github.com/stretchr/testify/require"
)

func copyFixture(t *testing.T, name string) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), ".uda.yaml")
	require.NoError(t, os.WriteFile(path, raw, 0o644))

	return path
}

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	t.Run("parses_languages_and_exclude", func(t *testing.T) {
		t.Parallel()

		cfg, err := lint.LoadConfig(copyFixture(t, "rewrite_input.yaml"))
		require.NoError(t, err)

		require.Equal(t, []string{"poc/**"}, cfg.Exclude)
		require.Len(t, cfg.Languages, 2)

		goRules := cfg.Languages["go"]
		require.Equal(t, []string{"internal/domain"}, goRules.Roles.Stable)
		require.Equal(t,
			[]lint.ForbidRule{{From: "internal/domain", To: "internal/adapter/**"}},
			goRules.Forbid)
		require.Equal(t, []string{"cmd -> internal/analyzer"}, goRules.Allowed)

		require.Equal(t, []string{"src/api -> src/core"}, cfg.Languages["typescript"].Allowed)
	})

	t.Run("missing_file_is_no_lint_config", func(t *testing.T) {
		t.Parallel()

		_, err := lint.LoadConfig(filepath.Join(t.TempDir(), ".uda.yaml"))
		require.ErrorIs(t, err, lint.ErrNoLintConfig)
	})

	t.Run("file_without_lint_section_is_no_lint_config", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), ".uda.yaml")
		require.NoError(t, os.WriteFile(path, []byte("since: 90d\n"), 0o644))

		_, err := lint.LoadConfig(path)
		require.ErrorIs(t, err, lint.ErrNoLintConfig)
	})
}

// TestWriteRules_golden pins the full rewritten file: comments, the
// untouched sibling sections (analyzer defaults, advisory, the other
// language block), and the roles/forbid blocks must all survive a rewrite
// of allowed+pending — the failure a struct re-marshal would cause and an
// inline assert would miss.
func TestWriteRules_golden(t *testing.T) {
	t.Parallel()

	g := goldie.New(t)

	t.Run("accept_rewrites_go_block_only", func(t *testing.T) {
		t.Parallel()

		path := copyFixture(t, "rewrite_input.yaml")

		cfg, err := lint.LoadConfig(path)
		require.NoError(t, err)

		rules, skipped := lint.Accept(
			cfg.Languages["go"],
			lint.Evaluate([]lint.Edge{
				{From: "cmd", To: "internal/analyzer"}, // already allowed
				{From: "cmd", To: "internal/cache"},    // new -> staged
			}, cfg.Languages["go"]),
			"2026-07-10", "abc1234",
		)
		require.Empty(t, skipped)

		require.NoError(t, lint.WriteRules(path, "go", rules))

		out, err := os.ReadFile(path)
		require.NoError(t, err)
		g.Assert(t, "rewrite_accept", out)
	})

	t.Run("approve_then_rewrite", func(t *testing.T) {
		t.Parallel()

		path := copyFixture(t, "rewrite_input.yaml")

		cfg, err := lint.LoadConfig(path)
		require.NoError(t, err)

		rules := cfg.Languages["go"]
		rules.Pending = []lint.PendingEdge{
			{Edge: "cmd -> internal/cache", Added: "2026-07-10", By: "abc1234"},
		}

		rules, rejected := lint.Approve(rules)
		require.Empty(t, rejected)

		require.NoError(t, lint.WriteRules(path, "go", rules))

		out, err := os.ReadFile(path)
		require.NoError(t, err)
		g.Assert(t, "rewrite_approve", out)
	})

	t.Run("init_creates_file_and_block", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), ".uda.yaml")

		rules := lint.Init([]lint.Edge{
			{From: "cmd", To: "internal/analyzer"},
			{From: "internal/analyzer", To: "internal/detect"},
		})

		require.NoError(t, lint.WriteRules(path, "go", rules))

		out, err := os.ReadFile(path)
		require.NoError(t, err)
		g.Assert(t, "rewrite_init", out)
	})
}
