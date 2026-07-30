package ui

import (
	"testing"
	"time"

	"github.com/sebdah/goldie/v2"
)

func chartTestDataPoints() []PackageDataPoint {
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	return []PackageDataPoint{
		{
			SHA:         "abc1234abc1234abc1234",
			Timestamp:   baseTime,
			Message:     "Initial commit",
			Inward:      0,
			Outward:     2,
			Instability: 1.0,
		},
		{
			SHA:         "def5678def5678def5678",
			Timestamp:   baseTime.Add(24 * time.Hour),
			Message:     "Add feature",
			Inward:      1,
			Outward:     3,
			Instability: 0.75,
		},
		{
			SHA:         "ghi9012ghi9012ghi9012",
			Timestamp:   baseTime.Add(48 * time.Hour),
			Message:     "Refactor",
			Inward:      2,
			Outward:     2,
			Instability: 0.5,
		},
		{
			SHA:         "jkl3456jkl3456jkl3456",
			Timestamp:   baseTime.Add(72 * time.Hour),
			Message:     "Fix bug",
			Inward:      2,
			Outward:     1,
			Instability: 0.33,
		},
		{
			SHA:         "mno7890mno7890mno7890",
			Timestamp:   baseTime.Add(96 * time.Hour),
			Message:     "Optimize",
			Inward:      3,
			Outward:     1,
			Instability: 0.25,
		},
	}
}

func TestChartCard_Render(t *testing.T) {
	card := newChartCard("example.com/project/main", chartTestDataPoints(), 80)

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(card.View()))
}

func TestChartCard_RenderFocused(t *testing.T) {
	card := newChartCard("example.com/project/main", chartTestDataPoints(), 80)
	card.Focus()

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(card.View()))
}

func TestChartCard_MarkerLeft(t *testing.T) {
	card := newChartCard("example.com/project/main", chartTestDataPoints(), 80)
	card.Focus()
	// Move marker left
	card.moveMarker(-1)
	card.rebuildCharts()

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(card.View()))
}

func TestChartCard_ZoomIn(t *testing.T) {
	card := newChartCard("example.com/project/main", chartTestDataPoints(), 80)
	card.Focus()
	card.zoomIn()

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(card.View()))
}

func TestChartCard_TruncateLongPackageName(t *testing.T) {
	card := newChartCard(
		"github.com/flamingoosesoftwareinc/uda/internal/analyzer/golang/very/long/package/name",
		chartTestDataPoints(),
		60,
	)

	g := goldie.New(t, goldie.WithFixtureDir("testdata"))
	g.Assert(t, t.Name(), []byte(card.View()))
}
