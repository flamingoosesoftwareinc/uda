package git

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// Repository wraps a go-git repository with additional utilities.
type Repository struct {
	repo   *git.Repository
	path   string
	repoID string
}

// OpenRepository opens a git repository at the given path.
// Returns an error if the path is not a git repository.
func OpenRepository(path string) (*Repository, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

	repo, err := git.PlainOpenWithOptions(absPath, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return nil, fmt.Errorf("opening repository: %w", err)
	}

	// Find the actual root of the repository
	wt, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("getting worktree: %w", err)
	}

	rootPath := wt.Filesystem.Root()

	return &Repository{
		repo:   repo,
		path:   rootPath,
		repoID: computeRepoID(repo, rootPath),
	}, nil
}

// RepoID returns a unique identifier for the repository.
// This is a hash of the origin URL if available, otherwise a hash of the path.
func (r *Repository) RepoID() string {
	return r.repoID
}

// RootPath returns the root path of the repository.
func (r *Repository) RootPath() string {
	return r.path
}

// ResolveCommit resolves a commit-ish string (e.g., "HEAD", "main", "abc123")
// to a concrete commit hash.
func (r *Repository) ResolveCommit(commitish string) (plumbing.Hash, error) {
	hash, err := r.repo.ResolveRevision(plumbing.Revision(commitish))
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("resolving %q: %w", commitish, err)
	}

	return *hash, nil
}

// Repo returns the underlying go-git repository.
func (r *Repository) Repo() *git.Repository {
	return r.repo
}

func computeRepoID(repo *git.Repository, path string) string {
	// Try to get the origin URL first
	remote, err := repo.Remote("origin")
	if err == nil && remote != nil {
		urls := remote.Config().URLs
		if len(urls) > 0 {
			return hashString(urls[0])
		}
	}

	// Fall back to hashing the path
	return hashString(path)
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))

	return hex.EncodeToString(h[:8]) // Use first 8 bytes (16 hex chars)
}
