package hotspot

import (
	"math"
	"testing"
)

func TestNewCatmullRomCurve_TooFewPoints(t *testing.T) {
	_, err := NewCatmullRomCurve([]ControlPoint{{X: 0, Y: 0}})
	if err == nil {
		t.Fatal("expected error for single point")
	}
}

func TestNewCatmullRomCurve_UnsortedPoints(t *testing.T) {
	_, err := NewCatmullRomCurve([]ControlPoint{{X: 1, Y: 0}, {X: 0, Y: 1}})
	if err == nil {
		t.Fatal("expected error for unsorted points")
	}
}

func TestCurve_PassesThroughControlPoints(t *testing.T) {
	points := []ControlPoint{
		{X: 0, Y: 0.5},
		{X: 0.5, Y: 1},
		{X: 1, Y: 0.5},
	}

	c, err := NewCatmullRomCurve(points)
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range points {
		got := c.Evaluate(p.X)
		if math.Abs(got-p.Y) > 1e-10 {
			t.Errorf("Evaluate(%v) = %v, want %v", p.X, got, p.Y)
		}
	}
}

func TestDefaultCurve_PeakAndTails(t *testing.T) {
	c := DefaultCurve()

	// Peak at 0.5
	peak := c.Evaluate(0.5)
	if math.Abs(peak-1.0) > 1e-10 {
		t.Errorf("peak at 0.5 = %v, want 1.0", peak)
	}

	// Tails at 0 and 1
	left := c.Evaluate(0)
	if math.Abs(left-0.5) > 1e-10 {
		t.Errorf("left tail at 0 = %v, want 0.5", left)
	}

	right := c.Evaluate(1)
	if math.Abs(right-0.5) > 1e-10 {
		t.Errorf("right tail at 1 = %v, want 0.5", right)
	}
}

func TestCurve_ClampsBeyondRange(t *testing.T) {
	c := DefaultCurve()

	if got := c.Evaluate(-1); math.Abs(got-0.5) > 1e-10 {
		t.Errorf("Evaluate(-1) = %v, want 0.5 (clamped to x=0)", got)
	}

	if got := c.Evaluate(2); math.Abs(got-0.5) > 1e-10 {
		t.Errorf("Evaluate(2) = %v, want 0.5 (clamped to x=1)", got)
	}
}

func TestCurve_TwoPoints(t *testing.T) {
	c, err := NewCatmullRomCurve([]ControlPoint{
		{X: 0, Y: 0},
		{X: 1, Y: 1},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should pass through endpoints
	if got := c.Evaluate(0); math.Abs(got) > 1e-10 {
		t.Errorf("Evaluate(0) = %v, want 0", got)
	}

	if got := c.Evaluate(1); math.Abs(got-1) > 1e-10 {
		t.Errorf("Evaluate(1) = %v, want 1", got)
	}

	// Midpoint should be roughly 0.5 for a linear-ish curve
	mid := c.Evaluate(0.5)
	if math.Abs(mid-0.5) > 0.1 {
		t.Errorf("Evaluate(0.5) = %v, want ~0.5", mid)
	}
}

func TestCurve_ManyPoints(t *testing.T) {
	points := []ControlPoint{
		{X: 0, Y: 0},
		{X: 0.25, Y: 0.5},
		{X: 0.5, Y: 1},
		{X: 0.75, Y: 0.5},
		{X: 1, Y: 0},
	}

	c, err := NewCatmullRomCurve(points)
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range points {
		got := c.Evaluate(p.X)
		if math.Abs(got-p.Y) > 1e-10 {
			t.Errorf("Evaluate(%v) = %v, want %v", p.X, got, p.Y)
		}
	}
}

func TestDefaultCurve_Symmetry(t *testing.T) {
	c := DefaultCurve()

	// Test symmetry around 0.5
	steps := 10
	for i := 0; i <= steps; i++ {
		x := float64(i) / float64(steps)
		left := c.Evaluate(x)

		right := c.Evaluate(1.0 - x)
		if math.Abs(left-right) > 1e-10 {
			t.Errorf("asymmetric: Evaluate(%v)=%v != Evaluate(%v)=%v", x, left, 1-x, right)
		}
	}
}
