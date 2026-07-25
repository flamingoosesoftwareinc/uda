package evocoupling_test

import (
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/evocoupling"
	"github.com/stretchr/testify/require"
)

func TestAdvise(t *testing.T) {
	t.Parallel()

	pair := func(a, b string, corr float64, low bool) evocoupling.CouplingPair {
		return evocoupling.CouplingPair{
			PackageA: a, PackageB: b, Correlation: corr, LowSupport: low,
		}
	}

	tests := map[string]struct {
		pairs   []evocoupling.CouplingPair
		touched map[string]bool
		opts    evocoupling.AdviseOptions
		want    []evocoupling.Advisory
	}{
		"touched_side_flags_untouched_partner": {
			pairs:   []evocoupling.CouplingPair{pair("a", "b", 0.8, false)},
			touched: map[string]bool{"a": true},
			want: []evocoupling.Advisory{
				{Touched: "a", Expected: "b", Correlation: 0.8},
			},
		},
		"both_touched_is_silent": {
			pairs:   []evocoupling.CouplingPair{pair("a", "b", 0.8, false)},
			touched: map[string]bool{"a": true, "b": true},
			want:    nil,
		},
		"neither_touched_is_silent": {
			pairs:   []evocoupling.CouplingPair{pair("a", "b", 0.8, false)},
			touched: map[string]bool{"c": true},
			want:    nil,
		},
		"symmetric_direction": {
			pairs:   []evocoupling.CouplingPair{pair("a", "b", 0.8, false)},
			touched: map[string]bool{"b": true},
			want: []evocoupling.Advisory{
				{Touched: "b", Expected: "a", Correlation: 0.8},
			},
		},
		"low_support_suppressed_by_default": {
			pairs:   []evocoupling.CouplingPair{pair("a", "b", 0.99, true)},
			touched: map[string]bool{"a": true},
			want:    nil,
		},
		"low_support_opt_in": {
			pairs:   []evocoupling.CouplingPair{pair("a", "b", 0.99, true)},
			touched: map[string]bool{"a": true},
			opts:    evocoupling.AdviseOptions{IncludeLowSupport: true},
			want: []evocoupling.Advisory{
				{Touched: "a", Expected: "b", Correlation: 0.99},
			},
		},
		"below_min_correlation_suppressed": {
			pairs:   []evocoupling.CouplingPair{pair("a", "b", 0.4, false)},
			touched: map[string]bool{"a": true},
			opts:    evocoupling.AdviseOptions{MinCorrelation: 0.6},
			want:    nil,
		},
		"sorted_by_correlation_desc": {
			pairs: []evocoupling.CouplingPair{
				pair("a", "b", 0.6, false),
				pair("a", "c", 0.9, false),
			},
			touched: map[string]bool{"a": true},
			want: []evocoupling.Advisory{
				{Touched: "a", Expected: "c", Correlation: 0.9},
				{Touched: "a", Expected: "b", Correlation: 0.6},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, evocoupling.Advise(tt.pairs, tt.touched, tt.opts))
		})
	}
}
