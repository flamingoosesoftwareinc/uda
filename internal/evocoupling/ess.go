package evocoupling

import (
	"time"
)

// essTailCutoffSigmas drops weight beyond this many sigmas from centerAt.
// At 4σ the Gaussian weight is ≈3e-4, contributing nothing meaningful to
// either Σw or Σw². Symmetric — applies to past and future deltas.
const essTailCutoffSigmas = 4

// EffectiveSampleSize returns the Gaussian-kernel effective sample size
// for the given event timestamps, kernel bandwidth, and centering time.
//
// ESS = (Σ wᵢ)² / Σ wᵢ², where wᵢ = exp(-Δᵢ² / 2σ²) and Δᵢ = centerAt - eventᵢ.
//
// Returns 0 when events is empty, sigma is non-positive, or no event lies
// within the kernel's effective support. Documented choice: ESS at σ=0 is
// the limit of a degenerate point-mass kernel and not meaningful for the
// downstream binary search; returning 0 keeps the search bounds well-defined.
//
// Implementation note: events more than essTailCutoffSigmas × σ from
// centerAt are skipped as an O(N) optimization. The Gaussian falls off so
// fast that retaining them changes ESS by less than the 5% tolerance the
// auto-picker converges to.
func EffectiveSampleSize(
	events []time.Time,
	centerAt time.Time,
	sigma time.Duration,
) float64 {
	if len(events) == 0 || sigma <= 0 {
		return 0
	}

	cutoff := time.Duration(essTailCutoffSigmas) * sigma

	var sumW, sumW2 float64

	for _, ev := range events {
		delta := centerAt.Sub(ev)
		if delta < 0 {
			delta = -delta
		}

		if delta > cutoff {
			continue
		}

		weight := Gaussian(delta, sigma)
		sumW += weight
		sumW2 += weight * weight
	}

	if sumW2 == 0 {
		return 0
	}

	return (sumW * sumW) / sumW2
}
