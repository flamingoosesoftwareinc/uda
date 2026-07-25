package cmd

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/evocoupling"
	"github.com/flamingoosesoftwareinc/uda/internal/git"
	"github.com/go-git/go-git/v5/plumbing"
)

// timedPackageSets maps commit history onto package boundaries: commit
// files resolve to the packages they touch, producing the TimedPackageSet
// series evocoupling analyzes. The resolver is returned alongside so
// callers can resolve further file sets (e.g. a review range's touched
// files) against the same boundaries. Both return values are nil-safe:
// an empty repo window or a tree with no boundaries yields empty sets.
func timedPackageSets(
	ctx context.Context,
	dirFS fs.FS,
	repo *git.Repository,
	hashes []plumbing.Hash,
	language, boundary string,
) ([]evocoupling.TimedPackageSet, *evocoupling.PackageResolver, error) {
	details, err := repo.CommitDetails(hashes)
	if err != nil {
		return nil, nil, fmt.Errorf("getting commit details: %w", err)
	}

	analyzers, err := selectAnalyzers(ctx, dirFS, language, boundary)
	if err != nil {
		return nil, nil, err
	}

	var boundaries []analyzer.PackageBoundary

	for _, langAnalyzer := range analyzers {
		provider, ok := langAnalyzer.(analyzer.BoundaryProvider)
		if !ok {
			continue
		}

		found, err := provider.Boundaries(ctx, dirFS)
		if err != nil {
			continue
		}

		boundaries = append(boundaries, found...)
	}

	if len(boundaries) == 0 {
		return nil, nil, nil
	}

	resolver := evocoupling.NewPackageResolver(boundaries)

	commits := make([]evocoupling.TimedPackageSet, 0, len(details))

	for _, detail := range details {
		files := make([]string, 0, len(detail.Files))
		for _, file := range detail.Files {
			files = append(files, file.Path)
		}

		pkgs := resolver.ResolveCommit(files)
		if len(pkgs) == 0 {
			continue
		}

		commits = append(commits, evocoupling.TimedPackageSet{
			Time:     detail.Timestamp,
			Packages: pkgs,
		})
	}

	return commits, resolver, nil
}
