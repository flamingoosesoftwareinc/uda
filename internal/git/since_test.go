package git

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func setupTimedRepo(t *testing.T) *Repository {
	t.Helper()

	dir := t.TempDir()

	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()

	// Commit 1: 90 days ago
	writeFile(t, dir, "old.go", "package old")

	if _, err := wt.Add("old.go"); err != nil {
		t.Fatal(err)
	}

	if _, err := wt.Commit("old commit", &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "t@t.com",
			When:  now.Add(-90 * 24 * time.Hour),
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Commit 2: 30 days ago
	writeFile(t, dir, "mid.go", "package mid")

	if _, err := wt.Add("mid.go"); err != nil {
		t.Fatal(err)
	}

	if _, err := wt.Commit("mid commit", &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "t@t.com",
			When:  now.Add(-30 * 24 * time.Hour),
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Commit 3: 1 day ago
	writeFile(t, dir, "recent.go", "package recent")

	if _, err := wt.Add("recent.go"); err != nil {
		t.Fatal(err)
	}

	if _, err := wt.Commit("recent commit", &gogit.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "t@t.com", When: now.Add(-24 * time.Hour)},
	}); err != nil {
		t.Fatal(err)
	}

	return &Repository{repo: repo, path: dir}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCommitsSince(t *testing.T) {
	r := setupTimedRepo(t)

	cases := []struct {
		name  string
		since time.Duration
		want  int
	}{
		{"all_commits", 365 * 24 * time.Hour, 3},
		{"last_60_days", 60 * 24 * time.Hour, 2},
		{"last_7_days", 7 * 24 * time.Hour, 1},
		{"last_hour", time.Hour, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hashes, err := r.CommitsSince(tc.since)
			if err != nil {
				t.Fatal(err)
			}

			if len(hashes) != tc.want {
				t.Errorf(
					"CommitsSince(%v) returned %d commits, want %d",
					tc.since,
					len(hashes),
					tc.want,
				)
			}
		})
	}
}

func TestCommitsSinceOldestFirst(t *testing.T) {
	r := setupTimedRepo(t)

	hashes, err := r.CommitsSince(365 * 24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if len(hashes) != 3 {
		t.Fatalf("expected 3 commits, got %d", len(hashes))
	}

	// Verify oldest-first by checking file changes
	details, err := r.CommitDetails(hashes)
	if err != nil {
		t.Fatal(err)
	}

	if details[0].Timestamp.After(details[1].Timestamp) {
		t.Error("commits not in oldest-first order")
	}

	if details[1].Timestamp.After(details[2].Timestamp) {
		t.Error("commits not in oldest-first order")
	}
}
