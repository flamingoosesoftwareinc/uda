package git

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func setupTestRepo(t *testing.T) *Repository {
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

	sig := &object.Signature{
		Name:  "Test",
		Email: "test@test.com",
		When:  time.Now(),
	}

	// Commit 1: add file1.go
	if err := os.WriteFile(
		filepath.Join(dir, "file1.go"),
		[]byte("package main"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := wt.Add("file1.go"); err != nil {
		t.Fatal(err)
	}

	hash1, err := wt.Commit("add file1", &gogit.CommitOptions{Author: sig})
	if err != nil {
		t.Fatal(err)
	}

	// Commit 2: add file2.go, modify file1.go
	if err := os.WriteFile(
		filepath.Join(dir, "file2.go"),
		[]byte("package lib"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(dir, "file1.go"),
		[]byte("package main\n// updated"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := wt.Add("file2.go"); err != nil {
		t.Fatal(err)
	}

	if _, err := wt.Add("file1.go"); err != nil {
		t.Fatal(err)
	}

	hash2, err := wt.Commit("add file2, update file1", &gogit.CommitOptions{Author: sig})
	if err != nil {
		t.Fatal(err)
	}

	_ = hash1
	_ = hash2

	return &Repository{
		repo: repo,
		path: dir,
	}
}

func TestCommitChangedFiles(t *testing.T) {
	r := setupTestRepo(t)

	// Parse full range
	cr, err := ParseCommitRange(r, "HEAD~1..HEAD")
	if err != nil {
		t.Fatal(err)
	}

	hashes, err := r.Commits(cr)
	if err != nil {
		t.Fatal(err)
	}

	commits, err := r.CommitChangedFiles(hashes)
	if err != nil {
		t.Fatal(err)
	}

	if len(commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(commits))
	}

	files := commits[0].Files
	if len(files) < 2 {
		t.Fatalf("expected at least 2 files changed, got %d: %v", len(files), files)
	}

	// Check that both file1.go and file2.go appear
	hasFile1, hasFile2 := false, false

	for _, f := range files {
		if f == "file1.go" {
			hasFile1 = true
		}

		if f == "file2.go" {
			hasFile2 = true
		}
	}

	if !hasFile1 || !hasFile2 {
		t.Errorf("expected file1.go and file2.go, got %v", files)
	}
}

func TestCommitDetails(t *testing.T) {
	r := setupTestRepo(t)

	cr, err := ParseCommitRange(r, "HEAD~1..HEAD")
	if err != nil {
		t.Fatal(err)
	}

	hashes, err := r.Commits(cr)
	if err != nil {
		t.Fatal(err)
	}

	details, err := r.CommitDetails(hashes)
	if err != nil {
		t.Fatal(err)
	}

	if len(details) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(details))
	}

	d := details[0]
	if d.Message != "add file2, update file1" {
		t.Errorf("unexpected message: %q", d.Message)
	}

	if d.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}

	if len(d.Files) < 2 {
		t.Fatalf("expected at least 2 file changes, got %d", len(d.Files))
	}

	// file2.go should be an addition
	for _, fc := range d.Files {
		if fc.Path == "file2.go" {
			if fc.Additions == 0 {
				t.Errorf("expected additions for file2.go, got 0")
			}
		}
	}
}
