package evocoupling_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/flamingoosesoftwareinc/uda/internal/evocoupling"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyze_MIDisabled_ByteStable(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	commits := []evocoupling.TimedPackageSet{
		{Time: t0, Packages: map[string]struct{}{"auth": {}, "session": {}}},
		{Time: t0.Add(1 * day), Packages: map[string]struct{}{"auth": {}, "session": {}}},
		{Time: t0.Add(2 * day), Packages: map[string]struct{}{"auth": {}, "session": {}}},
		{Time: t0.Add(3 * day), Packages: map[string]struct{}{"billing": {}}},
	}

	pairs := evocoupling.Analyze(commits, evocoupling.Options{Sigma: 7 * day, MinCorr: 0})

	encoded, err := json.Marshal(pairs)
	require.NoError(t, err)

	// Pre-MI consumers see only the three original fields; presence of
	// the new keys in MI-off mode would be a regression.
	for _, key := range []string{"pearson_binned", "nmi", "divergence", "low_support"} {
		assert.NotContains(t, string(encoded), key,
			"omitempty contract violated for %s", key)
	}
}

func TestAnalyze_MIEnabled_PopulatesMetrics(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// 16 weeks, weekly bin width via sigma=7d. auth + session co-change
	// every other week (n_11 = 8); billing changes alone occasionally.
	commits := make([]evocoupling.TimedPackageSet, 0)

	for week := range 16 {
		base := t0.Add(time.Duration(week) * 7 * day)

		if week%2 == 0 {
			commits = append(commits, evocoupling.TimedPackageSet{
				Time:     base,
				Packages: map[string]struct{}{"auth": {}, "session": {}},
			})
		} else {
			commits = append(commits, evocoupling.TimedPackageSet{
				Time:     base,
				Packages: map[string]struct{}{"billing": {}},
			})
		}
	}

	opts := evocoupling.Options{
		Sigma:     7 * day,
		MinCorr:   0,
		MIEnabled: true,
	}

	pairs := evocoupling.Analyze(commits, opts)

	require.NotEmpty(t, pairs)

	authSession := findPair(t, pairs, "auth", "session")
	require.NotNil(t, authSession.NMI, "auth/session NMI should be populated")
	require.NotNil(t, authSession.PearsonBinned)
	require.NotNil(t, authSession.Divergence)
	assert.False(t, authSession.LowSupport,
		"auth/session co-change 8 times — above min support of 3")
	assert.GreaterOrEqual(t, *authSession.NMI, 0.0)
	assert.LessOrEqual(t, *authSession.NMI, 1.0)
}

func TestAnalyze_MIEnabled_MinSupportFlags(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// auth and session change together exactly once — n_11 = 1 < default
	// min support of 3. Both packages still appear in other commits so
	// they're in the pair enumeration.
	commits := make([]evocoupling.TimedPackageSet, 0, 16)

	for week := range 12 {
		base := t0.Add(time.Duration(week) * 7 * day)
		if week == 0 {
			commits = append(commits, evocoupling.TimedPackageSet{
				Time:     base,
				Packages: map[string]struct{}{"auth": {}, "session": {}},
			})

			continue
		}

		if week%2 == 0 {
			commits = append(commits, evocoupling.TimedPackageSet{
				Time:     base,
				Packages: map[string]struct{}{"auth": {}},
			})
		} else {
			commits = append(commits, evocoupling.TimedPackageSet{
				Time:     base,
				Packages: map[string]struct{}{"session": {}},
			})
		}
	}

	opts := evocoupling.Options{
		Sigma:     7 * day,
		MinCorr:   0,
		MIEnabled: true,
	}

	pairs := evocoupling.Analyze(commits, opts)

	authSession := findPair(t, pairs, "auth", "session")
	assert.True(t, authSession.LowSupport,
		"single co-change should flag low_support at default min=3")
}

func TestAnalyze_MIEnabled_NaNSuppressed(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Window so short that BuildPresenceMatrix yields fewer than miMinBins
	// bins — NMI / PearsonBinned must return NaN, and the JSON output
	// must omit the fields rather than emit non-marshallable NaN.
	commits := []evocoupling.TimedPackageSet{
		{Time: t0, Packages: map[string]struct{}{"auth": {}, "session": {}}},
		{Time: t0.Add(1 * day), Packages: map[string]struct{}{"auth": {}, "session": {}}},
	}

	opts := evocoupling.Options{
		Sigma:     7 * day, // bin width 7d, but only 2 days of data → 1 bin
		MinCorr:   0,
		MIEnabled: true,
	}

	pairs := evocoupling.Analyze(commits, opts)

	encoded, err := json.Marshal(pairs)
	require.NoError(t, err, "NaN-suppressed pairs must marshal cleanly")
	assert.NotContains(t, string(encoded), "NaN")

	for _, pair := range pairs {
		assert.Nil(t, pair.NMI, "low-bin pair should have NaN-suppressed NMI")
		assert.Nil(t, pair.PearsonBinned)
		assert.Nil(t, pair.Divergence)
	}
}

func TestAnalyze_MIEnabled_DivergenceComputed(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// 16 weeks; "a" and "b" co-change every other week, "c" changes
	// independently. Yields non-degenerate marginals — NMI and
	// Pearson_binned are well-defined and Divergence is non-nil.
	commits := make([]evocoupling.TimedPackageSet, 0, 16)

	for week := range 16 {
		base := t0.Add(time.Duration(week) * 7 * day)

		if week%2 == 0 {
			commits = append(commits, evocoupling.TimedPackageSet{
				Time:     base,
				Packages: map[string]struct{}{"a": {}, "b": {}},
			})
		} else {
			commits = append(commits, evocoupling.TimedPackageSet{
				Time:     base,
				Packages: map[string]struct{}{"c": {}},
			})
		}
	}

	opts := evocoupling.Options{
		Sigma:     7 * day,
		MinCorr:   0,
		MIEnabled: true,
	}

	pairs := evocoupling.Analyze(commits, opts)

	pair := findPair(t, pairs, "a", "b")
	require.NotNil(t, pair.NMI, "non-degenerate pair should populate NMI")
	require.NotNil(t, pair.PearsonBinned)
	require.NotNil(t, pair.Divergence)

	// Both metrics agree on perfect co-change → divergence near zero.
	assert.InDelta(t, *pair.NMI-*pair.PearsonBinned, *pair.Divergence, 1e-9)
}

func TestAnalyze_MIEnabled_DegenerateMarginalSuppressed(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// 16 weeks of strict co-change for a+b only — both columns are
	// all-true, marginals are degenerate, NMI/Pearson return NaN and
	// the JSON output omits the fields entirely.
	commits := make([]evocoupling.TimedPackageSet, 16)
	for week := range 16 {
		commits[week] = evocoupling.TimedPackageSet{
			Time:     t0.Add(time.Duration(week) * 7 * day),
			Packages: map[string]struct{}{"a": {}, "b": {}},
		}
	}

	opts := evocoupling.Options{
		Sigma:     7 * day,
		MinCorr:   0,
		MIEnabled: true,
	}

	pairs := evocoupling.Analyze(commits, opts)
	require.Len(t, pairs, 1)

	pair := pairs[0]
	assert.Nil(t, pair.NMI, "all-true columns are degenerate; NMI must be nil")
	assert.Nil(t, pair.PearsonBinned)
	assert.Nil(t, pair.Divergence)

	encoded, err := json.Marshal(pair)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "NaN")
}

func findPair(
	t *testing.T,
	pairs []evocoupling.CouplingPair,
	a, b string,
) evocoupling.CouplingPair {
	t.Helper()

	for _, pair := range pairs {
		if pair.PackageA == a && pair.PackageB == b {
			return pair
		}

		if pair.PackageA == b && pair.PackageB == a {
			return pair
		}
	}

	t.Fatalf("pair %s/%s not found", a, b)

	return evocoupling.CouplingPair{}
}
