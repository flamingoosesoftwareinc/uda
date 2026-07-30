package git

import (
	"fmt"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// CommitsSince returns commits from the last d duration, oldest first.
func (r *Repository) CommitsSince(d time.Duration) ([]plumbing.Hash, error) {
	return r.CommitsWithin(d, time.Now())
}

// CommitsWithin returns commits in the window [end-d, end], oldest first.
// An explicit end keeps history-derived analyses anchored to the data
// (e.g. a review head's commit time) instead of the wall clock.
func (r *Repository) CommitsWithin(d time.Duration, end time.Time) ([]plumbing.Hash, error) {
	head, err := r.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("getting HEAD: %w", err)
	}

	since := end.Add(-d)

	logIter, err := r.repo.Log(&gogit.LogOptions{
		From:  head.Hash(),
		Since: &since,
		Until: &end,
		Order: gogit.LogOrderCommitterTime,
	})
	if err != nil {
		return nil, fmt.Errorf("getting commit log: %w", err)
	}
	defer logIter.Close()

	var hashes []plumbing.Hash

	err = logIter.ForEach(func(c *object.Commit) error {
		hashes = append(hashes, c.Hash)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("iterating commits: %w", err)
	}

	reverse(hashes)

	return hashes, nil
}
