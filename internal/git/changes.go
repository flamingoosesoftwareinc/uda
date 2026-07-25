// Package git wraps go-git with the helpers uda needs (commit ranges, repo IDs, changed files).
package git

import (
	"fmt"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// CommitFiles holds files changed in a single commit.
type CommitFiles struct {
	SHA   string
	Files []string // Paths relative to repo root
}

// CommitChangedFiles returns the files changed in each commit.
func (r *Repository) CommitChangedFiles(hashes []plumbing.Hash) ([]CommitFiles, error) {
	result := make([]CommitFiles, 0, len(hashes))
	for _, hash := range hashes {
		files, err := r.changedFilesForCommit(hash)
		if err != nil {
			return nil, fmt.Errorf("getting changes for %s: %w", hash.String()[:8], err)
		}

		result = append(result, CommitFiles{
			SHA:   hash.String(),
			Files: files,
		})
	}

	return result, nil
}

// FileChange holds change stats for a single file.
type FileChange struct {
	Path      string
	Additions int
	Deletions int
}

// CommitDetail holds full commit info with file-level stats.
type CommitDetail struct {
	SHA       string
	Message   string
	Timestamp time.Time
	Files     []FileChange
}

// CommitDetails returns detailed commit info with file stats.
func (r *Repository) CommitDetails(hashes []plumbing.Hash) ([]CommitDetail, error) {
	result := make([]CommitDetail, 0, len(hashes))
	for _, hash := range hashes {
		commit, err := r.repo.CommitObject(hash)
		if err != nil {
			return nil, fmt.Errorf("getting commit %s: %w", hash.String()[:8], err)
		}

		stats, err := commit.Stats()
		if err != nil {
			return nil, fmt.Errorf("getting stats for %s: %w", hash.String()[:8], err)
		}

		files := make([]FileChange, 0, len(stats))
		for _, s := range stats {
			files = append(files, FileChange{
				Path:      s.Name,
				Additions: s.Addition,
				Deletions: s.Deletion,
			})
		}

		result = append(result, CommitDetail{
			SHA:       hash.String(),
			Message:   commit.Message,
			Timestamp: commit.Author.When,
			Files:     files,
		})
	}

	return result, nil
}

func (r *Repository) changedFilesForCommit(hash plumbing.Hash) ([]string, error) {
	commit, err := r.repo.CommitObject(hash)
	if err != nil {
		return nil, err
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}

	// Get parent tree (empty tree for root commits)
	var parentTree *object.Tree

	if commit.NumParents() > 0 {
		parent, err := commit.Parents().Next()
		if err != nil {
			return nil, err
		}

		parentTree, err = parent.Tree()
		if err != nil {
			return nil, err
		}
	}

	changes, err := object.DiffTree(parentTree, tree)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(changes))

	var files []string

	for _, change := range changes {
		// Collect both from and to paths (renames have different paths)
		if change.From.Name != "" {
			if _, ok := seen[change.From.Name]; !ok {
				seen[change.From.Name] = struct{}{}
				files = append(files, change.From.Name)
			}
		}

		if change.To.Name != "" {
			if _, ok := seen[change.To.Name]; !ok {
				seen[change.To.Name] = struct{}{}
				files = append(files, change.To.Name)
			}
		}
	}

	return files, nil
}

// WorkingTreeChangedFiles returns the paths (relative to the repo root)
// that differ between the working tree and HEAD, including staged and
// untracked files.
func (r *Repository) WorkingTreeChangedFiles() ([]string, error) {
	worktree, err := r.repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("getting worktree: %w", err)
	}

	status, err := worktree.Status()
	if err != nil {
		return nil, fmt.Errorf("getting worktree status: %w", err)
	}

	files := make([]string, 0, len(status))

	for path, fileStatus := range status {
		if fileStatus.Worktree == gogit.Unmodified && fileStatus.Staging == gogit.Unmodified {
			continue
		}

		files = append(files, path)
	}

	return files, nil
}
