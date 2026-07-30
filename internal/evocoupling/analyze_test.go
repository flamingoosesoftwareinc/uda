package evocoupling_test

import (
	"testing"
	"time"

	"github.com/flamingoosesoftwareinc/rapid"
	"github.com/flamingoosesoftwareinc/uda/internal/evocoupling"
	"github.com/sebdah/goldie/v2"
)

var day = 24 * time.Hour

func TestAnalyzeProperties(t *testing.T) {
	t.Parallel()

	t.Run("self_correlation_is_one", func(t *testing.T) {
		t.Parallel()

		rapid.Check(t, func(t *rapid.T) {
			n := rapid.IntRange(2, 10).Draw(t, "n_commits")
			commits := generateCommits(t, n, 2)
			opts := evocoupling.Options{
				Sigma:   14 * day,
				MinCorr: 0,
			}

			pairs := evocoupling.Analyze(commits, opts)

			// Find any package that appears with itself — shouldn't happen,
			// self-pairs are excluded. But all pairs should have corr <= 1.0.
			for _, p := range pairs {
				if p.PackageA == p.PackageB {
					t.Errorf("self-pair found: %s", p.PackageA)
				}
			}
		})
	})

	t.Run("symmetric", func(t *testing.T) {
		t.Parallel()

		rapid.Check(t, func(t *rapid.T) {
			n := rapid.IntRange(2, 10).Draw(t, "n_commits")
			commits := generateCommits(t, n, 3)
			opts := evocoupling.Options{
				Sigma:   14 * day,
				MinCorr: 0,
			}

			pairs := evocoupling.Analyze(commits, opts)

			// PackageA < PackageB always (canonical ordering).
			for _, p := range pairs {
				if p.PackageA >= p.PackageB {
					t.Errorf("pair not canonical: %s >= %s", p.PackageA, p.PackageB)
				}
			}
		})
	})

	t.Run("bounded_zero_to_one", func(t *testing.T) {
		t.Parallel()

		rapid.Check(t, func(t *rapid.T) {
			n := rapid.IntRange(2, 10).Draw(t, "n_commits")
			commits := generateCommits(t, n, 3)
			opts := evocoupling.Options{
				Sigma:   14 * day,
				MinCorr: 0,
			}

			pairs := evocoupling.Analyze(commits, opts)

			for _, p := range pairs {
				if p.Correlation < 0 || p.Correlation > 1.0001 {
					t.Errorf("correlation out of bounds: %s-%s = %v",
						p.PackageA, p.PackageB, p.Correlation)
				}
			}
		})
	})

	t.Run("identical_activity_perfect_correlation", func(t *testing.T) {
		t.Parallel()

		rapid.Check(t, func(t *rapid.T) {
			n := rapid.IntRange(2, 10).Draw(t, "n_commits")
			t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

			commits := make([]evocoupling.TimedPackageSet, n)
			for i := range n {
				commits[i] = evocoupling.TimedPackageSet{
					Time:     t0.Add(time.Duration(i) * day),
					Packages: map[string]struct{}{"pkgA": {}, "pkgB": {}},
				}
			}

			opts := evocoupling.Options{
				Sigma:   14 * day,
				MinCorr: 0,
			}

			pairs := evocoupling.Analyze(commits, opts)

			if len(pairs) != 1 {
				t.Fatalf("expected 1 pair, got %d", len(pairs))
			}

			if pairs[0].Correlation < 0.9999 {
				t.Errorf("identical activity should produce correlation ~1.0, got %v",
					pairs[0].Correlation)
			}
		})
	})
}

func TestAnalyzeScenarios(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		commits []evocoupling.TimedPackageSet
		opts    evocoupling.Options
	}{
		{
			name: "tightly_coupled",
			commits: []evocoupling.TimedPackageSet{
				{Time: t0, Packages: map[string]struct{}{"auth": {}, "session": {}}},
				{Time: t0.Add(1 * day), Packages: map[string]struct{}{"auth": {}, "session": {}}},
				{Time: t0.Add(2 * day), Packages: map[string]struct{}{"auth": {}, "session": {}}},
				{Time: t0.Add(3 * day), Packages: map[string]struct{}{"billing": {}}},
				{Time: t0.Add(30 * day), Packages: map[string]struct{}{"billing": {}}},
			},
			opts: evocoupling.Options{Sigma: 7 * day, MinCorr: 0},
		},
		{
			name: "uncoupled",
			commits: []evocoupling.TimedPackageSet{
				{Time: t0, Packages: map[string]struct{}{"auth": {}}},
				{Time: t0.Add(1 * day), Packages: map[string]struct{}{"auth": {}}},
				{Time: t0.Add(90 * day), Packages: map[string]struct{}{"billing": {}}},
				{Time: t0.Add(91 * day), Packages: map[string]struct{}{"billing": {}}},
			},
			opts: evocoupling.Options{Sigma: 7 * day, MinCorr: 0},
		},
		{
			name: "narrow_sigma",
			commits: []evocoupling.TimedPackageSet{
				{Time: t0, Packages: map[string]struct{}{"auth": {}, "session": {}}},
				{Time: t0.Add(3 * day), Packages: map[string]struct{}{"auth": {}}},
				{Time: t0.Add(4 * day), Packages: map[string]struct{}{"session": {}}},
			},
			opts: evocoupling.Options{Sigma: 1 * day, MinCorr: 0},
		},
		{
			name: "wide_sigma",
			commits: []evocoupling.TimedPackageSet{
				{Time: t0, Packages: map[string]struct{}{"auth": {}, "session": {}}},
				{Time: t0.Add(3 * day), Packages: map[string]struct{}{"auth": {}}},
				{Time: t0.Add(4 * day), Packages: map[string]struct{}{"session": {}}},
			},
			opts: evocoupling.Options{Sigma: 30 * day, MinCorr: 0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := goldie.New(t, goldie.WithNameSuffix(".json"))

			pairs := evocoupling.Analyze(tc.commits, tc.opts)
			g.AssertJson(t, tc.name, pairs)
		})
	}
}

// generateCommits creates random TimedPackageSets for property testing.
func generateCommits(t *rapid.T, n, maxPkgs int) []evocoupling.TimedPackageSet {
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	pkgNames := []string{"alpha", "bravo", "charlie", "delta", "echo"}

	if maxPkgs > len(pkgNames) {
		maxPkgs = len(pkgNames)
	}

	commits := make([]evocoupling.TimedPackageSet, n)

	for i := range n {
		dayOffset := rapid.IntRange(0, 365).Draw(t, "day")
		nPkgs := rapid.IntRange(1, maxPkgs).Draw(t, "n_pkgs")

		pkgs := make(map[string]struct{}, nPkgs)

		for range nPkgs {
			idx := rapid.IntRange(0, maxPkgs-1).Draw(t, "pkg_idx")
			pkgs[pkgNames[idx]] = struct{}{}
		}

		commits[i] = evocoupling.TimedPackageSet{
			Time:     t0.Add(time.Duration(dayOffset) * day),
			Packages: pkgs,
		}
	}

	return commits
}
