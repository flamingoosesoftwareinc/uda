package evocoupling_test

import (
	"math"
	"testing"
	"time"

	"github.com/flamingoosesoftwareinc/rapid"
	"github.com/flamingoosesoftwareinc/uda/internal/evocoupling"
)

func TestGaussianProperties(t *testing.T) {
	t.Parallel()

	t.Run("zero_delta_returns_one", func(t *testing.T) {
		t.Parallel()

		rapid.Check(t, func(t *rapid.T) {
			sigma := rapid.Int64Range(1, int64(365*24*time.Hour)).Draw(t, "sigma")
			result := evocoupling.Gaussian(0, time.Duration(sigma))

			if result != 1.0 {
				t.Errorf("Gaussian(0, %v) = %v, want 1.0", time.Duration(sigma), result)
			}
		})
	})

	t.Run("symmetric", func(t *testing.T) {
		t.Parallel()

		rapid.Check(t, func(t *rapid.T) {
			delta := rapid.Int64Range(0, int64(365*24*time.Hour)).Draw(t, "delta")
			sigma := rapid.Int64Range(1, int64(365*24*time.Hour)).Draw(t, "sigma")

			pos := evocoupling.Gaussian(time.Duration(delta), time.Duration(sigma))
			neg := evocoupling.Gaussian(time.Duration(-delta), time.Duration(sigma))

			if pos != neg {
				t.Errorf("Gaussian(%v) = %v, Gaussian(%v) = %v — not symmetric",
					time.Duration(delta), pos, time.Duration(-delta), neg)
			}
		})
	})

	t.Run("bounded_zero_to_one", func(t *testing.T) {
		t.Parallel()

		rapid.Check(t, func(t *rapid.T) {
			delta := rapid.Int64Range(-int64(365*24*time.Hour), int64(365*24*time.Hour)).
				Draw(t, "delta")
			sigma := rapid.Int64Range(1, int64(365*24*time.Hour)).Draw(t, "sigma")

			result := evocoupling.Gaussian(time.Duration(delta), time.Duration(sigma))

			if result < 0 || result > 1 {
				t.Errorf("Gaussian(%v, %v) = %v, out of [0, 1]",
					time.Duration(delta), time.Duration(sigma), result)
			}
		})
	})

	t.Run("monotonically_decreasing_with_distance", func(t *testing.T) {
		t.Parallel()

		rapid.Check(t, func(t *rapid.T) {
			d1 := rapid.Int64Range(0, int64(180*24*time.Hour)).Draw(t, "d1")
			d2 := rapid.Int64Range(d1+1, int64(365*24*time.Hour)).Draw(t, "d2")
			sigma := rapid.Int64Range(1, int64(365*24*time.Hour)).Draw(t, "sigma")

			closer := evocoupling.Gaussian(time.Duration(d1), time.Duration(sigma))
			farther := evocoupling.Gaussian(time.Duration(d2), time.Duration(sigma))

			if closer < farther {
				t.Errorf("Gaussian(%v) = %v < Gaussian(%v) = %v — should decrease with distance",
					time.Duration(d1), closer, time.Duration(d2), farther)
			}
		})
	})

	t.Run("wider_sigma_higher_weight_for_same_delta", func(t *testing.T) {
		t.Parallel()

		rapid.Check(t, func(t *rapid.T) {
			delta := rapid.Int64Range(1, int64(365*24*time.Hour)).Draw(t, "delta")
			s1 := rapid.Int64Range(1, int64(180*24*time.Hour)).Draw(t, "s1")
			s2 := rapid.Int64Range(s1+1, int64(365*24*time.Hour)).Draw(t, "s2")

			narrow := evocoupling.Gaussian(time.Duration(delta), time.Duration(s1))
			wide := evocoupling.Gaussian(time.Duration(delta), time.Duration(s2))

			if wide < narrow {
				t.Errorf("wider sigma (%v) gave lower weight %v than narrow (%v) weight %v",
					time.Duration(s2), wide, time.Duration(s1), narrow)
			}
		})
	})
}

func TestGaussianKnownValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		delta time.Duration
		sigma time.Duration
		want  float64
	}{
		{"1_sigma", 14 * 24 * time.Hour, 14 * 24 * time.Hour, math.Exp(-0.5)},
		{"2_sigma", 28 * 24 * time.Hour, 14 * 24 * time.Hour, math.Exp(-2.0)},
		{"half_sigma", 7 * 24 * time.Hour, 14 * 24 * time.Hour, math.Exp(-0.125)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := evocoupling.Gaussian(tc.delta, tc.sigma)
			if math.Abs(got-tc.want) > 1e-10 {
				t.Errorf("Gaussian(%v, %v) = %v, want %v", tc.delta, tc.sigma, got, tc.want)
			}
		})
	}
}
