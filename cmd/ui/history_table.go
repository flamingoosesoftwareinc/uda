package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/flamingoosesoftwareinc/uda/internal/history"
)

const (
	minChartWidth   = 60  // below this the table falls back to the default width
	printChartWidth = 100 // width used for non-interactive table printing
)

// RenderHistoryTable renders charts for all packages non-interactively.
// This is useful for debugging chart rendering without TUI interaction.
func RenderHistoryTable(commits []history.CommitMetrics, width int) string {
	if width < minChartWidth {
		width = defaultTermWidth
	}

	timeSeries := TransformToTimeSeries(commits)

	var b strings.Builder

	for _, lts := range timeSeries {
		b.WriteString(fmt.Sprintf("\n=== %s ===\n\n", lts.Language))

		for _, series := range lts.Series {
			// Create a chart card for this package
			card := newChartCard(series.Package, series.DataPoints, width)

			// Render commit info header
			b.WriteString(
				fmt.Sprintf("Package: %s (%d commits)\n", series.Package, len(series.DataPoints)),
			)

			// Show data points summary
			b.WriteString("Commits:\n")

			for i, dataPoint := range series.DataPoints {
				b.WriteString(fmt.Sprintf(
					"  %d. %s %s - In=%d Out=%d Inst=%.2f\n",
					i+1,
					dataPoint.SHA[:7],
					dataPoint.Timestamp.Format("2006-01-02"),
					dataPoint.Inward,
					dataPoint.Outward,
					dataPoint.Instability,
				))
			}

			b.WriteString("\n")

			// Render the chart card
			b.WriteString(card.View())
			b.WriteString("\n\n")
		}
	}

	return b.String()
}

// PrintHistoryTable prints charts to the given writer.
func PrintHistoryTable(w io.Writer, commits []history.CommitMetrics) {
	_, _ = fmt.Fprint(w, RenderHistoryTable(commits, printChartWidth))
}
