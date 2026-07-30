package evocoupling_test

import (
	"testing"
	"time"

	"github.com/flamingoosesoftwareinc/uda/internal/evocoupling"
	"github.com/stretchr/testify/require"
)

func TestDeriveSigma(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("uniform_dense_events_pick_predictable_sigma", func(t *testing.T) {
		t.Parallel()

		// Hourly events over 180 days with loose precision (0.1 →
		// required_ess=100). For uniform density, ESS ≈ 2·σ·√π/spacing
		// → target σ ≈ 100·1h / (2·√π) ≈ 28h. The binary search should
		// land in that order of magnitude — assert a generous range to
		// keep the test resilient to grid/tolerance behaviour without
		// being trivially true.
		events := make([]time.Time, 180*24)
		for i := range events {
			events[i] = t0.Add(time.Duration(i) * time.Hour)
		}

		res := evocoupling.DeriveSigma(events, evocoupling.AutoSigmaOptions{
			TargetStddev: 0.1,
			WindowStart:  t0,
			WindowEnd:    t0.Add(180 * day),
		})

		require.False(t, res.LowConfidence, "should not be low confidence on dense input")
		require.Equal(t, evocoupling.ReasonOK, res.Reason)
		require.GreaterOrEqual(t, res.Sigma, 12*time.Hour, "sigma=%v", res.Sigma)
		require.LessOrEqual(t, res.Sigma, 7*day, "sigma=%v", res.Sigma)
		require.GreaterOrEqual(t, res.AchievedESSMedian, res.RequiredESS*0.95)
		require.Equal(t, time.Duration(0), res.DensityCap,
			"uniform cadence has no session structure — cap must not fire")
	})

	t.Run("burst_cadence_caps_sigma_at_session_scale", func(t *testing.T) {
		t.Parallel()

		// Agent cadence: 8 sessions of 60 commits at 2-minute spacing
		// (~2h per session), sessions 3 days apart. 480 commits ≥
		// required ESS 400, so WITHOUT the session cap the picker would
		// happily satisfy the precision target with a multi-day sigma
		// (reason ok) — precisely the smearing this exists to prevent.
		// The cap bounds sigma at session scale; 400 is unreachable
		// there, so the picker settles for the ESS floor instead.
		// Kill-claims: cap-not-derived mutant → reason flips to ok with
		// a wide sigma; wall-clock-grid mutant → grid points land in the
		// 3-day dead zones, even the floor is unreachable, and the
		// result collapses to window/3 with insufficient_commits.
		events := make([]time.Time, 0, 8*60)

		for session := range 8 {
			start := t0.Add(time.Duration(session) * 3 * day)
			for i := range 60 {
				events = append(events, start.Add(time.Duration(i)*2*time.Minute))
			}
		}

		res := evocoupling.DeriveSigma(events, evocoupling.AutoSigmaOptions{
			WindowStart: t0,
			WindowEnd:   t0.Add(22 * day),
		})

		require.True(t, res.LowConfidence)
		require.Equal(t, evocoupling.ReasonDensityFallback, res.Reason)
		require.GreaterOrEqual(t, res.DensityCap, time.Hour, "cap=%v", res.DensityCap)
		require.LessOrEqual(t, res.DensityCap, 4*time.Hour, "cap=%v", res.DensityCap)
		require.GreaterOrEqual(t, res.Sigma, 2*time.Minute, "sigma=%v", res.Sigma)
		require.LessOrEqual(t, res.Sigma, 4*time.Hour,
			"sigma must stay at session scale, not smear to days (sigma=%v)", res.Sigma)
		require.GreaterOrEqual(t, res.AchievedESSMedian, 30*0.95)
	})

	t.Run("burst_cadence_below_ess_floor_settles_at_session_scale", func(t *testing.T) {
		t.Parallel()

		// 3 sessions of 5 commits — session structure exists but even
		// the ESS floor of 30 is unreachable under the cap. Precision
		// is lost regardless, so the picker must keep sigma at session
		// scale (the cap) rather than smear to window/3: this is the
		// mapforge case, where window/3 turned every pair into a
		// precise 1.0 estimate of a meaningless blend. Kill-claim: a
		// mutant restoring the unconditional window/3 fallback returns
		// 3d with insufficient_commits and fails both asserts.
		events := make([]time.Time, 0, 3*5)

		for session := range 3 {
			start := t0.Add(time.Duration(session) * 3 * day)
			for i := range 5 {
				events = append(events, start.Add(time.Duration(i)*2*time.Minute))
			}
		}

		res := evocoupling.DeriveSigma(events, evocoupling.AutoSigmaOptions{
			WindowStart: t0,
			WindowEnd:   t0.Add(9 * day),
		})

		require.True(t, res.LowConfidence)
		require.Equal(t, evocoupling.ReasonDensityFallback, res.Reason)
		require.NotEqual(t, time.Duration(0), res.DensityCap)
		require.Equal(t, res.DensityCap, res.Sigma,
			"sigma should sit at the session ceiling when even the ESS floor is unreachable")
	})

	t.Run("sparse_window_triggers_low_confidence", func(t *testing.T) {
		t.Parallel()

		// 3 events spread over 90 days. Required ESS=400 is impossible.
		// Expect the fallback σ = window/3 and the insufficient_commits
		// reason.
		events := []time.Time{t0, t0.Add(30 * day), t0.Add(60 * day)}

		res := evocoupling.DeriveSigma(events, evocoupling.AutoSigmaOptions{
			WindowStart: t0,
			WindowEnd:   t0.Add(90 * day),
		})

		require.True(t, res.LowConfidence)
		require.Equal(t, evocoupling.ReasonInsufficientCommits, res.Reason)
		require.Equal(t, 30*day, res.Sigma)
	})

	t.Run("tighter_precision_monotonically_increases_sigma", func(t *testing.T) {
		t.Parallel()

		// Hourly events over 365 days. Required ESS grows as target
		// stddev shrinks — σ must grow to satisfy it. Monotonicity is
		// the picker's core contract: precision is the knob, sigma is
		// the response.
		events := make([]time.Time, 365*24)
		for i := range events {
			events[i] = t0.Add(time.Duration(i) * time.Hour)
		}

		precisions := []float64{0.20, 0.10, 0.05}

		var prev time.Duration

		for _, target := range precisions {
			res := evocoupling.DeriveSigma(events, evocoupling.AutoSigmaOptions{
				TargetStddev: target,
				WindowStart:  t0,
				WindowEnd:    t0.Add(365 * day),
			})

			require.False(t, res.LowConfidence, "target=%v should not be low confidence", target)
			require.GreaterOrEqual(t, res.Sigma, prev,
				"sigma should be non-decreasing as precision tightens (prev=%v sigma=%v target=%v)",
				prev, res.Sigma, target)

			prev = res.Sigma
		}
	})

	t.Run("sigma_clamped_to_window_upper_bound", func(t *testing.T) {
		t.Parallel()

		// Even when required ESS is achievable, σ must not exceed the
		// window length — wider than the window is meaningless. With a
		// very loose precision target the picker should settle for a
		// modest σ; with a tight target on a barely-sufficient window it
		// should ride the upper bound.
		events := make([]time.Time, 1000)
		for i := range 1000 {
			events[i] = t0.Add(time.Duration(i) * time.Hour)
		}

		window := 1000 * time.Hour

		res := evocoupling.DeriveSigma(events, evocoupling.AutoSigmaOptions{
			TargetStddev: 0.05,
			WindowStart:  t0,
			WindowEnd:    t0.Add(window),
		})

		require.LessOrEqual(t, res.Sigma, window)
		require.GreaterOrEqual(t, res.Sigma, time.Minute)
	})

	t.Run("eval_points_three_works", func(t *testing.T) {
		t.Parallel()

		events := make([]time.Time, 90)
		for i := range 90 {
			events[i] = t0.Add(time.Duration(i) * day)
		}

		res := evocoupling.DeriveSigma(events, evocoupling.AutoSigmaOptions{
			EvalPoints:  3,
			WindowStart: t0,
			WindowEnd:   t0.Add(90 * day),
		})

		require.NotEqual(t, time.Duration(0), res.Sigma)
	})

	t.Run("zero_eval_points_uses_default", func(t *testing.T) {
		t.Parallel()

		events := make([]time.Time, 90)
		for i := range 90 {
			events[i] = t0.Add(time.Duration(i) * day)
		}

		// EvalPoints=0 → default 20 (not min 3). Result should be similar
		// to the explicit-3 case but not identical (the percentile lands
		// on a denser grid).
		res := evocoupling.DeriveSigma(events, evocoupling.AutoSigmaOptions{
			WindowStart: t0,
			WindowEnd:   t0.Add(90 * day),
		})

		require.NotEqual(t, time.Duration(0), res.Sigma)
	})

	t.Run("empty_events_returns_fallback", func(t *testing.T) {
		t.Parallel()

		res := evocoupling.DeriveSigma(nil, evocoupling.AutoSigmaOptions{
			WindowStart: t0,
			WindowEnd:   t0.Add(90 * day),
		})

		require.True(t, res.LowConfidence)
		require.Equal(t, evocoupling.ReasonInsufficientCommits, res.Reason)
	})

	t.Run("zero_window_returns_fallback", func(t *testing.T) {
		t.Parallel()

		events := []time.Time{t0, t0, t0}

		res := evocoupling.DeriveSigma(events, evocoupling.AutoSigmaOptions{
			WindowStart: t0,
			WindowEnd:   t0,
		})

		require.True(t, res.LowConfidence)
	})
}
