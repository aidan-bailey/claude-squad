package overlay

import (
	"claude-squad/config"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// WorkspacePicker is an overlay that lets the user select a workspace.
type WorkspacePicker struct {
	workspaces []config.Workspace
	cursor     int
	width      int
	currentWs  string // name of active workspace, highlighted differently
}

// NewWorkspacePicker creates a workspace picker overlay.
func NewWorkspacePicker(workspaces []config.Workspace, currentName string) *WorkspacePicker {
	return &WorkspacePicker{
		workspaces: workspaces,
		cursor:     0,
		width:      50,
		currentWs:  currentName,
	}
}

// HandleKeyPress processes navigation and selection keys.
// Returns (selected, canceled).
func (w *WorkspacePicker) HandleKeyPress(msg tea.KeyMsg) (bool, bool) {
	switch msg.String() {
	case "up", "k":
		if w.cursor > 0 {
			w.cursor--
		}
	case "down", "j":
		if w.cursor < len(w.workspaces) {
			w.cursor++
		}
	case "enter":
		return true, false
	case "esc", "q":
		return false, true
	}
	return false, false
}

// GetSelectedWorkspace returns the selected workspace, or nil if "Global" is selected.
func (w *WorkspacePicker) GetSelectedWorkspace() *config.Workspace {
	if w.cursor >= len(w.workspaces) {
		return nil // "Global" option
	}
	return &w.workspaces[w.cursor]
}

// Render renders the workspace picker overlay.
func (w *WorkspacePicker) Render() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	activeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#51bd73")).Bold(true)
	selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("#dde4f0")).Foreground(lipgloss.Color("#1a1a1a"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#777777"))

	var content string
	content += titleStyle.Render("Switch Workspace") + "\n\n"

	for i, ws := range w.workspaces {
		cursor := "  "
		if i == w.cursor {
			cursor = "> "
		}

		name := ws.Name
		if ws.Name == w.currentWs {
			name += " *"
		}

		line := fmt.Sprintf("%s%s", cursor, name)
		path := fmt.Sprintf("  %s", ws.Path)

		if i == w.cursor {
			content += selectedStyle.Render(line) + "\n"
			content += selectedStyle.Render(path) + "\n"
		} else if ws.Name == w.currentWs {
			content += activeStyle.Render(line) + "\n"
			content += pathStyle.Render(path) + "\n"
		} else {
			content += normalStyle.Render(line) + "\n"
			content += pathStyle.Render(path) + "\n"
		}
	}

	// "Global" option
	globalCursor := "  "
	if w.cursor == len(w.workspaces) {
		globalCursor = "> "
	}
	globalLine := fmt.Sprintf("%sGlobal (default)", globalCursor)
	if w.currentWs == "" {
		globalLine += " *"
	}
	if w.cursor == len(w.workspaces) {
		content += selectedStyle.Render(globalLine) + "\n"
	} else if w.currentWs == "" {
		content += activeStyle.Render(globalLine) + "\n"
	} else {
		content += normalStyle.Render(globalLine) + "\n"
	}

	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#777777"))
	content += "\n" + helpStyle.Render("↑/↓ navigate • enter select • esc cancel")

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Width(w.width)

	return border.Render(content)
}

// SetWidth sets the width of the overlay.
func (w *WorkspacePicker) SetWidth(width int) {
	w.width = width
}
