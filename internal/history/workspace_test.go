package history_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	udagit "github.com/flamingoosesoftwareinc/uda/internal/git"
	"github.com/flamingoosesoftwareinc/uda/internal/history"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"
)

// movableRepo builds a tiny repo at dir and returns a commit function that
// writes a file revision and commits it.
func movableRepo(t *testing.T, dir string) func(rev string) plumbing.Hash {
	t.Helper()

	repo, err := gogit.PlainInit(dir, false)
	require.NoError(t, err)

	worktree, err := repo.Worktree()
	require.NoError(t, err)

	when := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	return func(rev string) plumbing.Hash {
		t.Helper()

		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "main.go"),
			[]byte("package main\n\n// rev "+rev+"\n"), 0o644))

		_, err := worktree.Add("main.go")
		require.NoError(t, err)

		when = when.Add(time.Minute)

		hash, err := worktree.Commit("rev "+rev, &gogit.CommitOptions{
			Author: &object.Signature{Name: "t", Email: "t@t", When: when},
		})
		require.NoError(t, err)

		return hash
	}
}

// TestWorkspace_survivesRepoMove reproduces the stale-origin failure: a
// workspace cloned while the repo lived at path A must still be able to
// fetch new commits after the repo moves to path B. The cached clone's
// origin remote pins the old absolute path; Checkout's fetch used to die
// with "fetching: repository not found" until the cache was deleted by
// hand.
func TestWorkspace_survivesRepoMove(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	oldPath := filepath.Join(root, "old", "repo")
	require.NoError(t, os.MkdirAll(oldPath, 0o755))

	commit := movableRepo(t, oldPath)
	first := commit("one")

	// Workspace cloned while the repo lives at oldPath.
	workspacePath := filepath.Join(root, "cache", "workspace")

	oldRepo, err := udagit.OpenRepository(oldPath)
	require.NoError(t, err)

	workspace, err := history.NewWorkspace(oldRepo, workspacePath)
	require.NoError(t, err)
	require.NoError(t, workspace.Checkout(first))

	// The repo moves on disk; a commit lands at the new location only.
	newPath := filepath.Join(root, "new", "repo")
	require.NoError(t, os.MkdirAll(filepath.Dir(newPath), 0o755))
	require.NoError(t, os.Rename(oldPath, newPath))

	movedRepo, err := udagit.OpenRepository(newPath)
	require.NoError(t, err)

	reopened, err := history.NewWorkspace(movedRepo, workspacePath)
	require.NoError(t, err)

	second := commitAt(t, newPath, "two")

	require.NoError(t, reopened.Checkout(second),
		"workspace must fetch from the repo's current location after a move")
}

// commitAt opens the repo at dir and commits a new revision of main.go.
func commitAt(t *testing.T, dir, rev string) plumbing.Hash {
	t.Helper()

	repo, err := gogit.PlainOpen(dir)
	require.NoError(t, err)

	worktree, err := repo.Worktree()
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "main.go"),
		[]byte("package main\n\n// rev "+rev+"\n"), 0o644))

	_, err = worktree.Add("main.go")
	require.NoError(t, err)

	hash, err := worktree.Commit("rev "+rev, &gogit.CommitOptions{
		Author: &object.Signature{
			Name: "t", Email: "t@t",
			When: time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC),
		},
	})
	require.NoError(t, err)

	return hash
}
