package ui_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/flamingoosesoftwareinc/uda/cmd/ui"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/stretchr/testify/require"
)

func isUpdate() bool {
	f := flag.Lookup("update")

	return f != nil && f.Value.String() == "true"
}

func testMetrics() []analyzer.Metrics {
	return []analyzer.Metrics{
		{
			Package: "example.com/project/main",
			Inward:  analyzer.PackageCouplingStats{},
			Outward: analyzer.PackageCouplingStats{
				"example.com/project/cmd": {
					"cmd.Execute": {Count: 1},
				},
			},
		},
		{
			Package: "example.com/project/cmd",
			Inward: analyzer.PackageCouplingStats{
				"example.com/project/main": {
					"cmd.Execute": {Count: 1},
				},
			},
			Outward: analyzer.PackageCouplingStats{
				"example.com/project/pkg/foo": {
					"foo.DoFoo": {Count: 2},
				},
				"example.com/project/pkg/bar": {
					"bar.DoBar": {Count: 1},
				},
				"fmt": {
					"fmt.Println": {Count: 3},
				},
			},
		},
		{
			Package: "example.com/project/pkg/foo",
			Inward: analyzer.PackageCouplingStats{
				"example.com/project/cmd": {
					"foo.DoFoo": {Count: 2},
				},
			},
			Outward: analyzer.PackageCouplingStats{
				"fmt": {
					"fmt.Sprintf": {Count: 1},
				},
			},
		},
		{
			Package: "example.com/project/pkg/bar",
			Inward: analyzer.PackageCouplingStats{
				"example.com/project/cmd": {
					"bar.DoBar": {Count: 1},
				},
			},
			Outward: analyzer.PackageCouplingStats{
				"fmt": {
					"fmt.Println": {Count: 1},
				},
			},
		},
	}
}

func testLanguageMetrics() []ui.LanguageMetrics {
	return []ui.LanguageMetrics{
		{
			Language: "Go",
			Metrics:  testMetrics(),
		},
	}
}

func TestMetricsJSON(t *testing.T) {
	t.Parallel()

	got, err := ui.MetricsJSON(testLanguageMetrics(), ui.DefaultSort(), nil)
	require.NoError(t, err)

	goldenPath := filepath.Join(".testdata", "metrics_json.golden")
	if isUpdate() {
		err := os.WriteFile(goldenPath, []byte(got), 0o644)
		require.NoError(t, err)

		return
	}

	expected, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "golden file not found, run with -update to generate")

	require.Equal(t, string(expected), got)
}

func TestMetricsJSONExtended(t *testing.T) {
	t.Parallel()

	got, err := ui.MetricsJSONExtended(testLanguageMetrics(), nil)
	require.NoError(t, err)

	goldenPath := filepath.Join(".testdata", "metrics_json_extended.golden")
	if isUpdate() {
		err := os.WriteFile(goldenPath, []byte(got), 0o644)
		require.NoError(t, err)

		return
	}

	expected, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "golden file not found, run with -update to generate")

	require.Equal(t, string(expected), got)
}

func TestMetricsTable(t *testing.T) {
	t.Parallel()

	got := ui.MetricsTable(testLanguageMetrics(), ui.DefaultSort(), nil)

	goldenPath := filepath.Join(".testdata", "metrics_table.golden")
	if isUpdate() {
		err := os.WriteFile(goldenPath, []byte(got), 0o644)
		require.NoError(t, err)

		return
	}

	expected, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "golden file not found, run with -update to generate")

	require.Equal(t, string(expected), got)
}
