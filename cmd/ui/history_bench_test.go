package ui

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/history"
)

// generateCommits creates realistic test data with the given number of commits and packages.
func generateCommits(numCommits, numPackages int) []history.CommitMetrics {
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	commits := make([]history.CommitMetrics, numCommits)

	for i := range numCommits {
		metrics := make([]analyzer.MetricsSummary, numPackages)
		for j := range numPackages {
			inward := (i + j) % 10
			outward := (i*2 + j) % 15
			total := float64(inward + outward)

			instability := 0.0
			if total > 0 {
				instability = float64(outward) / total
			}

			metrics[j] = analyzer.MetricsSummary{
				Package: fmt.Sprintf(
					"example.com/project/pkg/%c/%c",
					'a'+rune(j/26),
					'a'+rune(j%26),
				),
				Inward:      inward,
				Outward:     outward,
				Instability: instability,
			}
		}

		commits[i] = history.CommitMetrics{
			SHA:       fmt.Sprintf("%040x", i),
			Timestamp: baseTime.Add(time.Duration(i) * 24 * time.Hour),
			Message:   fmt.Sprintf("commit %d: update package dependencies and refactor code", i),
			Metrics: []analyzer.LanguageMetricsSummary{
				{Language: "Go", Metrics: metrics},
			},
		}
	}

	return commits
}

// generateDrillDownMetrics creates realistic full metrics for drill-down testing.
func generateDrillDownMetrics(
	numDepsPerPkg, numPositionsPerDep int,
) *analyzer.Metrics {
	inward := make(analyzer.PackageCouplingStats, numDepsPerPkg)
	outward := make(analyzer.PackageCouplingStats, numDepsPerPkg)

	for i := range numDepsPerPkg {
		pkg := analyzer.Package(fmt.Sprintf("example.com/dep/pkg%d", i))

		stats := make(analyzer.CouplingStats, 3)
		for j := range 3 {
			positions := make([]analyzer.Position, numPositionsPerDep)
			for k := range numPositionsPerDep {
				positions[k] = analyzer.Position{
					File:     fmt.Sprintf("internal/pkg%d/file%d.go", i, k/10),
					Line:     uint(k + 1),
					ColStart: 5,
					ColEnd:   20,
				}
			}

			sym := fmt.Sprintf("Type%d", j)
			stats[sym] = analyzer.CouplingStat{
				Count:     uint(numPositionsPerDep),
				Positions: positions,
			}
		}

		inward[pkg] = stats
		outward[pkg] = stats
	}

	return &analyzer.Metrics{
		Package: "example.com/project/main",
		Inward:  inward,
		Outward: outward,
	}
}

func heapAllocBytes() uint64 {
	runtime.GC()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return m.HeapAlloc
}

// BenchmarkHistoryModel_Creation benchmarks model creation with various scales.
// Run: go test -bench=BenchmarkHistoryModel_Creation -benchmem -memprofile=mem.prof ./cmd/ui/.
func BenchmarkHistoryModel_Creation(b *testing.B) {
	for _, tc := range []struct {
		name     string
		commits  int
		packages int
	}{
		{"10commits_5pkgs", 10, 5},
		{"50commits_20pkgs", 50, 20},
		{"100commits_50pkgs", 100, 50},
		{"200commits_100pkgs", 200, 100},
	} {
		commits := generateCommits(tc.commits, tc.packages)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				_ = newHistoryModel(commits, nil, nil)
			}
		})
	}
}

// BenchmarkHistoryModel_RenderCards benchmarks card rendering.
// Run: go test -bench=BenchmarkHistoryModel_RenderCards -benchmem -memprofile=mem.prof ./cmd/ui/.
func BenchmarkHistoryModel_RenderCards(b *testing.B) {
	for _, tc := range []struct {
		name     string
		commits  int
		packages int
	}{
		{"10commits_5pkgs", 10, 5},
		{"50commits_20pkgs", 50, 20},
		{"100commits_50pkgs", 100, 50},
		{"200commits_100pkgs", 200, 100},
	} {
		commits := generateCommits(tc.commits, tc.packages)
		m := newHistoryModel(commits, nil, nil)

		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				_ = m.renderCards()
			}
		})
	}
}

// BenchmarkHistoryModel_MemoryProfile measures heap memory at each stage of the pipeline.
// This is a one-shot measurement test (not iterative) for accurate heap snapshots.
// Run: go test -bench=BenchmarkHistoryModel_MemoryProfile -benchmem -memprofile=mem.prof ./cmd/ui/.
func BenchmarkHistoryModel_MemoryProfile(b *testing.B) {
	for _, tc := range []struct {
		name     string
		commits  int
		packages int
	}{
		{"50commits_20pkgs", 50, 20},
		{"200commits_100pkgs", 200, 100},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()

			// Single iteration to get accurate heap snapshot
			commits := generateCommits(tc.commits, tc.packages)

			before := heapAllocBytes()

			ts := TransformToTimeSeries(commits)
			afterTransform := heapAllocBytes()

			m := newHistoryModel(commits, nil, nil)
			afterModel := heapAllocBytes()

			_ = m.renderCards()
			afterRender := heapAllocBytes()

			_ = ts

			b.ReportMetric(float64(afterTransform-before), "transform_bytes")
			b.ReportMetric(float64(afterModel-afterTransform), "model_bytes")
			b.ReportMetric(float64(afterRender-afterModel), "render_bytes")
			b.ReportMetric(float64(afterRender-before), "total_bytes")

			// Run b.N iterations for benchmem stats
			for range b.N {
				_ = newHistoryModel(commits, nil, nil)
			}
		})
	}
}

// BenchmarkDrillDown_Creation benchmarks drill-down state creation with full metrics.
// Run: go test -bench=BenchmarkDrillDown -benchmem -memprofile=mem.prof ./cmd/ui/.
func BenchmarkDrillDown_Creation(b *testing.B) {
	for _, tc := range []struct {
		name      string
		deps      int
		positions int
	}{
		{"10deps_10pos", 10, 10},
		{"50deps_50pos", 50, 50},
		{"100deps_100pos", 100, 100},
	} {
		current := &PackageDataPoint{
			SHA:         "abc1234abc1234abc1234abc1234abc1234abc123",
			Timestamp:   time.Now(),
			Message:     "test commit",
			Inward:      tc.deps,
			Outward:     tc.deps,
			Instability: 0.5,
		}
		previous := &PackageDataPoint{
			SHA:         "def5678def5678def5678def5678def5678def567",
			Timestamp:   time.Now().Add(-24 * time.Hour),
			Message:     "previous commit",
			Inward:      tc.deps - 2,
			Outward:     tc.deps - 1,
			Instability: 0.45,
		}
		currentMetrics := generateDrillDownMetrics(tc.deps, tc.positions)
		previousMetrics := generateDrillDownMetrics(tc.deps, tc.positions)

		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				_ = newHistoryDrillDownState(
					"example.com/project/main",
					current, previous,
					currentMetrics, previousMetrics,
					80, 40,
				)
			}
		})
	}
}

// BenchmarkTransformToTimeSeries benchmarks the data transformation step.
func BenchmarkTransformToTimeSeries(b *testing.B) {
	for _, tc := range []struct {
		name     string
		commits  int
		packages int
	}{
		{"50commits_20pkgs", 50, 20},
		{"200commits_100pkgs", 200, 100},
	} {
		commits := generateCommits(tc.commits, tc.packages)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				_ = TransformToTimeSeries(commits)
			}
		})
	}
}
