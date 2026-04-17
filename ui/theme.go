package ui

import "github.com/charmbracelet/lipgloss"

// Shared color palette. Referenced by style vars across ui/ and app/ so the
// theme can be retuned in one place.
var (
	// BorderActive is the accent color used for focused borders, highlighted
	// tabs, and inline attach hints.
	BorderActive = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}

	// BorderMuted is the color for unfocused borders and muted chrome.
	BorderMuted = lipgloss.AdaptiveColor{Light: "#999999", Dark: "#555555"}

	// TextDim is for secondary labels, hints, and idle text elements.
	TextDim = lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"}
)
