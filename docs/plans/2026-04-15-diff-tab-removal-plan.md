# Diff Tab Removal — Split Pane Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the tabbed right panel with a split pane showing Agent (preview) and Terminal simultaneously, with diff as a hotkey-toggled overlay.

**Architecture:** Delete `TabbedWindow`, create `SplitPane` with agent (70% height) and terminal (30% height) stacked vertically. Diff is rendered as a full overlay on `d` press. Left panel width changes from 30% to 20%.

**Tech Stack:** Go, Bubble Tea (lipgloss for layout), existing `PreviewPane`, `DiffPane`, `TerminalPane` components.

---

### Task 1: Add KeyDiff binding

**Files:**
- Modify: `keys/keys.go`

**Step 1: Add KeyDiff constant**

In the `const` block, add `KeyDiff` after `KeyFullScreenAttach`:

```go
KeyFullScreenAttach // Key for full-screen attach (existing attach behavior)
KeyDiff             // Key for toggling diff overlay
```

**Step 2: Add key mapping**

In `GlobalKeyStringsMap`, add:

```go
"d": KeyDiff,
```

**Step 3: Add key binding**

In `GlobalkeyBindings`, add before the special keybindings comment:

```go
KeyDiff: key.NewBinding(
    key.WithKeys("d"),
    key.WithHelp("d", "diff"),
),
```

**Step 4: Verify it compiles**

Run: `CGO_ENABLED=0 go build -o /dev/null`
Expected: success

**Step 5: Commit**

```bash
git add keys/keys.go
git commit -m "feat(keys): add KeyDiff binding for d key"
```

---

### Task 2: Create SplitPane component

**Files:**
- Create: `ui/split_pane.go`

**Step 1: Write split_pane.go**

```go
package ui

import (
	"claude-squad/log"
	"claude-squad/session"

	"github.com/charmbracelet/lipgloss"
)

const (
	AgentPane    int = iota // top pane
	TerminalPane            // bottom pane
)

var (
	splitPaneBorder = lipgloss.NewStyle().
		BorderForeground(highlightColor).
		Border(lipgloss.NormalBorder(), false, true, true, true)
	separatorStyle = lipgloss.NewStyle().
		Foreground(highlightColor)
	focusedSeparatorStyle = lipgloss.NewStyle().
		Foreground(highlightColor).
		Bold(true)
	diffOverlayTitleStyle = lipgloss.NewStyle().
		Foreground(highlightColor).
		Bold(true)
)

// SplitPane displays agent (preview) and terminal panes stacked vertically,
// with an optional diff overlay triggered by hotkey.
type SplitPane struct {
	agent    *PreviewPane
	terminal *TerminalPane
	diff     *DiffPane

	focusedPane int
	diffVisible bool

	height int
	width  int

	instance *session.Instance
}

func NewSplitPane(agent *PreviewPane, diff *DiffPane, terminal *TerminalPane) *SplitPane {
	return &SplitPane{
		agent:       agent,
		diff:        diff,
		terminal:    terminal,
		focusedPane: AgentPane,
	}
}

func (s *SplitPane) SetInstance(instance *session.Instance) {
	s.instance = instance
}

func (s *SplitPane) SetSize(width, height int) {
	s.width = AdjustPreviewWidth(width)
	s.height = height

	contentWidth := s.width - splitPaneBorder.GetHorizontalFrameSize()
	borderV := splitPaneBorder.GetVerticalFrameSize()

	// 1 line for the separator between panes
	separatorHeight := 1
	availableHeight := height - borderV - separatorHeight

	// 70/30 split
	agentHeight := int(float64(availableHeight) * 0.7)
	terminalHeight := availableHeight - agentHeight

	s.agent.SetSize(contentWidth, agentHeight)
	s.terminal.SetSize(contentWidth, terminalHeight)

	// Diff overlay gets the full content area
	s.diff.SetSize(contentWidth, height-borderV)
}

func (s *SplitPane) GetAgentSize() (width, height int) {
	return s.agent.width, s.agent.height
}

// ToggleFocus swaps focus between agent and terminal panes.
func (s *SplitPane) ToggleFocus() {
	if s.focusedPane == AgentPane {
		s.focusedPane = TerminalPane
	} else {
		s.focusedPane = AgentPane
	}
}

// ToggleDiff shows or hides the diff overlay.
func (s *SplitPane) ToggleDiff() {
	s.diffVisible = !s.diffVisible
}

// IsDiffVisible returns true if the diff overlay is currently shown.
func (s *SplitPane) IsDiffVisible() bool {
	return s.diffVisible
}

// GetFocusedPane returns the currently focused pane constant.
func (s *SplitPane) GetFocusedPane() int {
	return s.focusedPane
}

// UpdateAgent updates the agent (preview) pane content. Always updates since it's always visible.
func (s *SplitPane) UpdateAgent(instance *session.Instance) error {
	return s.agent.UpdateContent(instance)
}

// UpdateDiff updates the diff pane content. Only updates when the overlay is visible.
func (s *SplitPane) UpdateDiff(instance *session.Instance) {
	if !s.diffVisible {
		return
	}
	s.diff.SetDiff(instance)
}

// UpdateTerminal updates the terminal pane content. Always updates since it's always visible.
func (s *SplitPane) UpdateTerminal(instance *session.Instance) error {
	return s.terminal.UpdateContent(instance)
}

// ResetAgentToNormalMode resets the agent pane to normal mode.
func (s *SplitPane) ResetAgentToNormalMode(instance *session.Instance) error {
	return s.agent.ResetToNormalMode(instance)
}

func (s *SplitPane) ScrollUp() {
	if s.diffVisible {
		s.diff.ScrollUp()
		return
	}
	switch s.focusedPane {
	case AgentPane:
		if err := s.agent.ScrollUp(s.instance); err != nil {
			log.InfoLog.Printf("split pane failed to scroll agent up: %v", err)
		}
	case TerminalPane:
		if err := s.terminal.ScrollUp(); err != nil {
			log.InfoLog.Printf("split pane failed to scroll terminal up: %v", err)
		}
	}
}

func (s *SplitPane) ScrollDown() {
	if s.diffVisible {
		s.diff.ScrollDown()
		return
	}
	switch s.focusedPane {
	case AgentPane:
		if err := s.agent.ScrollDown(s.instance); err != nil {
			log.InfoLog.Printf("split pane failed to scroll agent down: %v", err)
		}
	case TerminalPane:
		if err := s.terminal.ScrollDown(); err != nil {
			log.InfoLog.Printf("split pane failed to scroll terminal down: %v", err)
		}
	}
}

// IsAgentInScrollMode returns true if the agent pane is in scroll mode.
func (s *SplitPane) IsAgentInScrollMode() bool {
	return s.agent.isScrolling
}

// IsTerminalInScrollMode returns true if the terminal pane is in scroll mode.
func (s *SplitPane) IsTerminalInScrollMode() bool {
	return s.terminal.IsScrolling()
}

// ResetTerminalToNormalMode exits scroll mode on the terminal pane.
func (s *SplitPane) ResetTerminalToNormalMode() {
	s.terminal.ResetToNormalMode()
}

// AttachTerminal attaches to the terminal tmux session.
func (s *SplitPane) AttachTerminal() (chan struct{}, error) {
	return s.terminal.Attach()
}

// CleanupTerminal closes the terminal session.
func (s *SplitPane) CleanupTerminal() {
	s.terminal.Close()
}

// CleanupTerminalForInstance closes the cached terminal session for the given instance title.
func (s *SplitPane) CleanupTerminalForInstance(title string) {
	s.terminal.CloseForInstance(title)
}

// SendTerminalPrompt sends text followed by Enter to the terminal pane's tmux session.
func (s *SplitPane) SendTerminalPrompt(text string) error {
	return s.terminal.SendPrompt(text)
}

func (s *SplitPane) String() string {
	if s.width == 0 || s.height == 0 {
		return ""
	}

	borderV := splitPaneBorder.GetVerticalFrameSize()

	if s.diffVisible {
		// Render diff overlay filling the full area
		diffContent := s.diff.String()
		title := diffOverlayTitleStyle.Render(" Diff ") +
			lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#808080", Dark: "#808080"}).
				Render("(d/Esc to close)")
		window := splitPaneBorder.Render(
			lipgloss.Place(
				s.width-splitPaneBorder.GetHorizontalFrameSize(),
				s.height-borderV,
				lipgloss.Left, lipgloss.Top,
				diffContent))
		return lipgloss.JoinVertical(lipgloss.Left, title, window)
	}

	// Render both panes stacked
	agentContent := s.agent.String()
	terminalContent := s.terminal.String()

	// Build separator line with focus indicator
	sep := s.buildSeparator()

	// Stack: agent, separator, terminal
	stacked := lipgloss.JoinVertical(lipgloss.Left,
		agentContent,
		sep,
		terminalContent,
	)

	window := splitPaneBorder.Render(
		lipgloss.Place(
			s.width-splitPaneBorder.GetHorizontalFrameSize(),
			s.height-borderV,
			lipgloss.Left, lipgloss.Top,
			stacked))

	return lipgloss.JoinVertical(lipgloss.Left, "\n", window)
}

// buildSeparator creates the horizontal separator between panes with a focus label.
func (s *SplitPane) buildSeparator() string {
	contentWidth := s.width - splitPaneBorder.GetHorizontalFrameSize()

	var label string
	if s.focusedPane == TerminalPane {
		label = " Terminal "
	} else {
		label = " Agent "
	}

	labelRendered := focusedSeparatorStyle.Render(label)
	labelWidth := lipgloss.Width(labelRendered)

	remaining := contentWidth - labelWidth
	if remaining < 0 {
		remaining = 0
	}
	leftLen := 2
	rightLen := remaining - leftLen
	if rightLen < 0 {
		rightLen = 0
		leftLen = remaining
	}

	left := separatorStyle.Render(repeatChar('─', leftLen))
	right := separatorStyle.Render(repeatChar('─', rightLen))

	return left + labelRendered + right
}

func repeatChar(ch rune, n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(ch)
	}
	return string(b)
}
```

**Step 2: Verify it compiles**

Run: `CGO_ENABLED=0 go build -o /dev/null`
Expected: success (SplitPane is not yet referenced by app.go, but it should compile independently)

**Step 3: Commit**

```bash
git add ui/split_pane.go
git commit -m "feat(ui): add SplitPane component for agent+terminal layout"
```

---

### Task 3: Update menu for new layout

**Files:**
- Modify: `ui/menu.go`

**Step 1: Replace SetActiveTab with SetFocusedPane and update addInstanceOptions**

Replace the `SetActiveTab` method:

```go
// SetActiveTab updates the currently active tab
func (m *Menu) SetActiveTab(tab int) {
	m.activeTab = tab
	m.updateOptions()
}
```

with:

```go
// SetFocusedPane updates the currently focused pane
func (m *Menu) SetFocusedPane(pane int) {
	m.activeTab = pane
	m.updateOptions()
}
```

In `addInstanceOptions`, replace the navigation group block:

```go
// Navigation group (when in diff tab)
if m.activeTab == DiffTab || m.activeTab == TerminalTab {
    actionGroup = append(actionGroup, keys.KeyShiftUp)
}
```

with:

```go
// Scroll hint is always relevant now (both panes visible)
actionGroup = append(actionGroup, keys.KeyShiftUp)
```

In the system group, replace:

```go
systemGroup := []keys.KeyName{keys.KeyTab, keys.KeyHelp, keys.KeyQuit}
```

with:

```go
systemGroup := []keys.KeyName{keys.KeyTab, keys.KeyDiff, keys.KeyHelp, keys.KeyQuit}
```

This requires `keys.KeyDiff` from Task 1.

**Step 2: Verify it compiles**

Run: `CGO_ENABLED=0 go build -o /dev/null`
Expected: success

**Step 3: Commit**

```bash
git add ui/menu.go
git commit -m "feat(ui): update menu for split pane layout with diff hotkey"
```

---

### Task 4: Update app.go to use SplitPane

**Files:**
- Modify: `app/app.go`

This is the largest task. It's mechanical: replace every `tabbedWindow` reference with `splitPane` and update the logic.

**Step 1: Update workspaceSlot struct (line ~79-86)**

Replace:

```go
type workspaceSlot struct {
	wsCtx        *config.WorkspaceContext
	storage      *session.Storage
	appConfig    *config.Config
	appState     config.AppState
	list         *ui.List
	tabbedWindow *ui.TabbedWindow
}
```

with:

```go
type workspaceSlot struct {
	wsCtx     *config.WorkspaceContext
	storage   *session.Storage
	appConfig *config.Config
	appState  config.AppState
	list      *ui.List
	splitPane *ui.SplitPane
}
```

**Step 2: Update home struct (line ~124)**

Replace:

```go
// tabbedWindow displays the tabbed window with preview and diff panes
tabbedWindow *ui.TabbedWindow
```

with:

```go
// splitPane displays the agent and terminal panes with diff overlay
splitPane *ui.SplitPane
```

**Step 3: Update newHome constructor (line ~187)**

Replace:

```go
tabbedWindow: ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane()),
```

with:

```go
splitPane: ui.NewSplitPane(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane()),
```

**Step 4: Update updateHandleWindowSizeEvent (lines ~276-312)**

Replace the width calculation:

```go
// List takes 30% of width, preview takes 70%
listWidth := int(float32(msg.Width) * 0.3)
tabsWidth := msg.Width - listWidth
```

with:

```go
// List takes 20% of width, split pane takes 80%
listWidth := int(float32(msg.Width) * 0.2)
paneWidth := msg.Width - listWidth
```

Update quick input width and split pane sizing — replace:

```go
	m.quickInputBar.SetWidth(ui.AdjustPreviewWidth(tabsWidth))
```
with:
```go
	m.quickInputBar.SetWidth(ui.AdjustPreviewWidth(paneWidth))
```

Replace:

```go
m.tabbedWindow.SetSize(tabsWidth, contentHeight-quickInputHeight)
```

with:

```go
m.splitPane.SetSize(paneWidth, contentHeight-quickInputHeight)
```

Replace:

```go
previewWidth, previewHeight := m.tabbedWindow.GetPreviewSize()
if err := m.list.SetSessionPreviewSize(previewWidth, previewHeight); err != nil {
```

with:

```go
agentWidth, agentHeight := m.splitPane.GetAgentSize()
if err := m.list.SetSessionPreviewSize(agentWidth, agentHeight); err != nil {
```

**Step 5: Update mouse scroll events (lines ~474-476)**

Replace:

```go
case tea.MouseButtonWheelUp:
    m.tabbedWindow.ScrollUp()
case tea.MouseButtonWheelDown:
    m.tabbedWindow.ScrollDown()
```

with:

```go
case tea.MouseButtonWheelUp:
    m.splitPane.ScrollUp()
case tea.MouseButtonWheelDown:
    m.splitPane.ScrollDown()
```

**Step 6: Update quick interact handler (lines ~834-835)**

Replace:

```go
if m.tabbedWindow.IsInTerminalTab() {
    if err := m.tabbedWindow.SendTerminalPrompt(text); err != nil {
```

with:

```go
if m.splitPane.GetFocusedPane() == ui.TerminalPane {
    if err := m.splitPane.SendTerminalPrompt(text); err != nil {
```

**Step 7: Update Esc key handler (lines ~908-927)**

Replace the entire Esc block:

```go
if msg.Type == tea.KeyEsc {
    // If in preview tab and in scroll mode, exit scroll mode
    if m.tabbedWindow.IsInPreviewTab() && m.tabbedWindow.IsPreviewInScrollMode() {
        // Use the selected instance from the list
        selected := m.list.GetSelectedInstance()
        err := m.tabbedWindow.ResetPreviewToNormalMode(selected)
        if err != nil {
            return m, m.handleError(err)
        }
        return m, m.instanceChanged()
    }
    // If in terminal tab and in scroll mode, exit scroll mode
    if m.tabbedWindow.IsInTerminalTab() && m.tabbedWindow.IsTerminalInScrollMode() {
        m.tabbedWindow.ResetTerminalToNormalMode()
        return m, m.instanceChanged()
    }
}
```

with:

```go
if msg.Type == tea.KeyEsc {
    // Dismiss diff overlay first
    if m.splitPane.IsDiffVisible() {
        m.splitPane.ToggleDiff()
        return m, m.instanceChanged()
    }
    // Exit agent scroll mode
    if m.splitPane.IsAgentInScrollMode() {
        selected := m.list.GetSelectedInstance()
        err := m.splitPane.ResetAgentToNormalMode(selected)
        if err != nil {
            return m, m.handleError(err)
        }
        return m, m.instanceChanged()
    }
    // Exit terminal scroll mode
    if m.splitPane.IsTerminalInScrollMode() {
        m.splitPane.ResetTerminalToNormalMode()
        return m, m.instanceChanged()
    }
}
```

**Step 8: Update key dispatch in switch statement (lines ~999-1008)**

Replace:

```go
case keys.KeyShiftUp:
    m.tabbedWindow.ScrollUp()
    return m, nil
case keys.KeyShiftDown:
    m.tabbedWindow.ScrollDown()
    return m, nil
case keys.KeyTab:
    m.tabbedWindow.Toggle()
    m.menu.SetActiveTab(m.tabbedWindow.GetActiveTab())
    return m, m.instanceChanged()
```

with:

```go
case keys.KeyShiftUp:
    m.splitPane.ScrollUp()
    return m, nil
case keys.KeyShiftDown:
    m.splitPane.ScrollDown()
    return m, nil
case keys.KeyTab:
    m.splitPane.ToggleFocus()
    m.menu.SetFocusedPane(m.splitPane.GetFocusedPane())
    return m, m.instanceChanged()
case keys.KeyDiff:
    m.splitPane.ToggleDiff()
    return m, m.instanceChanged()
```

**Step 9: Update kill instance cleanup (lines ~1033, ~1082)**

Replace both occurrences of:

```go
m.tabbedWindow.CleanupTerminalForInstance(selected.Title)
```

with:

```go
m.splitPane.CleanupTerminalForInstance(selected.Title)
```

**Step 10: Update quick interact guard (line ~1141)**

Replace:

```go
if m.tabbedWindow.IsInDiffTab() {
    return m, nil
}
```

with:

```go
if m.splitPane.IsDiffVisible() {
    return m, nil
}
```

**Step 11: Update full-screen attach handler (lines ~1157-1178)**

Replace:

```go
if m.tabbedWindow.IsInTerminalTab() {
    m.showHelpScreen(helpTypeInstanceAttach{}, func() {
        ch, err := m.tabbedWindow.AttachTerminal()
```

with:

```go
if m.splitPane.GetFocusedPane() == ui.TerminalPane {
    m.showHelpScreen(helpTypeInstanceAttach{}, func() {
        ch, err := m.splitPane.AttachTerminal()
```

**Step 12: Update instanceChanged method (lines ~1191-1202)**

Replace:

```go
m.tabbedWindow.UpdateDiff(selected)
m.tabbedWindow.SetInstance(selected)
// Update menu with current instance
m.menu.SetInstance(selected)

// If there's no selected instance, we don't need to update the preview.
if err := m.tabbedWindow.UpdatePreview(selected); err != nil {
    return m.handleError(err)
}
if err := m.tabbedWindow.UpdateTerminal(selected); err != nil {
    return m.handleError(err)
}
```

with:

```go
m.splitPane.UpdateDiff(selected)
m.splitPane.SetInstance(selected)
// Update menu with current instance
m.menu.SetInstance(selected)

if err := m.splitPane.UpdateAgent(selected); err != nil {
    return m.handleError(err)
}
if err := m.splitPane.UpdateTerminal(selected); err != nil {
    return m.handleError(err)
}
```

**Step 13: Update activateWorkspace (lines ~1418-1436)**

Replace:

```go
tabbedWindow := ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane())

// Pre-size components if terminal dimensions are known.
if m.lastWidth > 0 && m.lastHeight > 0 {
    listWidth := int(float32(m.lastWidth) * 0.3)
    tabsWidth := m.lastWidth - listWidth
    contentHeight := int(float32(m.lastHeight)*0.9) - m.tabBar.Height()
    list.SetSize(listWidth, contentHeight)
    tabbedWindow.SetSize(tabsWidth, contentHeight)
}

m.slots = append(m.slots, workspaceSlot{
    wsCtx:        wsCtx,
    storage:      storage,
    appConfig:    appConfig,
    appState:     state,
    list:         list,
    tabbedWindow: tabbedWindow,
})
```

with:

```go
splitPane := ui.NewSplitPane(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane())

// Pre-size components if terminal dimensions are known.
if m.lastWidth > 0 && m.lastHeight > 0 {
    listWidth := int(float32(m.lastWidth) * 0.2)
    paneWidth := m.lastWidth - listWidth
    contentHeight := int(float32(m.lastHeight)*0.9) - m.tabBar.Height()
    list.SetSize(listWidth, contentHeight)
    splitPane.SetSize(paneWidth, contentHeight)
}

m.slots = append(m.slots, workspaceSlot{
    wsCtx:     wsCtx,
    storage:   storage,
    appConfig: appConfig,
    appState:  state,
    list:      list,
    splitPane: splitPane,
})
```

**Step 14: Update saveCurrentSlot (line ~1472)**

Replace:

```go
s.tabbedWindow = m.tabbedWindow
```

with:

```go
s.splitPane = m.splitPane
```

**Step 15: Update loadSlot (line ~1487)**

Replace:

```go
m.tabbedWindow = slot.tabbedWindow
```

with:

```go
m.splitPane = slot.splitPane
```

**Step 16: Update View method (line ~1584)**

Replace:

```go
rightContent := m.tabbedWindow.String()
```

with:

```go
rightContent := m.splitPane.String()
```

**Step 17: Update app_test.go (line ~445)**

Replace:

```go
tabbedWindow: ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane()),
```

with:

```go
splitPane: ui.NewSplitPane(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane()),
```

**Step 18: Verify it compiles**

Run: `CGO_ENABLED=0 go build -o /dev/null`
Expected: success

**Step 19: Run tests**

Run: `go test -v ./...`
Expected: all pass

**Step 20: Commit**

```bash
git add app/app.go app/app_test.go
git commit -m "feat(app): replace TabbedWindow with SplitPane in app"
```

---

### Task 5: Rename Preview to Agent in display strings

**Files:**
- Modify: `ui/preview.go`

**Step 1: Update fallback text message**

In `preview.go` line 78, replace:

```go
p.setFallbackState("No agents running yet. Spin up a new instance with 'n' to get started!")
```

No change needed here — this text doesn't reference "Preview".

Check `split_pane.go` — the separator label already says "Agent" (done in Task 2).

The only display string that said "Preview" was in the tab header of `TabbedWindow`, which is now deleted. The separator label in `SplitPane.buildSeparator()` already uses "Agent". No changes needed to `preview.go`.

**Step 1: Verify no "Preview" display strings remain**

Search for "Preview" in UI strings:

Run: `grep -rn '"Preview"' ui/`
Expected: only the `PreviewTab` constant (which is removed with tabbed_window.go) and struct type names (which we keep)

**Step 2: Commit (skip if no changes needed)**

No commit needed for this task — the rename is handled by the SplitPane separator label.

---

### Task 6: Delete tabbed_window.go

**Files:**
- Delete: `ui/tabbed_window.go`

**Step 1: Verify no remaining references**

Run: `grep -rn 'TabbedWindow\|tabbedWindow\|PreviewTab\|DiffTab\b' --include='*.go' . | grep -v tabbed_window.go | grep -v '_test.go' | grep -v docs/`
Expected: no results (all references were updated in Task 4)

Note: `TerminalPane` is a type in `terminal.go` and a constant in `split_pane.go` — that's fine, the constant in `split_pane.go` is the new one.

**Step 2: Delete the file**

```bash
rm ui/tabbed_window.go
```

**Step 3: Verify it compiles**

Run: `CGO_ENABLED=0 go build -o /dev/null`
Expected: success

**Step 4: Run all tests**

Run: `go test -v ./...`
Expected: all pass

**Step 5: Commit**

```bash
git add -A ui/tabbed_window.go
git commit -m "refactor(ui): remove TabbedWindow (replaced by SplitPane)"
```

---

### Task 7: Update keybinding help text

**Files:**
- Modify: `keys/keys.go`

**Step 1: Update tab key help text**

Replace:

```go
KeyTab: key.NewBinding(
    key.WithKeys("tab"),
    key.WithHelp("tab", "switch tab"),
),
```

with:

```go
KeyTab: key.NewBinding(
    key.WithKeys("tab"),
    key.WithHelp("tab", "focus"),
),
```

**Step 2: Verify it compiles**

Run: `CGO_ENABLED=0 go build -o /dev/null`
Expected: success

**Step 3: Commit**

```bash
git add keys/keys.go
git commit -m "fix(keys): update tab help text from 'switch tab' to 'focus'"
```

---

### Task 8: Final verification

**Step 1: Run full test suite**

Run: `go test -v ./...`
Expected: all pass

**Step 2: Run linter**

Run: `golangci-lint run --timeout=3m --fast`
Expected: no errors

**Step 3: Format check**

Run: `gofmt -l .`
Expected: no output (all files formatted)

**Step 4: Build binary**

Run: `CGO_ENABLED=0 go build -o claude-squad`
Expected: success, binary created

**Step 5: Manual smoke test**

Run `./claude-squad` and verify:
- Left panel is narrower (20% width)
- Right panel shows agent output on top (~70% height) and terminal on bottom (~30% height)
- `tab` swaps focus indicator between agent and terminal panes
- `shift+up/down` scrolls the focused pane
- `d` opens diff overlay covering the right panel
- `d` or `Esc` closes the diff overlay
- Menu bar shows `d diff` hint
- All existing functionality still works (n, N, D, p, c, r, i, o, O, etc.)
