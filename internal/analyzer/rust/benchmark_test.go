package rust_test

import (
	"context"
	"os"
	"runtime"
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/rust"
)

// BenchmarkRustAnalyzerLargeCodebase benchmarks the Rust analyzer against a large codebase.
// To run this benchmark, first fetch the benchmark data:
//
//	./internal/analyzer/rust/.testdata/benchmark/fetch.sh
//
// Then run:
//
//	go test -bench=BenchmarkRustAnalyzer -benchmem ./internal/analyzer/rust/...
func BenchmarkRustAnalyzerLargeCodebase(b *testing.B) {
	dir := ".testdata/benchmark/project_mono"
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		b.Skip(
			"benchmark data not available, run './internal/analyzer/rust/.testdata/benchmark/fetch.sh' to download",
		)
	}

	fsDir := os.DirFS(dir)
	analyzer := rust.RustAnalyzer()
	ctx := context.Background()

	// Report memory allocations
	b.ReportAllocs()

	// Reset timer to exclude setup time

	for b.Loop() {
		_, err := analyzer.Analyze(ctx, fsDir)
		if err != nil {
			b.Fatalf("analyze failed: %v", err)
		}
	}

	b.StopTimer()

	// Report detailed memory stats after benchmark
	reportMemStats(b)
}

// BenchmarkRustAnalyzerSmallCodebase benchmarks the Rust analyzer against a small codebase.
// This uses the existing test fixture and doesn't require external data.
func BenchmarkRustAnalyzerSmallCodebase(b *testing.B) {
	dir := ".testdata/project_workspace_three_crates"
	fsDir := os.DirFS(dir)
	analyzer := rust.RustAnalyzer()
	ctx := context.Background()

	b.ReportAllocs()

	for b.Loop() {
		_, err := analyzer.Analyze(ctx, fsDir)
		if err != nil {
			b.Fatalf("analyze failed: %v", err)
		}
	}
}

// BenchmarkRustAnalyzerMediumCodebase benchmarks with the grouped imports fixture.
func BenchmarkRustAnalyzerMediumCodebase(b *testing.B) {
	dir := ".testdata/project_crate_grouped_imports"
	fsDir := os.DirFS(dir)
	analyzer := rust.RustAnalyzer()
	ctx := context.Background()

	b.ReportAllocs()

	for b.Loop() {
		_, err := analyzer.Analyze(ctx, fsDir)
		if err != nil {
			b.Fatalf("analyze failed: %v", err)
		}
	}
}

// reportMemStats prints detailed memory statistics using runtime.MemStats.
// This provides more granular memory information than b.ReportAllocs() alone.
func reportMemStats(b *testing.B) {
	var m runtime.MemStats

	runtime.GC() // Force GC to get accurate stats
	runtime.ReadMemStats(&m)

	b.ReportMetric(float64(m.Alloc), "bytes_in_use")
	b.ReportMetric(float64(m.TotalAlloc), "bytes_total_alloc")
	b.ReportMetric(float64(m.HeapAlloc), "bytes_heap_alloc")
	b.ReportMetric(float64(m.HeapInuse), "bytes_heap_in_use")
	b.ReportMetric(float64(m.HeapObjects), "heap_objects")
	b.ReportMetric(float64(m.NumGC), "gc_cycles")
}
