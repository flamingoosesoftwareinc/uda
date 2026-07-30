package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/NimbleMarkets/ntcharts/v2/canvas"
	"github.com/NimbleMarkets/ntcharts/v2/linechart"
	"github.com/NimbleMarkets/ntcharts/v2/linechart/timeserieslinechart"
)

// Chart color constants are defined in styles.go:
// colorInward, colorOutward, colorInstability, colorMarker

// chartCard displays coupling and instability charts for a single package.
type chartCard struct {
	pkgName          string
	dataPoints       []PackageDataPoint
	couplingChart    timeserieslinechart.Model
	instabilityChart timeserieslinechart.Model
	chartsBuilt      bool // whether charts have been materialized
	markerIndex      int  // cursor position on X axis (index into dataPoints)
	viewStart        int  // start index for visible range
	viewEnd          int  // end index for visible range (exclusive)
	focused          bool
	width            int
	height           int
	changeFreq       float64 // from hotspot analysis
	hotspotScore     float64 // from hotspot analysis
}

const (
	minVisiblePoints = 3
	chartHeight      = 6

	minChartCardWidth   = 40             // floor width for a chart card
	chartCardExtraRows  = 6              // title + axis + change-freq rows around the chart body
	chartBorderPadding  = 4              // cells consumed by the card border
	headerGap           = 2              // spacing between package name and SHA in the header
	placeholderReserved = 12             // cells reserved for the placeholder header suffix
	minTruncateLen      = 10             // below this, package names are hard-truncated (no ellipsis)
	chartWidthReserved  = 8              // cells reserved around the two side-by-side charts
	chartsPerRow        = 2              // inward/outward charts sit side by side
	minInnerChartWidth  = 20             // floor width for a single inner chart
	maxTimeBuffer       = 12 * time.Hour // pad the max time so the rightmost point isn't on the boundary
)

// newChartCard creates a chart card for a package with its time series data.
// Charts are NOT built eagerly; call ensureCharts() before rendering.
func newChartCard(pkgName string, dataPoints []PackageDataPoint, width int) chartCard {
	if width < minChartCardWidth {
		width = minChartCardWidth
	}

	card := chartCard{
		pkgName:    pkgName,
		dataPoints: dataPoints,
		viewStart:  0,
		viewEnd:    len(dataPoints),
		width:      width,
		height:     chartHeight + chartCardExtraRows,
	}

	// Start marker at most recent commit
	if len(dataPoints) > 0 {
		card.markerIndex = len(dataPoints) - 1
	}

	return card
}

func emptyXLabelFormatter() linechart.LabelFormatter {
	return func(_ int, _ float64) string { return "" }
}

func (c *chartCard) Update(msg tea.Msg) (chartCard, tea.Cmd) {
	if !c.focused {
		return *c, nil
	}

	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch keyMsg.String() {
		case "left", "h":
			c.moveMarker(-1)
			c.rebuildCharts()
		case "right", "l":
			c.moveMarker(1)
			c.rebuildCharts()
		case "+", "=":
			c.zoomIn()
		case "-", "_":
			c.zoomOut()
		}
	}

	return *c, nil
}

//nolint:funlen // sequential view assembly: charts + marker overlay + axis labels.
func (c *chartCard) View() string {
	c.ensureCharts()

	titleStyle := lipgloss.NewStyle().Bold(true)
	shaStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorSHA))

	// Color styles for legend
	inwardStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorInward))
	outwardStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorOutward))
	instStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorInstability))

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorBorder))

	if c.focused {
		borderStyle = borderStyle.BorderForeground(lipgloss.Color(colorTitle))
	}

	chartW := c.chartWidth()
	innerWidth := c.width - chartBorderPadding // account for border

	var b strings.Builder

	// Header row: package name (left) + marker indicator + SHA (right)
	var sha, markerIndicator string
	if len(c.dataPoints) > 0 && c.markerIndex >= 0 && c.markerIndex < len(c.dataPoints) {
		sha = c.dataPoints[c.markerIndex].SHA[:7]
		// Show position indicator: [2/5] means commit 2 of 5
		markerIndicator = fmt.Sprintf("[%d/%d] ", c.markerIndex+1, len(c.dataPoints))
	}

	markerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorMarkerFg))
	shaDisplay := markerStyle.Render(markerIndicator) + shaStyle.Render(sha)
	shaDisplayLen := len(markerIndicator) + len(sha)

	pkgDisplay := truncatePackageName(c.pkgName, innerWidth-shaDisplayLen-headerGap)
	headerLine := fmt.Sprintf("%s%s%s",
		titleStyle.Render(pkgDisplay),
		strings.Repeat(" ", max(1, innerWidth-len(pkgDisplay)-shaDisplayLen)),
		shaDisplay,
	)
	b.WriteString(headerLine)
	b.WriteString("\n")

	// Chart labels row
	couplingText := "Coupling"
	instabilityText := "Instability"

	padding := max(chartW-2-len(couplingText), 1)

	labelRow := fmt.Sprintf(
		"  %s%s    %s",
		couplingText,
		strings.Repeat(" ", padding),
		instabilityText,
	)
	b.WriteString(labelRow)
	b.WriteString("\n")

	// Render charts side by side
	couplingView := c.couplingChart.View()
	instabilityView := c.instabilityChart.View()

	couplingLines := strings.Split(couplingView, "\n")
	instabilityLines := strings.Split(instabilityView, "\n")

	maxLines := max(len(couplingLines), len(instabilityLines))

	for i := range maxLines {
		var left, right string
		if i < len(couplingLines) {
			left = couplingLines[i]
		}

		if i < len(instabilityLines) {
			right = instabilityLines[i]
		}

		b.WriteString(fmt.Sprintf("%-*s  %s\n", chartW, left, right))
	}

	// Date range row using actual commit timestamps
	visible := c.visibleDataPoints()
	if len(visible) > 0 {
		startDate := visible[0].Timestamp.Format("2006-01-02")
		endDate := visible[len(visible)-1].Timestamp.Format("2006-01-02")

		dateRangePadding := max(chartW-2-len(startDate)-len(endDate), 1)

		dateRange := fmt.Sprintf(
			"  %s%s%s",
			startDate,
			strings.Repeat(" ", dateRangePadding),
			endDate,
		)
		// Repeat for both charts side by side
		b.WriteString(fmt.Sprintf("%-*s  %s\n", chartW, dateRange, dateRange))
	}

	// Legend row with colored values
	if len(c.dataPoints) > 0 && c.markerIndex >= 0 && c.markerIndex < len(c.dataPoints) {
		dp := c.dataPoints[c.markerIndex]

		// Calculate display widths before applying styles
		inText := fmt.Sprintf("In=%d", dp.Inward)
		outText := fmt.Sprintf("Out=%d", dp.Outward)
		instText := fmt.Sprintf("%.2f", dp.Instability)

		// Display width of coupling legend: "  " + inText + " " + outText
		couplingDisplayLen := 2 + len(inText) + 1 + len(outText)

		legendPadding := max(chartW-2-couplingDisplayLen, 1)

		// Build legend with correct spacing
		couplingLegend := fmt.Sprintf("  %s %s",
			inwardStyle.Render(inText),
			outwardStyle.Render(outText),
		)
		instLegend := instStyle.Render(instText)

		legendRow := fmt.Sprintf(
			"%s%s    %s",
			couplingLegend,
			strings.Repeat(" ", legendPadding),
			instLegend,
		)
		b.WriteString(legendRow)

		// Show hotspot data when available
		if c.changeFreq > 0 || c.hotspotScore > 0 {
			dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorDim))

			b.WriteString("\n")
			b.WriteString(dimStyle.Render(
				fmt.Sprintf("  ChngFreq=%.3f  Hotspot=%.3f", c.changeFreq, c.hotspotScore),
			))
		}
	}

	return borderStyle.Render(b.String())
}

// ViewPlaceholder renders a lightweight placeholder with the same dimensions as View().
// Used for off-screen cards to save memory from chart model allocations.
func (c *chartCard) ViewPlaceholder() string {
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorBorder))

	if c.focused {
		borderStyle = borderStyle.BorderForeground(lipgloss.Color(colorTitle))
	}

	titleStyle := lipgloss.NewStyle().Bold(true)
	innerWidth := c.width - chartBorderPadding

	pkgDisplay := truncatePackageName(c.pkgName, innerWidth-placeholderReserved)

	var b strings.Builder
	b.WriteString(titleStyle.Render(pkgDisplay))
	b.WriteString("\n")

	// Fill remaining lines with empty space to maintain card height
	emptyLine := strings.Repeat(" ", innerWidth)
	for range chartHeight + 4 {
		b.WriteString(emptyLine)
		b.WriteString("\n")
	}

	return borderStyle.Render(b.String())
}

// Focus sets focus state on the card.
func (c *chartCard) Focus() {
	c.focused = true
}

// Blur removes focus from the card.
func (c *chartCard) Blur() {
	c.focused = false
}

// Resize updates the card dimensions and invalidates charts.
func (c *chartCard) Resize(width int) {
	c.width = width
	c.chartsBuilt = false
}

// MarkerDataPoint returns the data point at the current marker position.
func (c *chartCard) MarkerDataPoint() *PackageDataPoint {
	if c.markerIndex >= 0 && c.markerIndex < len(c.dataPoints) {
		return &c.dataPoints[c.markerIndex]
	}

	return nil
}

// MarkerPreviousDataPoint returns the data point before the marker.
func (c *chartCard) MarkerPreviousDataPoint() *PackageDataPoint {
	if c.markerIndex > 0 && c.markerIndex < len(c.dataPoints) {
		return &c.dataPoints[c.markerIndex-1]
	}

	return nil
}

// truncatePackageName shortens a package name to fit within maxLen.
func truncatePackageName(name string, maxLen int) string {
	if len(name) <= maxLen {
		return name
	}

	if maxLen < minTruncateLen {
		return name[:maxLen]
	}

	return ellipsis + name[len(name)-maxLen+len(ellipsis):]
}

func (c *chartCard) chartWidth() int {
	w := max((c.width-chartWidthReserved)/chartsPerRow, minInnerChartWidth)

	return w
}

// chartTime returns a time for plotting that ensures each commit gets its own column.
// Uses actual commit dates as the base, but ensures minimum spacing between commits
// so they don't overlap in the chart.
func (c *chartCard) chartTime(commitIndex int) time.Time {
	if len(c.dataPoints) == 0 {
		return time.Now()
	}

	// Use first commit's date as anchor, space commits 1 day apart
	// This preserves the general time period while ensuring distinct columns
	baseTime := c.dataPoints[0].Timestamp

	return baseTime.Add(time.Duration(commitIndex) * 24 * time.Hour)
}

// emptyXLabelFormatter suppresses the library's auto-generated date labels
// so we can render our own date range below the chart.
func (c *chartCard) createCouplingChart(width int) timeserieslinechart.Model {
	chart := timeserieslinechart.New(width, chartHeight,
		timeserieslinechart.WithXLabelFormatter(emptyXLabelFormatter()),
	)

	visible := c.visibleDataPoints()
	if len(visible) == 0 {
		return chart
	}

	// Push to default dataset (needed for SetColumnBackgroundStyle scaling)
	// Use synthetic times so each commit gets its own column
	for i, dp := range visible {
		chart.Push(timeserieslinechart.TimePoint{
			Time:  c.chartTime(c.viewStart + i),
			Value: float64(max(dp.Inward, dp.Outward)),
		})
	}

	// Push inward coupling data (blue)
	for i, dp := range visible {
		chart.PushDataSet("inward", timeserieslinechart.TimePoint{
			Time:  c.chartTime(c.viewStart + i),
			Value: float64(dp.Inward),
		})
	}

	// Push outward coupling data (orange)
	for i, dp := range visible {
		chart.PushDataSet("outward", timeserieslinechart.TimePoint{
			Time:  c.chartTime(c.viewStart + i),
			Value: float64(dp.Outward),
		})
	}

	// Set explicit time range AFTER pushing data - prevents auto-scaling to wide range
	// Add small buffer to maxTime so rightmost point isn't exactly on boundary
	minTime := c.chartTime(c.viewStart)
	maxTime := c.chartTime(c.viewStart + len(visible) - 1).Add(maxTimeBuffer)
	chart.SetViewTimeRange(minTime, maxTime)

	// Style the data sets
	chart.SetDataSetStyle("inward", lipgloss.NewStyle().Foreground(lipgloss.Color(colorInward)))
	chart.SetDataSetStyle(
		"outward",
		lipgloss.NewStyle().Foreground(lipgloss.Color(colorOutward)),
	)

	// Only draw inward and outward datasets (not default)
	chart.DrawBrailleDataSets([]string{"inward", "outward"})

	// Highlight marker column based on index position, not timestamp
	c.highlightMarkerColumn(&chart)

	return chart
}

// highlightMarkerColumn sets background color for the marker's column based on its index position.
func (c *chartCard) highlightMarkerColumn(chart *timeserieslinechart.Model) {
	if c.markerIndex < c.viewStart || c.markerIndex >= c.viewEnd {
		return
	}

	visibleCount := c.viewEnd - c.viewStart
	if visibleCount <= 0 {
		return
	}

	// Calculate column position directly from index
	relativePos := c.markerIndex - c.viewStart
	graphWidth := chart.GraphWidth()

	// Map marker position to column: rightmost marker -> rightmost column
	var col int
	if visibleCount == 1 {
		col = graphWidth / centerDivisor // Center single point
	} else {
		col = (relativePos * graphWidth) / (visibleCount - 1)
		if col >= graphWidth {
			col = graphWidth - 1
		}
	}

	// Apply background to all cells in this column
	startX := chart.Origin().X + 1 // +1 for Y axis
	drawX := startX + col
	bgStyle := lipgloss.NewStyle().Background(lipgloss.Color(colorMarker))

	for y := range chart.Origin().Y {
		cell := chart.Canvas.Cell(canvas.Point{X: drawX, Y: y})
		newStyle := cell.Style.Background(bgStyle.GetBackground())
		chart.Canvas.SetCellStyle(canvas.Point{X: drawX, Y: y}, newStyle)
	}
}

func (c *chartCard) createInstabilityChart(width int) timeserieslinechart.Model {
	chart := timeserieslinechart.New(width, chartHeight,
		timeserieslinechart.WithYRange(0, 1),
		timeserieslinechart.WithXLabelFormatter(emptyXLabelFormatter()),
	)

	visible := c.visibleDataPoints()
	if len(visible) == 0 {
		return chart
	}

	// Push instability data (purple)
	// Use synthetic times so each commit gets its own column
	for i, dp := range visible {
		chart.Push(timeserieslinechart.TimePoint{
			Time:  c.chartTime(c.viewStart + i),
			Value: dp.Instability,
		})
	}

	// Set explicit time range AFTER pushing data - prevents auto-scaling to wide range
	// Add small buffer to maxTime so rightmost point isn't exactly on boundary
	minTime := c.chartTime(c.viewStart)
	maxTime := c.chartTime(c.viewStart + len(visible) - 1).Add(maxTimeBuffer)
	chart.SetViewTimeRange(minTime, maxTime)

	chart.SetStyle(lipgloss.NewStyle().Foreground(lipgloss.Color(colorInstability)))

	chart.DrawBraille()

	// Highlight marker column based on index position, not timestamp
	c.highlightMarkerColumn(&chart)

	return chart
}

func (c *chartCard) visibleDataPoints() []PackageDataPoint {
	if len(c.dataPoints) == 0 {
		return nil
	}

	end := min(c.viewEnd, len(c.dataPoints))

	start := max(c.viewStart, 0)

	if start >= end {
		return nil
	}

	return c.dataPoints[start:end]
}

// ensureCharts builds the charts if they haven't been materialized yet.
func (c *chartCard) ensureCharts() {
	if !c.chartsBuilt {
		c.rebuildCharts()
	}
}

// rebuildCharts recreates the charts with current visible range and marker.
func (c *chartCard) rebuildCharts() {
	w := c.chartWidth()
	c.couplingChart = c.createCouplingChart(w)
	c.instabilityChart = c.createInstabilityChart(w)
	c.chartsBuilt = true
}

// releaseCharts frees the chart models to reclaim memory.
// The card can still be re-materialized via ensureCharts().
func (c *chartCard) releaseCharts() {
	c.couplingChart = timeserieslinechart.Model{}
	c.instabilityChart = timeserieslinechart.Model{}
	c.chartsBuilt = false
}

// Update handles keyboard events for the chart card.
func (c *chartCard) moveMarker(delta int) {
	newIndex := c.markerIndex + delta
	if newIndex >= 0 && newIndex < len(c.dataPoints) {
		c.markerIndex = newIndex
	}
}

func (c *chartCard) zoomIn() {
	visible := c.viewEnd - c.viewStart
	if visible <= minVisiblePoints {
		return
	}

	if c.viewStart < c.markerIndex {
		c.viewStart++
	}

	if c.viewEnd > c.markerIndex+1 && c.viewEnd-c.viewStart > minVisiblePoints {
		c.viewEnd--
	}

	c.rebuildCharts()
}

func (c *chartCard) zoomOut() {
	if c.viewStart > 0 {
		c.viewStart--
	}

	if c.viewEnd < len(c.dataPoints) {
		c.viewEnd++
	}

	c.rebuildCharts()
}

// View renders the chart card. Lazily builds charts if needed.
