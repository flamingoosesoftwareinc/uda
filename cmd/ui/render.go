package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	tabHorizontalPadding     = 2 // cells of left/right padding on each tab label
	helpBoxHorizontalPadding = 2 // cells of left/right padding inside the help box
	centerDivisor            = 2 // halve the leftover space to center an overlay
)

func (m metricsModel) renderTabBar() string {
	if len(m.groups) == 0 {
		return ""
	}

	activeStyle := lipgloss.NewStyle().
		Bold(true).
		Reverse(true).
		Padding(0, tabHorizontalPadding)

	inactiveStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorInactiveFg)).
		Background(lipgloss.Color(colorInactiveBg)).
		Padding(0, tabHorizontalPadding)

	var tabs []string

	for i, g := range m.groups {
		if i == m.activeTab {
			tabs = append(tabs, activeStyle.Render(g.Language))
		} else {
			tabs = append(tabs, inactiveStyle.Render(g.Language))
		}
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}

func (m metricsModel) renderHelp() string {
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorDim))
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorError))

	if m.filterMode {
		line := "Filter: " + m.filterInput.View()
		if m.filterErr != "" {
			line += " " + errStyle.Render(m.filterErr)
		}

		line += helpStyle.Render("  enter: confirm | esc: clear")

		return line
	}

	if m.drillDown != nil {
		dd := m.drillDown

		help := "esc/backspace: back | j/k: navigate | space: expand"
		if dd.cursor >= 0 && dd.cursor < len(dd.items) && dd.items[dd.cursor].pos != nil {
			help += " | enter: open in editor"
		}

		help += " | q: quit"

		return helpStyle.Render(help)
	}

	sortKeys := "p/i/o/s"
	if m.hasHotspots {
		sortKeys = "p/i/o/s/c/h"
	}

	help := fmt.Sprintf(
		"j/k: navigate | %s: sort | /: filter | enter: details | ?: help | q: quit",
		sortKeys,
	)

	if m.filterRegex != nil {
		filtered := FilterMetrics(m.groups[m.activeTab].Metrics, m.filterRegex)
		help = fmt.Sprintf("Filter: /%s/ (%d matches) | %s",
			m.filterInput.Value(), len(filtered), help)
	}

	if len(m.groups) > 1 {
		help = "tab/shift+tab: switch language | " + help
	}

	return helpStyle.Render(help)
}

// renderHelpBox renders the help box content without positioning.
func renderHelpBox() string {
	titleStyle := lipgloss.NewStyle().Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorDim))

	lines := []string{
		titleStyle.Render("Metric Definitions"),
		"",
		"Inward       Number of packages that depend on this package",
		"Outward      Number of packages this package depends on",
		"Instability  Outward / (Inward + Outward) — 0=stable, 1=unstable",
		"Chng Freq    Fraction of commits that touched this package",
		"Hotspot      Change frequency × instability curve weight",
		"",
		dimStyle.Render("Press any key to close"),
	}

	content := strings.Join(lines, "\n")

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorHelpBoxBorder)).
		Padding(1, helpBoxHorizontalPadding)

	return boxStyle.Render(content)
}

// overlayCenter composites fg centered on top of bg, preserving bg content
// around the overlay edges. Both bg and fg are newline-delimited rendered strings.
func overlayCenter(bg, fg string, width, height int) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")

	// Pad bg to fill the full height so we always have lines to composite onto.
	for len(bgLines) < height {
		bgLines = append(bgLines, "")
	}

	// Compute the fg dimensions in cells.
	fgHeight := len(fgLines)

	fgWidth := 0
	for _, line := range fgLines {
		if w := ansi.StringWidth(line); w > fgWidth {
			fgWidth = w
		}
	}

	// Center offsets.
	startRow := (height - fgHeight) / centerDivisor
	startCol := (width - fgWidth) / centerDivisor

	if startRow < 0 {
		startRow = 0
	}

	if startCol < 0 {
		startCol = 0
	}

	for i, fgLine := range fgLines {
		row := startRow + i
		if row >= len(bgLines) {
			break
		}

		bgLine := bgLines[row]
		bgW := ansi.StringWidth(bgLine)

		// Build: left part of bg | fg line | right part of bg
		var out strings.Builder

		// Left portion of the background (before overlay).
		if startCol > 0 {
			if bgW >= startCol {
				out.WriteString(ansi.Truncate(bgLine, startCol, ""))
			} else {
				out.WriteString(bgLine)
				out.WriteString(strings.Repeat(" ", startCol-bgW))
			}
		}

		// The overlay line itself.
		out.WriteString(fgLine)

		// Right portion of the background (after overlay).
		endCol := startCol + ansi.StringWidth(fgLine)
		if bgW > endCol {
			// Skip the first endCol cells of the bg line to get the suffix.
			right := ansi.TruncateLeft(bgLine, endCol, "")
			out.WriteString(right)
		}

		bgLines[row] = out.String()
	}

	return strings.Join(bgLines[:height], "\n")
}
