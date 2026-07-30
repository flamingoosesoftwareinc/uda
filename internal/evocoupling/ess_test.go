package evocoupling_test

import (
	"math"
	"testing"
	"time"

	"github.com/flamingoosesoftwareinc/uda/internal/evocoupling"
	"github.com/stretchr/testify/require"
)

func TestEffectiveSampleSize(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("zero_events_returns_zero", func(t *testing.T) {
		t.Parallel()
		require.InDelta(t, 0.0, evocoupling.EffectiveSampleSize(nil, t0, 7*day), 1e-12)
	})

	t.Run("zero_sigma_returns_zero", func(t *testing.T) {
		t.Parallel()

		events := []time.Time{t0, t0.Add(day), t0.Add(2 * day)}
		require.InDelta(t, 0.0, evocoupling.EffectiveSampleSize(events, t0, 0), 1e-12)
	})

	t.Run("negative_sigma_returns_zero", func(t *testing.T) {
		t.Parallel()

		events := []time.Time{t0}
		require.InDelta(t, 0.0, evocoupling.EffectiveSampleSize(events, t0, -1*time.Hour), 1e-12)
	})

	t.Run("uniform_spacing_matches_nominal_within_5pct", func(t *testing.T) {
		t.Parallel()

		// 60 daily events centered on day 30 with σ=7d. ESS for a uniform
		// density λ over a Gaussian kernel converges to 2·σ·λ·√π — the
		// integral ratio (∫w)² / ∫w² evaluated continuously. With
		// λ=1/day, σ=7d: nominal ≈ 24.8. ±5% accommodates finite-grid
		// discretisation; the test catches off-by-constant errors in the
		// weight formula or the sum-of-squares accumulation.
		const n = 60

		sigma := 7 * day
		spacing := day

		events := make([]time.Time, n)
		for i := range n {
			events[i] = t0.Add(time.Duration(i) * spacing)
		}

		ess := evocoupling.EffectiveSampleSize(events, t0.Add(30*day), sigma)
		nominal := 2 * float64(sigma) / float64(spacing) * math.Sqrt(math.Pi)

		require.InDelta(t, nominal, ess, 0.05*nominal,
			"ess=%v nominal=%v", ess, nominal)
	})

	t.Run("cluster_dominates_sparse_tail", func(t *testing.T) {
		t.Parallel()

		// A burst of 30 events spread over a year + a single near-center
		// commit. Centered far from the burst with σ=7d. Only the single
		// commit lies in the kernel's effective support, so ESS should be
		// ≈1 even though there are 31 events total — far below nominal.
		events := make([]time.Time, 0, 31)
		events = append(events, t0)

		for i := range 30 {
			events = append(events, t0.Add(time.Duration(i+1)*10*day))
		}

		ess := evocoupling.EffectiveSampleSize(events, t0, 7*day)

		// Nominal for the full 31 events would be ~31 if all were in-kernel;
		// the realised ESS reflects only the in-kernel commits and stays
		// near 1 because the burst lies in the tail.
		require.Less(t, ess, 3.0, "ess=%v should reflect only in-kernel commits", ess)
	})

	t.Run("symmetric_under_time_reversal", func(t *testing.T) {
		t.Parallel()

		// Reflecting every event around centerAt should leave ESS
		// invariant — Gaussian weight depends only on |Δ|.
		events := []time.Time{
			t0.Add(-7 * day), t0.Add(-3 * day), t0.Add(-1 * day),
			t0.Add(2 * day), t0.Add(5 * day), t0.Add(10 * day),
		}

		reversed := make([]time.Time, len(events))
		for i, ev := range events {
			delta := ev.Sub(t0)
			reversed[i] = t0.Add(-delta)
		}

		sigma := 7 * day
		fwd := evocoupling.EffectiveSampleSize(events, t0, sigma)
		rev := evocoupling.EffectiveSampleSize(reversed, t0, sigma)

		require.InDelta(t, fwd, rev, 1e-9)
	})

	t.Run("monotonic_in_sigma_at_fixed_center", func(t *testing.T) {
		t.Parallel()

		// For a fixed set of events spread over a window wider than the
		// kernel, widening σ admits more events into the effective
		// support, so ESS rises until it saturates at the event count.
		events := make([]time.Time, 20)
		for i := range 20 {
			events[i] = t0.Add(time.Duration(i) * day)
		}

		center := t0.Add(10 * day)

		prev := evocoupling.EffectiveSampleSize(events, center, 1*day)
		for _, sigma := range []time.Duration{2 * day, 5 * day, 10 * day, 30 * day} {
			ess := evocoupling.EffectiveSampleSize(events, center, sigma)
			require.GreaterOrEqual(t, ess+1e-9, prev,
				"ess should be non-decreasing in sigma (prev=%v ess=%v sigma=%v)",
				prev, ess, sigma)

			prev = ess
		}
	})
}
