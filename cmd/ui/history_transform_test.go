package ui

import (
	"testing"
	"time"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/history"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dummyCommitMetrics() []history.CommitMetrics {
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	return []history.CommitMetrics{
		goCommit("abc1234", "Initial commit", baseTime,
			pkgSummary("example.com/project/main", 0, 2, 1.0),
			pkgSummary("example.com/project/pkg/foo", 1, 0, 0.0),
		),
		goCommit("def5678", "Add feature", baseTime.Add(24*time.Hour),
			pkgSummary("example.com/project/main", 0, 3, 1.0),
			pkgSummary("example.com/project/pkg/foo", 2, 1, 0.333),
			pkgSummary("example.com/project/pkg/bar", 1, 0, 0.0),
		),
		goCommit("ghi9012", "Refactor", baseTime.Add(48*time.Hour),
			pkgSummary("example.com/project/main", 1, 2, 0.667),
			pkgSummary("example.com/project/pkg/foo", 2, 2, 0.5),
			pkgSummary("example.com/project/pkg/bar", 1, 1, 0.5),
		),
	}
}

func multiLanguageCommitMetrics() []history.CommitMetrics {
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	return []history.CommitMetrics{
		goTSCommit("abc1234", "Initial commit", baseTime,
			[]analyzer.MetricsSummary{pkgSummary("example.com/project/main", 0, 2, 1.0)},
			[]analyzer.MetricsSummary{pkgSummary("my-app/src/App", 0, 1, 1.0)},
		),
		goTSCommit("def5678", "Add feature", baseTime.Add(24*time.Hour),
			[]analyzer.MetricsSummary{pkgSummary("example.com/project/main", 1, 2, 0.667)},
			[]analyzer.MetricsSummary{pkgSummary("my-app/src/App", 1, 2, 0.667)},
		),
	}
}

func TestTransformToTimeSeries_SingleLanguage(t *testing.T) {
	commits := dummyCommitMetrics()
	result := TransformToTimeSeries(commits)

	require.Len(t, result, 1)
	assert.Equal(t, "Go", result[0].Language)
	assert.Len(t, result[0].Series, 3) // main, foo, bar

	// Check that packages are sorted alphabetically
	assert.Equal(t, "example.com/project/main", result[0].Series[0].Package)
	assert.Equal(t, "example.com/project/pkg/bar", result[0].Series[1].Package)
	assert.Equal(t, "example.com/project/pkg/foo", result[0].Series[2].Package)

	// Check main package data points
	mainSeries := result[0].Series[0]
	require.Len(t, mainSeries.DataPoints, 3)
	assert.Equal(t, "abc1234", mainSeries.DataPoints[0].SHA)
	assert.Equal(t, 0, mainSeries.DataPoints[0].Inward)
	assert.Equal(t, 2, mainSeries.DataPoints[0].Outward)

	// Check that bar only has 2 data points (wasn't in first commit)
	barSeries := result[0].Series[1]
	require.Len(t, barSeries.DataPoints, 2)
	assert.Equal(t, "def5678", barSeries.DataPoints[0].SHA)
}

func TestTransformToTimeSeries_MultiLanguage(t *testing.T) {
	commits := multiLanguageCommitMetrics()
	result := TransformToTimeSeries(commits)

	require.Len(t, result, 2)
	assert.Equal(t, "Go", result[0].Language)
	assert.Equal(t, "TypeScript", result[1].Language)

	// Check Go series
	require.Len(t, result[0].Series, 1)
	assert.Equal(t, "example.com/project/main", result[0].Series[0].Package)

	// Check TypeScript series
	require.Len(t, result[1].Series, 1)
	assert.Equal(t, "my-app/src/App", result[1].Series[0].Package)
}

func TestTransformToTimeSeries_ChronologicalOrder(t *testing.T) {
	commits := dummyCommitMetrics()
	result := TransformToTimeSeries(commits)

	mainSeries := result[0].Series[0]

	// Verify data points are sorted oldest to newest
	for i := 1; i < len(mainSeries.DataPoints); i++ {
		assert.True(t,
			mainSeries.DataPoints[i-1].Timestamp.Before(mainSeries.DataPoints[i].Timestamp),
			"Data points should be in chronological order",
		)
	}
}

func TestTransformToTimeSeries_Empty(t *testing.T) {
	result := TransformToTimeSeries(nil)
	assert.Empty(t, result)

	result = TransformToTimeSeries([]history.CommitMetrics{})
	assert.Empty(t, result)
}

func TestGetLanguages(t *testing.T) {
	commits := multiLanguageCommitMetrics()
	langs := GetLanguages(commits)

	require.Len(t, langs, 2)
	assert.Equal(t, "Go", langs[0])
	assert.Equal(t, "TypeScript", langs[1])
}

func TestFilterByLanguage(t *testing.T) {
	commits := multiLanguageCommitMetrics()
	all := TransformToTimeSeries(commits)

	goSeries := FilterByLanguage(all, "Go")
	require.Len(t, goSeries, 1)
	assert.Equal(t, "example.com/project/main", goSeries[0].Package)

	tsSeries := FilterByLanguage(all, "TypeScript")
	require.Len(t, tsSeries, 1)
	assert.Equal(t, "my-app/src/App", tsSeries[0].Package)

	nilSeries := FilterByLanguage(all, "Rust")
	assert.Nil(t, nilSeries)
}
