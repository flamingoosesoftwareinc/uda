package ui

// Standard ANSI color constants (0-15) for terminal-adaptive styling.
// These map to the user's terminal palette, adapting to light/dark themes.
const (
	// Data visualization.
	colorInward      = "4" // Blue — inward coupling
	colorOutward     = "3" // Yellow — outward coupling
	colorInstability = "5" // Magenta — instability
	colorMarker      = "8" // Bright Black — marker column background

	// UI chrome.
	colorTitle         = "13" // Bright Magenta — titles, focused borders
	colorSection       = "14" // Bright Cyan — section headers
	colorDim           = "8"  // Bright Black — help text, dim text, unfocused borders
	colorBorder        = "8"  // Bright Black — border foreground
	colorError         = "9"  // Bright Red — error messages
	colorSpinner       = "5"  // Magenta — loading spinner
	colorHelpBoxBorder = "4"  // Blue — help box border

	// Diff.
	colorAdded   = "2" // Green — added items
	colorRemoved = "1" // Red — removed items
	colorChanged = "3" // Yellow — changed count

	// Tabs.
	colorInactiveFg = "7" // White — inactive tab foreground
	colorInactiveBg = "8" // Bright Black — inactive tab background

	// Miscellaneous.
	colorPosition = "8" // Bright Black — position/line text
	colorSHA      = "7" // White — SHA display
	colorMarkerFg = "3" // Yellow — marker indicator text
)

// Fallback terminal dimensions used before the first WindowSizeMsg arrives.
const (
	defaultTermWidth  = 80
	defaultTermHeight = 24
)

// percentMultiplier converts a 0..1 fraction to a percentage for display.
const percentMultiplier = 100
