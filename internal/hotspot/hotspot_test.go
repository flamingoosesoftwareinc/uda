package hotspot

import (
	"math"
	"testing"
)

func TestComputeChangeFrequencies(t *testing.T) {
	touchCounts := map[string]int{
		"pkg/a": 5,
		"pkg/b": 2,
		"pkg/c": 0,
	}

	freqs := ComputeChangeFrequencies(touchCounts, 10)

	if math.Abs(freqs["pkg/a"]-0.5) > 1e-10 {
		t.Errorf("pkg/a freq = %v, want 0.5", freqs["pkg/a"])
	}

	if math.Abs(freqs["pkg/b"]-0.2) > 1e-10 {
		t.Errorf("pkg/b freq = %v, want 0.2", freqs["pkg/b"])
	}

	if math.Abs(freqs["pkg/c"]) > 1e-10 {
		t.Errorf("pkg/c freq = %v, want 0", freqs["pkg/c"])
	}
}

func TestComputeChangeFrequencies_ZeroCommits(t *testing.T) {
	touchCounts := map[string]int{"pkg/a": 5}

	freqs := ComputeChangeFrequencies(touchCounts, 0)
	if len(freqs) != 0 {
		t.Errorf("expected empty map for 0 commits, got %v", freqs)
	}
}

func TestComputeScores(t *testing.T) {
	curve := DefaultCurve()

	instabilities := map[string]float64{
		"pkg/a": 0.5, // peak of default curve = 1.0
		"pkg/b": 0.0, // tail of default curve = 0.5
		"pkg/c": 1.0, // tail of default curve = 0.5
	}
	changeFreqs := map[string]float64{
		"pkg/a": 0.8,
		"pkg/b": 0.4,
		"pkg/c": 0.2,
	}

	scores := ComputeScores(instabilities, changeFreqs, curve)

	// pkg/a: 0.8 * 1.0 = 0.8
	if math.Abs(scores["pkg/a"].HotspotScore-0.8) > 1e-10 {
		t.Errorf("pkg/a hotspot = %v, want 0.8", scores["pkg/a"].HotspotScore)
	}

	if math.Abs(scores["pkg/a"].ChangeFreq-0.8) > 1e-10 {
		t.Errorf("pkg/a changefreq = %v, want 0.8", scores["pkg/a"].ChangeFreq)
	}

	// pkg/b: 0.4 * 0.5 = 0.2
	if math.Abs(scores["pkg/b"].HotspotScore-0.2) > 1e-10 {
		t.Errorf("pkg/b hotspot = %v, want 0.2", scores["pkg/b"].HotspotScore)
	}

	// pkg/c: 0.2 * 0.5 = 0.1
	if math.Abs(scores["pkg/c"].HotspotScore-0.1) > 1e-10 {
		t.Errorf("pkg/c hotspot = %v, want 0.1", scores["pkg/c"].HotspotScore)
	}
}

func TestComputeScores_MissingPackages(t *testing.T) {
	curve := DefaultCurve()

	instabilities := map[string]float64{
		"pkg/a": 0.5,
		"pkg/b": 0.0,
	}
	// Only pkg/a has change frequency
	changeFreqs := map[string]float64{
		"pkg/a": 0.5,
	}

	scores := ComputeScores(instabilities, changeFreqs, curve)

	// pkg/b has no change frequency, so score should be 0
	if scores["pkg/b"].HotspotScore != 0 {
		t.Errorf("pkg/b hotspot = %v, want 0", scores["pkg/b"].HotspotScore)
	}

	if scores["pkg/b"].ChangeFreq != 0 {
		t.Errorf("pkg/b changefreq = %v, want 0", scores["pkg/b"].ChangeFreq)
	}
}
