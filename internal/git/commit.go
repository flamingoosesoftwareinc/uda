package git

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// rangeParts is the number of `from..to` parts in a git commit range.
const rangeParts = 2

// CommitRange represents a range of commits to analyze.
type CommitRange struct {
	From *plumbing.Hash // nil means "from beginning"
	To   plumbing.Hash
}

// ParseCommitRange parses a commit range string.
// Supported formats:
//   - "abc123" - single commit
//   - "abc123..def456" - range from abc123 (exclusive) to def456 (inclusive)
//
//nolint:nestif // range-string vs single-commit parsing branches; both arms are bounded.
func ParseCommitRange(repo *Repository, rangeStr string) (*CommitRange, error) {
	if strings.Contains(rangeStr, "..") {
		parts := strings.SplitN(rangeStr, "..", rangeParts)
		if len(parts) != rangeParts {
			return nil, fmt.Errorf("invalid range format: %s", rangeStr)
		}

		fromStr, toStr := parts[0], parts[1]

		var from *plumbing.Hash

		if fromStr != "" {
			fromHash, err := repo.ResolveCommit(fromStr)
			if err != nil {
				return nil, fmt.Errorf("resolving from commit: %w", err)
			}

			from = &fromHash
		}

		to, err := repo.ResolveCommit(toStr)
		if err != nil {
			return nil, fmt.Errorf("resolving to commit: %w", err)
		}

		return &CommitRange{From: from, To: to}, nil
	}

	// Single commit
	to, err := repo.ResolveCommit(rangeStr)
	if err != nil {
		return nil, err
	}

	return &CommitRange{From: nil, To: to}, nil
}

// Commits returns the list of commits in the range, ordered from oldest to newest.
// For a single commit (From == nil and no ".." in original), returns just that commit.
// For a range, returns commits from From (exclusive) to To (inclusive).
// If the range is backwards (From is not an ancestor of To), it automatically flips.
func (r *Repository) Commits(commitRange *CommitRange) ([]plumbing.Hash, error) {
	// If From is nil, this was a single commit request
	if commitRange.From == nil {
		return []plumbing.Hash{commitRange.To}, nil
	}

	commits, err := r.commitsInRange(*commitRange.From, commitRange.To)
	if err != nil {
		return nil, err
	}

	// If empty, try flipping the range
	if len(commits) == 0 {
		commits, err = r.commitsInRange(commitRange.To, *commitRange.From)
		if err != nil {
			return nil, err
		}
	}

	if len(commits) == 0 {
		return nil, fmt.Errorf(
			"no commits in range %s..%s",
			commitRange.From.String()[:8],
			commitRange.To.String()[:8],
		)
	}

	return commits, nil
}

func (r *Repository) commitsInRange(from, to plumbing.Hash) ([]plumbing.Hash, error) {
	logIter, err := r.repo.Log(&git.LogOptions{
		From:  to,
		Order: git.LogOrderCommitterTime,
	})
	if err != nil {
		return nil, fmt.Errorf("getting commit log: %w", err)
	}
	defer logIter.Close()

	var commits []plumbing.Hash

	foundFrom := false

	err = logIter.ForEach(func(c *object.Commit) error {
		if c.Hash == from {
			foundFrom = true

			return errStopIteration
		}

		commits = append(commits, c.Hash)

		return nil
	})
	if err != nil && !errors.Is(err, errStopIteration) {
		return nil, fmt.Errorf("iterating commits: %w", err)
	}

	if !foundFrom {
		return nil, nil // Range doesn't work this direction
	}

	reverse(commits)

	return commits, nil
}

var errStopIteration = errors.New("stop iteration")

func reverse(hashes []plumbing.Hash) {
	for i, j := 0, len(hashes)-1; i < j; i, j = i+1, j-1 {
		hashes[i], hashes[j] = hashes[j], hashes[i]
	}
}
