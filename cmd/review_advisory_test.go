package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/flamingoosesoftwareinc/uda/internal/git"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"
)

// advisoryFixtureRepo builds a synthetic repo with scripted commit dates:
// packages a and b co-change in every history commit (agent-cadence
// bursts), then a head commit touches only a. The co-change model learns
// a<->b; the head change set misses it.
func advisoryFixtureRepo(t *testing.T) (string, time.Time) {
	t.Helper()

	dir := t.TempDir()

	repo, err := gogit.PlainInit(dir, false)
	require.NoError(t, err)

	worktree, err := repo.Worktree()
	require.NoError(t, err)

	write := func(rel, content string) {
		t.Helper()

		path := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

		_, err := worktree.Add(rel)
		require.NoError(t, err)
	}

	commit := func(msg string, when time.Time) {
		t.Helper()

		_, err := worktree.Commit(msg, &gogit.CommitOptions{
			Author: &object.Signature{Name: "test", Email: "t@t", When: when},
		})
		require.NoError(t, err)
	}

	t0 := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	write("go.mod", "module example.com/fixture\n\ngo 1.22\n")
	write("a/a.go", "package a\n\nfunc A() int { return 0 }\n")
	write("b/b.go", "package b\n\nfunc B() int { return 0 }\n")
	commit("init", t0)

	// 4 sessions x 6 commits at 5-minute spacing, sessions a day apart:
	// a and b always change together.
	for session := range 4 {
		for i := range 6 {
			when := t0.Add(time.Duration(session)*24*time.Hour +
				time.Duration(i+1)*5*time.Minute)
			revision := when.Format("20060102150405")
			write("a/a.go", "package a\n\nfunc A() int { return "+revision+" }\n")
			write("b/b.go", "package b\n\nfunc B() int { return "+revision+" }\n")
			commit("co-change "+revision, when)
		}
	}

	// Head: a changes alone.
	head := t0.Add(4 * 24 * time.Hour)

	write("a/a.go", "package a\n\nfunc A() int { return 999999 }\n")
	commit("a alone", head)

	return dir, head
}

func TestReviewAdvisories(t *testing.T) {
	dir, _ := advisoryFixtureRepo(t)
	t.Chdir(dir)

	repo, err := git.OpenRepository(dir)
	require.NoError(t, err)

	headSha, err := repo.ResolveCommit("HEAD")
	require.NoError(t, err)

	baseSha, err := repo.ResolveCommit("HEAD~1")
	require.NoError(t, err)

	touched, headTime, err := rangeTouchedFiles(repo, &git.CommitRange{
		From: &baseSha,
		To:   headSha,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"a/a.go"}, touched)

	snapshot := reviewSnapshot{touchedFiles: touched, headTime: headTime}

	advisories, err := reviewAdvisories(t.Context(), repo, snapshot, "auto")
	require.NoError(t, err)

	require.Len(t, advisories, 1, "a changed alone after always co-changing with b")
	require.Equal(t, "example.com/fixture/a", advisories[0].Touched)
	require.Equal(t, "example.com/fixture/b", advisories[0].Expected)
	require.Greater(t, advisories[0].Correlation, 0.6)
}

// TestReviewAdvisories_bothTouched is the silent case: the head change
// honors the co-change expectation, so no advisory fires.
func TestReviewAdvisories_bothTouched(t *testing.T) {
	dir, headTime := advisoryFixtureRepo(t)
	t.Chdir(dir)

	repo, err := git.OpenRepository(dir)
	require.NoError(t, err)

	snapshot := reviewSnapshot{
		touchedFiles: []string{"a/a.go", "b/b.go"},
		headTime:     headTime,
	}

	advisories, err := reviewAdvisories(t.Context(), repo, snapshot, "auto")
	require.NoError(t, err)
	require.Empty(t, advisories)
}

// TestReviewAdvisories_deterministic is the reproducibility probe: the
// window anchors at the snapshot head, not the wall clock, so the same
// history yields byte-identical advisories on every run.
func TestReviewAdvisories_deterministic(t *testing.T) {
	dir, _ := advisoryFixtureRepo(t)
	t.Chdir(dir)

	repo, err := git.OpenRepository(dir)
	require.NoError(t, err)

	headSha, err := repo.ResolveCommit("HEAD")
	require.NoError(t, err)

	baseSha, err := repo.ResolveCommit("HEAD~1")
	require.NoError(t, err)

	touched, headTime, err := rangeTouchedFiles(repo, &git.CommitRange{
		From: &baseSha,
		To:   headSha,
	})
	require.NoError(t, err)

	snapshot := reviewSnapshot{touchedFiles: touched, headTime: headTime}

	first, err := reviewAdvisories(t.Context(), repo, snapshot, "auto")
	require.NoError(t, err)

	second, err := reviewAdvisories(t.Context(), repo, snapshot, "auto")
	require.NoError(t, err)

	require.Equal(t, first, second)
}
