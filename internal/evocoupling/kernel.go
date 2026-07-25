package evocoupling

import (
	"math"
	"time"
)

// KernelFunc computes the weight for a time delta given a bandwidth (sigma).
// Both are durations. Returns a value in [0, 1] where 0 = no influence,
// 1 = maximum influence (delta = 0).
type KernelFunc func(delta, sigma time.Duration) float64

// Gaussian kernel: exp(-(delta^2) / (2 * sigma^2)).
func Gaussian(delta, sigma time.Duration) float64 {
	d := float64(delta)
	s := float64(sigma)

	return math.Exp(-(d * d) / (2 * s * s))
}
