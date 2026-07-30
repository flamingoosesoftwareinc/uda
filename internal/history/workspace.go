package history

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/flamingoosesoftwareinc/uda/internal/analysisfs"
	udagit "github.com/flamingoosesoftwareinc/uda/internal/git"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
)

// Workspace manages a cloned copy of the repository for checkout operations.
type Workspace struct {
	path string
	repo *git.Repository
}

// NewWorkspace creates a workspace by cloning the original repository to the given path.
// If the workspace already exists, it opens the existing clone.
func NewWorkspace(originalRepo *udagit.Repository, workspacePath string) (*Workspace, error) {
	existing, err := openExistingWorkspace(workspacePath, originalRepo.RootPath())
	if err != nil {
		return nil, err
	}

	if existing != nil {
		return existing, nil
	}

	// Clone from local path
	repo, err := git.PlainClone(workspacePath, false, &git.CloneOptions{
		URL: originalRepo.RootPath(),
	})
	if err != nil {
		return nil, fmt.Errorf("cloning repository: %w", err)
	}

	return &Workspace{path: workspacePath, repo: repo}, nil
}

// openExistingWorkspace opens the cached clone at workspacePath if one
// exists, removing a corrupted one (nil, nil → caller re-clones).
func openExistingWorkspace(workspacePath, rootPath string) (*Workspace, error) {
	if _, err := os.Stat(workspacePath); err != nil {
		//nolint:nilerr,nilnil // absent workspace is the fresh-clone signal, not a failure
		return nil, nil
	}

	repo, err := git.PlainOpen(workspacePath)
	if err != nil {
		// Workspace is corrupted, remove and re-clone
		if err := os.RemoveAll(workspacePath); err != nil {
			return nil, fmt.Errorf("removing corrupted workspace: %w", err)
		}

		return nil, nil //nolint:nilnil // removed corrupted workspace: caller clones fresh
	}

	if err := refreshOrigin(repo, rootPath); err != nil {
		return nil, err
	}

	return &Workspace{path: workspacePath, repo: repo}, nil
}

// refreshOrigin re-points the workspace's origin at the repository's
// current location. The clone's origin pins the absolute path it was
// cloned from; when the original repo moves on disk, fetches would hit
// the dead path forever (RepoID hashes the remote URL, so the moved repo
// still resolves to this same cached workspace).
func refreshOrigin(repo *git.Repository, rootPath string) error {
	remote, err := repo.Remote(git.DefaultRemoteName)
	if err == nil && remote != nil {
		urls := remote.Config().URLs
		if len(urls) > 0 && urls[0] == rootPath {
			return nil
		}

		if err := repo.DeleteRemote(git.DefaultRemoteName); err != nil {
			return fmt.Errorf("refreshing workspace origin: %w", err)
		}
	}

	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: git.DefaultRemoteName,
		URLs: []string{rootPath},
	})
	if err != nil {
		return fmt.Errorf("refreshing workspace origin: %w", err)
	}

	return nil
}

// Checkout checks out the given commit in the workspace.
func (w *Workspace) Checkout(sha plumbing.Hash) error {
	worktree, err := w.repo.Worktree()
	if err != nil {
		return fmt.Errorf("getting worktree: %w", err)
	}

	// Fetch the commit if we don't have it
	_, err = w.repo.CommitObject(sha)
	if err != nil {
		// Try to fetch from origin
		err = w.repo.Fetch(&git.FetchOptions{
			RemoteName: "origin",
		})
		if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return fmt.Errorf("fetching: %w", err)
		}
	}

	err = worktree.Checkout(&git.CheckoutOptions{
		Hash:  sha,
		Force: true,
	})
	if err != nil {
		return fmt.Errorf("checkout: %w", err)
	}

	return nil
}

// FS returns the workspace as an fs.FS for use with analyzers. Build and vendor
// directories are pruned via analysisfs so historical snapshots measure the
// same source surface as working-tree analysis.
func (w *Workspace) FS() fs.FS {
	return analysisfs.New(w.path, "")
}

// Path returns the filesystem path of the workspace.
func (w *Workspace) Path() string {
	return w.path
}
