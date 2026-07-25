package evocoupling_test

import (
	"math"
	"testing"

	"github.com/flamingoosesoftwareinc/rapid"
	"github.com/flamingoosesoftwareinc/uda/internal/evocoupling"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// epsilon tolerates floating-point round-off in MI / Pearson assertions.
// Tight enough to catch bias-correction off-by-one errors; loose enough to
// survive log2 / sqrt accumulator drift on 16-bin sequences.
const epsilon = 1e-9

func TestEntropy(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		seq  []bool
		want float64
	}{
		"empty":     {seq: nil, want: 0},
		"all_true":  {seq: []bool{true, true, true, true}, want: 0},
		"all_false": {seq: []bool{false, false, false, false}, want: 0},
		"uniform_binary": {
			seq:  []bool{true, false, true, false, true, false, true, false},
			want: 1,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := evocoupling.Entropy(tc.seq)
			assert.InDelta(t, tc.want, got, epsilon)
		})
	}
}

func TestMutualInformation_KnownCases(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		a, b []bool
		want float64
	}{
		// Perfect coincidence: knowing A perfectly determines B.
		// MI = H(A) = 1 bit for uniform binary.
		"perfect_correlation": {
			a:    []bool{true, false, true, false, true, false, true, false},
			b:    []bool{true, false, true, false, true, false, true, false},
			want: 1,
		},
		// Perfect anti-correlation: still perfect information.
		"perfect_anti_correlation": {
			a:    []bool{true, false, true, false, true, false, true, false},
			b:    []bool{false, true, false, true, false, true, false, true},
			want: 1,
		},
		// All-same sequences have zero entropy; MI is 0 by convention.
		"degenerate_marginal": {
			a:    []bool{true, true, true, true, true, true, true, true},
			b:    []bool{true, false, true, false, true, false, true, false},
			want: 0,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := evocoupling.MutualInformation(tc.a, tc.b)
			assert.InDelta(t, tc.want, got, epsilon)
		})
	}
}

func TestNMI_KnownCases(t *testing.T) {
	t.Parallel()

	// perfectMM8 is the Miller-Madow-corrected NMI for two perfectly-
	// correlated 8-bin binary sequences with two nonzero contingency
	// cells: 1 - 1/(2·8·ln 2) ≈ 0.9098. Surfacing the closed form keeps
	// the kill-claim explicit — bias correction silently dropped would
	// flip this back to 1.0.
	perfectMM8 := 1 - 1/(2*8*math.Ln2)

	cases := map[string]struct {
		a, b    []bool
		wantNMI float64
		isNaN   bool
	}{
		"perfect_correlation": {
			a:       []bool{true, false, true, false, true, false, true, false},
			b:       []bool{true, false, true, false, true, false, true, false},
			wantNMI: perfectMM8,
		},
		"perfect_anti_correlation": {
			a:       []bool{true, false, true, false, true, false, true, false},
			b:       []bool{false, true, false, true, false, true, false, true},
			wantNMI: perfectMM8,
		},
		"degenerate_marginal_nan": {
			a:     []bool{true, true, true, true, true, true, true, true},
			b:     []bool{true, false, true, false, true, false, true, false},
			isNaN: true,
		},
		"below_min_bins_nan": {
			a:     []bool{true, false, true, false},
			b:     []bool{true, false, true, false},
			isNaN: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := evocoupling.NMI(tc.a, tc.b)

			if tc.isNaN {
				assert.True(t, math.IsNaN(got), "expected NaN, got %v", got)

				return
			}

			assert.InDelta(t, tc.wantNMI, got, epsilon)
		})
	}
}

func TestNMI_IndependentUniform(t *testing.T) {
	t.Parallel()

	// Constructed contingency: n_00 = n_01 = n_10 = n_11 = 4 (16 bins,
	// uniform independent). Plug-in MI = 0 exactly; Miller-Madow
	// correction (K=4) brings the reported value slightly below zero, so
	// NMI clips to 0.
	a := make([]bool, 16)
	b := make([]bool, 16)

	for i := range 16 {
		a[i] = i%2 == 0
		b[i] = i%4 < 2
	}

	mi := evocoupling.MutualInformation(a, b)
	assert.InDelta(t, 0, mi, epsilon)

	nmi := evocoupling.NMI(a, b)
	assert.InDelta(t, 0, nmi, epsilon)
}

func TestPearsonBinned_KnownCases(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		a, b  []bool
		want  float64
		isNaN bool
	}{
		"perfect_correlation": {
			a:    []bool{true, false, true, false, true, false, true, false},
			b:    []bool{true, false, true, false, true, false, true, false},
			want: 1,
		},
		"perfect_anti_correlation": {
			a:    []bool{true, false, true, false, true, false, true, false},
			b:    []bool{false, true, false, true, false, true, false, true},
			want: -1,
		},
		"degenerate_nan": {
			a:     []bool{true, true, true, true, true, true, true, true},
			b:     []bool{true, false, true, false, true, false, true, false},
			isNaN: true,
		},
		"below_min_bins_nan": {
			a:     []bool{true, false, true},
			b:     []bool{true, false, true},
			isNaN: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := evocoupling.PearsonBinned(tc.a, tc.b)

			if tc.isNaN {
				assert.True(t, math.IsNaN(got))

				return
			}

			assert.InDelta(t, tc.want, got, epsilon)
		})
	}
}

func TestNMI_Properties(t *testing.T) {
	t.Parallel()

	t.Run("symmetric", func(t *testing.T) {
		t.Parallel()

		rapid.Check(t, func(t *rapid.T) {
			a, b := drawBinaryPair(t)

			ab := evocoupling.NMI(a, b)
			ba := evocoupling.NMI(b, a)

			if math.IsNaN(ab) || math.IsNaN(ba) {
				require.True(t, math.IsNaN(ab) && math.IsNaN(ba),
					"symmetric NaN: ab=%v ba=%v", ab, ba)

				return
			}

			assert.InDelta(t, ab, ba, epsilon)
		})
	})

	t.Run("bounded_zero_to_one", func(t *testing.T) {
		t.Parallel()

		rapid.Check(t, func(t *rapid.T) {
			a, b := drawBinaryPair(t)

			nmi := evocoupling.NMI(a, b)
			if math.IsNaN(nmi) {
				return
			}

			assert.GreaterOrEqual(t, nmi, 0.0)
			assert.LessOrEqual(t, nmi, 1.0)
		})
	})

	t.Run("scale_invariant_under_relabeling", func(t *testing.T) {
		t.Parallel()

		// NMI quantifies shared information; swapping true↔false in both
		// sequences relabels but preserves dependence.
		rapid.Check(t, func(t *rapid.T) {
			a, b := drawBinaryPair(t)

			aInv := negate(a)
			bInv := negate(b)

			original := evocoupling.NMI(a, b)
			relabeled := evocoupling.NMI(aInv, bInv)

			if math.IsNaN(original) || math.IsNaN(relabeled) {
				require.True(t, math.IsNaN(original) && math.IsNaN(relabeled))

				return
			}

			assert.InDelta(t, original, relabeled, epsilon)
		})
	})
}

func drawBinaryPair(t *rapid.T) ([]bool, []bool) {
	n := rapid.IntRange(8, 64).Draw(t, "n")
	a := make([]bool, n)
	b := make([]bool, n)

	for i := range n {
		a[i] = rapid.Bool().Draw(t, "a")
		b[i] = rapid.Bool().Draw(t, "b")
	}

	return a, b
}

func negate(s []bool) []bool {
	out := make([]bool, len(s))
	for i, v := range s {
		out[i] = !v
	}

	return out
}
