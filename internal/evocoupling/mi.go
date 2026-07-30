package evocoupling

import (
	"math"
)

// miMinBins is the floor below which NMI / PearsonBinned / divergence are
// suppressed (return NaN). With fewer than 8 bins the 2×2 contingency is
// too sparse to learn anything — open question 4 in the design doc.
const miMinBins = 8

// MutualInformation returns the plug-in Shannon mutual information between
// two equal-length binary sequences, in bits.
//
// No bias correction is applied — this is the raw estimator. Use NMI for
// user-facing output; MutualInformation is exported for the conditional-MI
// v2 work and for tests that need the uncorrected baseline.
//
// Returns 0 when len(a) == 0, when sequences differ in length (no signal
// is defensible from mismatched inputs), or when either marginal is
// degenerate (one outcome with probability 1).
func MutualInformation(seriesA, seriesB []bool) float64 {
	if len(seriesA) != len(seriesB) || len(seriesA) == 0 {
		return 0
	}

	table := contingency(seriesA, seriesB)
	total := float64(len(seriesA))

	marginalA0 := float64(table.n00+table.n01) / total
	marginalA1 := float64(table.n10+table.n11) / total
	marginalB0 := float64(table.n00+table.n10) / total
	marginalB1 := float64(table.n01+table.n11) / total

	totalMI := miTerm(float64(table.n00)/total, marginalA0*marginalB0) +
		miTerm(float64(table.n01)/total, marginalA0*marginalB1) +
		miTerm(float64(table.n10)/total, marginalA1*marginalB0) +
		miTerm(float64(table.n11)/total, marginalA1*marginalB1)

	if totalMI < 0 {
		// Tiny negative values can appear from floating-point round-off
		// when the true MI is exactly 0; clip to the theoretical floor.
		return 0
	}

	return totalMI
}

// miTerm computes one cell's contribution to the MI sum:
// joint · log2(joint / (p_A · p_B)). Returns 0 when either factor is
// degenerate (the 0·log0 := 0 convention from the design doc).
func miTerm(joint, marginalProduct float64) float64 {
	if joint == 0 || marginalProduct == 0 {
		return 0
	}

	return joint * math.Log2(joint/marginalProduct)
}

// Entropy returns Shannon entropy H(A) of a binary sequence in bits.
// Returns 0 for empty input or degenerate distributions (all-true or
// all-false), matching the convention that 0·log0 := 0.
func Entropy(series []bool) float64 {
	if len(series) == 0 {
		return 0
	}

	trueCount := 0

	for _, v := range series {
		if v {
			trueCount++
		}
	}

	total := float64(len(series))
	pTrue := float64(trueCount) / total
	pFalse := 1 - pTrue

	return binaryEntropy(pTrue) + binaryEntropy(pFalse)
}

// binaryEntropy returns -p·log2(p) with the 0·log0 := 0 convention.
func binaryEntropy(p float64) float64 {
	if p <= 0 {
		return 0
	}

	return -p * math.Log2(p)
}

// NMI returns Miller-Madow-corrected normalized mutual information between
// two binary presence sequences. Result is in [0, 1], clipped to 0 when
// the bias correction exceeds the plug-in estimate.
//
// Normalization is by min(H(A), H(B)) per the design doc — bounded [0, 1]
// and information-symmetric (perfect correlation and perfect anti-
// correlation both yield 1).
//
// Returns NaN when:
//   - inputs are unequal-length or shorter than miMinBins (the contingency
//     is too sparse to learn from);
//   - either marginal is degenerate (min(H(A), H(B)) == 0), so the ratio
//     is undefined.
//
// The NaN return is a deliberate signal to the caller — a degenerate or
// underpowered pair carries no information about coupling. Downstream
// callers test with math.IsNaN and surface low_support.
func NMI(seriesA, seriesB []bool) float64 {
	if len(seriesA) != len(seriesB) || len(seriesA) < miMinBins {
		return math.NaN()
	}

	mi := MutualInformation(seriesA, seriesB)

	corrected := mi - millerMadowCorrection(seriesA, seriesB)
	if corrected < 0 {
		corrected = 0
	}

	hA := Entropy(seriesA)
	hB := Entropy(seriesB)
	hMin := math.Min(hA, hB)

	if hMin == 0 {
		return math.NaN()
	}

	nmi := corrected / hMin
	if nmi > 1 {
		// Round-off can push the ratio slightly above the theoretical
		// max; clip so the [0, 1] contract holds for callers.
		return 1
	}

	return nmi
}

// millerMadowCorrection is the (K_observed - 1) / (2 · N · ln 2) bias
// term, where K_observed is the count of nonzero cells in the 2×2
// contingency. Using observed nonzero cells (not max=4) avoids over-
// correction when cells are empty, per the design doc.
func millerMadowCorrection(seriesA, seriesB []bool) float64 {
	table := contingency(seriesA, seriesB)

	nonzero := nonzeroCells(table)
	if nonzero <= 1 {
		// Degenerate distribution — the correction is undefined but the
		// uncorrected MI is also exactly 0 in this case, so the clip
		// behavior in NMI handles it safely.
		return 0
	}

	bins := float64(len(seriesA))

	return float64(nonzero-1) / (2 * bins * math.Ln2)
}

func nonzeroCells(table contingencyTable) int {
	count := 0
	if table.n00 > 0 {
		count++
	}

	if table.n01 > 0 {
		count++
	}

	if table.n10 > 0 {
		count++
	}

	if table.n11 > 0 {
		count++
	}

	return count
}

// PearsonBinned returns the phi coefficient (Pearson correlation on two
// binary sequences). Result is in [-1, 1].
//
// Returns NaN when inputs are unequal-length, shorter than miMinBins, or
// when either sequence is degenerate (variance = 0). NaN propagates the
// same low-support signal as NMI for caller-side handling.
func PearsonBinned(seriesA, seriesB []bool) float64 {
	if len(seriesA) != len(seriesB) || len(seriesA) < miMinBins {
		return math.NaN()
	}

	table := contingency(seriesA, seriesB)
	total := float64(len(seriesA))

	// n_1• = n_10 + n_11; n_•1 = n_01 + n_11
	nA1 := float64(table.n10 + table.n11)
	nB1 := float64(table.n01 + table.n11)
	nA0 := total - nA1
	nB0 := total - nB1

	denomSq := nA1 * nA0 * nB1 * nB0
	if denomSq == 0 {
		return math.NaN()
	}

	numerator := float64(table.n11)*float64(table.n00) -
		float64(table.n10)*float64(table.n01)

	return numerator / math.Sqrt(denomSq)
}

// contingencyTable holds the four cell counts of a 2×2 contingency table
// over two binary sequences. n_xy = count of (a=x, b=y) bin pairs.
// Named struct (over [2][2]int) so gosec's array-bounds heuristic isn't
// false-flagged and callsites read like the design doc's n_xy notation.
type contingencyTable struct {
	n00, n01, n10, n11 int
}

// contingency builds the 2×2 contingency table for two binary sequences.
// n11 is the co-occurrence count surfaced as the min-support threshold
// input in Analyze.
func contingency(a, b []bool) contingencyTable {
	var table contingencyTable

	for i := range a {
		aTrue := a[i]
		bTrue := b[i]

		switch {
		case aTrue && bTrue:
			table.n11++
		case aTrue:
			table.n10++
		case bTrue:
			table.n01++
		default:
			table.n00++
		}
	}

	return table
}

// CoOccurrences returns n_11 — the number of bins where both sequences
// are true. Surfaced for the min-support filter in Analyze. Returns 0
// when inputs are unequal-length (no signal is defensible).
func CoOccurrences(seriesA, seriesB []bool) int {
	if len(seriesA) != len(seriesB) {
		return 0
	}

	count := 0

	for i := range seriesA {
		if seriesA[i] && seriesB[i] {
			count++
		}
	}

	return count
}
