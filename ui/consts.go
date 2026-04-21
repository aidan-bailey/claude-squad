package ui

// Layout proportions used across the TUI. Keeping them centralized prevents
// drift between app.go's sizing math and the component-level calculations.
const (
	// ListWidthPercent is the fraction of terminal width allotted to the
	// left-hand session list.
	ListWidthPercent = 0.2

	// SplitAgentPercent is the fraction of the right-hand pane height
	// given to the agent view; the terminal pane gets the remainder.
	SplitAgentPercent = 0.7

	// OverlayWidthPercent / OverlayHeightPercent size modal overlays
	// (text input, confirmation, pickers).
	OverlayWidthPercent  = 0.6
	OverlayHeightPercent = 0.4

	// PreviewWidthPercent sizes the preview-pane capture column.
	PreviewWidthPercent = 0.9
)

// FallBackText is the ASCII splash rendered in empty-state panes when no
// instance is selected or a newly created instance has not yet started.
var FallBackText = `
██╗░░░░░░█████╗░░█████╗░███╗░░░███╗
██║░░░░░██╔══██╗██╔══██╗████╗░████║
██║░░░░░██║░░██║██║░░██║██╔████╔██║
██║░░░░░██║░░██║██║░░██║██║╚██╔╝██║
███████╗╚█████╔╝╚█████╔╝██║░╚═╝░██║
╚══════╝░╚════╝░░╚════╝░╚═╝░░░░░╚═╝
`
