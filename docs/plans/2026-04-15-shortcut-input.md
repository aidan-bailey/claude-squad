# Shortcut Input Enhancements Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add targeted quick input shortcuts (`a`/`t`) and direct attach shortcuts (`ctrl+a`/`ctrl+t`), and change inline attach exit key from Esc to ctrl+q.

**Architecture:** Extend the existing QuickInputBar with a target enum so it knows where to route submitted text. Add a `SetFocusedPane` method to SplitPane for programmatic focus. New key constants drive both features. The inline attach exit key change is a one-line swap from `tea.KeyEscape` to `tea.KeyCtrlQ`.

**Tech Stack:** Go, Bubble Tea, existing keys/ui/app packages

---

### Task 1: Add key constants and mappings

**Files:**
- Modify: `keys/keys.go:9-68`

**Step 1: Add new key constants**

Add after `KeyDiff` (line 38):

```go
	KeyQuickInputAgent    // Key for quick input targeting agent pane
	KeyQuickInputTerminal // Key for quick input targeting terminal pane
	KeyDirectAttachAgent    // Key for direct attach to agent pane
	KeyDirectAttachTerminal // Key for direct attach to terminal pane
```

**Step 2: Add key string mappings**

Add to `GlobalKeyStringsMap`:

```go
	"a":      KeyQuickInputAgent,
	"t":      KeyQuickInputTerminal,
	"ctrl+a": KeyDirectAttachAgent,
	"ctrl+t": KeyDirectAttachTerminal,
```

**Step 3: Add key bindings**

Add to `GlobalkeyBindings`:

```go
	KeyQuickInputAgent: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "input to agent"),
	),
	KeyQuickInputTerminal: key.NewBinding(
		key.WithKeys("t"),
		key.WithHelp("t", "input to terminal"),
	),
	KeyDirectAttachAgent: key.NewBinding(
		key.WithKeys("ctrl+a"),
		key.WithHelp("ctrl+a", "attach agent"),
	),
	KeyDirectAttachTerminal: key.NewBinding(
		key.WithKeys("ctrl+t"),
		key.WithHelp("ctrl+t", "attach terminal"),
	),
```

**Step 4: Verify build**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/shortcut-input_18a68ad138a56685 && CGO_ENABLED=0 go build -o /dev/null`
Expected: success

**Step 5: Commit**

```
feat(keys): add shortcut input and direct attach key constants
```

---

### Task 2: Add QuickInputTarget to QuickInputBar

**Files:**
- Modify: `ui/quick_input.go:1-73`
- Modify: `ui/quick_input_test.go:1-44`

**Step 1: Add target enum and update struct**

Add after the `QuickInputAction` constants (line 16):

```go
// QuickInputTarget specifies where submitted text should be routed.
type QuickInputTarget int

const (
	QuickInputTargetFocused  QuickInputTarget = iota // send to whichever pane is focused
	QuickInputTargetAgent                            // always send to agent
	QuickInputTargetTerminal                         // always send to terminal
)
```

Update the struct to include the target:

```go
type QuickInputBar struct {
	textInput textinput.Model
	Target    QuickInputTarget
	width     int
}
```

**Step 2: Update constructor to accept target**

```go
func NewQuickInputBar(target QuickInputTarget) *QuickInputBar {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.Focus()
	ti.CharLimit = 256
	return &QuickInputBar{
		textInput: ti,
		Target:    target,
	}
}
```

**Step 3: Update hint text to show destination**

Update the `View` method:

```go
func (q *QuickInputBar) View() string {
	input := q.textInput.View()
	var hintText string
	switch q.Target {
	case QuickInputTargetAgent:
		hintText = "Enter to send to agent · Esc to cancel"
	case QuickInputTargetTerminal:
		hintText = "Enter to send to terminal · Esc to cancel"
	default:
		hintText = "Enter to send · Esc to cancel"
	}
	hint := quickInputHintStyle.Render(hintText)
	return lipgloss.JoinVertical(lipgloss.Left, input, hint)
}
```

**Step 4: Update tests**

Update all `NewQuickInputBar()` calls to `NewQuickInputBar(QuickInputTargetFocused)`.

Add a test for target-specific hint rendering:

```go
func TestQuickInputBar_ViewHintByTarget(t *testing.T) {
	tests := []struct {
		target   QuickInputTarget
		contains string
	}{
		{QuickInputTargetFocused, "Enter to send"},
		{QuickInputTargetAgent, "send to agent"},
		{QuickInputTargetTerminal, "send to terminal"},
	}
	for _, tt := range tests {
		bar := NewQuickInputBar(tt.target)
		bar.SetWidth(80)
		assert.Contains(t, bar.View(), tt.contains)
	}
}
```

**Step 5: Run tests**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/shortcut-input_18a68ad138a56685 && go test -v ./ui/ -run QuickInput`
Expected: all pass

**Step 6: Commit**

```
feat(ui): add target routing to QuickInputBar
```

---

### Task 3: Add SetFocusedPane to SplitPane

**Files:**
- Modify: `ui/split_pane.go:120-123`

**Step 1: Add SetFocusedPane method**

Add after `GetFocusedPane` (line 123):

```go
// SetFocusedPane sets focus to the specified pane.
func (s *SplitPane) SetFocusedPane(pane int) {
	s.focusedPane = pane
}
```

**Step 2: Verify build**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/shortcut-input_18a68ad138a56685 && CGO_ENABLED=0 go build -o /dev/null`
Expected: success

**Step 3: Commit**

```
feat(ui): add SetFocusedPane method to SplitPane
```

---

### Task 4: Wire up quick input shortcuts in app

**Files:**
- Modify: `app/app.go:835-862` (quick input submit routing)
- Modify: `app/app.go:1148-1159` (key handler for KeyQuickInteract)

**Step 1: Update quick input submit to route by target**

Replace the submit handler (lines 835-855) with target-based routing:

```go
		case ui.QuickInputSubmit:
			text := m.quickInputBar.Value()
			var err error
			switch m.quickInputBar.Target {
			case ui.QuickInputTargetTerminal:
				err = m.splitPane.SendTerminalPrompt(text)
			case ui.QuickInputTargetAgent:
				err = selected.SendPrompt(text)
			default: // QuickInputTargetFocused
				if m.splitPane.GetFocusedPane() == ui.FocusTerminal {
					err = m.splitPane.SendTerminalPrompt(text)
				} else {
					err = selected.SendPrompt(text)
				}
			}
			m.quickInputBar = nil
			m.state = stateDefault
			m.menu.SetState(ui.StateDefault)
			if err != nil {
				return m, tea.Batch(tea.WindowSize(), m.handleError(err))
			}
			return m, tea.WindowSize()
```

**Step 2: Update existing KeyQuickInteract to pass target**

Change line 1157 from:
```go
		m.quickInputBar = ui.NewQuickInputBar()
```
to:
```go
		m.quickInputBar = ui.NewQuickInputBar(ui.QuickInputTargetFocused)
```

**Step 3: Add handlers for KeyQuickInputAgent and KeyQuickInputTerminal**

Add after the `KeyQuickInteract` case (after line 1159):

```go
	case keys.KeyQuickInputAgent:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Paused() || !selected.TmuxAlive() || selected.Status == session.Loading {
			return m, nil
		}
		if m.splitPane.IsDiffVisible() {
			return m, nil
		}
		m.state = stateQuickInteract
		m.quickInputBar = ui.NewQuickInputBar(ui.QuickInputTargetAgent)
		m.menu.SetState(ui.StateQuickInteract)
		return m, tea.WindowSize()
	case keys.KeyQuickInputTerminal:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Paused() || !selected.TmuxAlive() || selected.Status == session.Loading {
			return m, nil
		}
		if m.splitPane.IsDiffVisible() {
			return m, nil
		}
		m.state = stateQuickInteract
		m.quickInputBar = ui.NewQuickInputBar(ui.QuickInputTargetTerminal)
		m.menu.SetState(ui.StateQuickInteract)
		return m, tea.WindowSize()
```

**Step 4: Verify build**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/shortcut-input_18a68ad138a56685 && CGO_ENABLED=0 go build -o /dev/null`
Expected: success

**Step 5: Commit**

```
feat(app): wire up targeted quick input shortcuts (a/t)
```

---

### Task 5: Wire up direct attach shortcuts and change exit key

**Files:**
- Modify: `app/app.go:796-797` (inline attach escape handler)
- Modify: `app/app.go:1107-1117` (KeyEnter inline attach entry)
- Modify: `app/app.go:1629` (inline attach hint text)

**Step 1: Change inline attach exit from Esc to ctrl+q**

Change line 797 from:
```go
		if msg.Type == tea.KeyEscape {
```
to:
```go
		if msg.Type == tea.KeyCtrlQ {
```

**Step 2: Add handlers for KeyDirectAttachAgent and KeyDirectAttachTerminal**

Add after the `KeyEnter` case (after line 1117):

```go
	case keys.KeyDirectAttachAgent:
		if m.list.NumInstances() == 0 {
			return m, nil
		}
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Paused() || selected.Status == session.Loading || !selected.TmuxAlive() {
			return m, nil
		}
		m.splitPane.SetFocusedPane(ui.FocusAgent)
		m.state = stateInlineAttach
		m.menu.SetState(ui.StateInlineAttach)
		return m, tea.WindowSize()
	case keys.KeyDirectAttachTerminal:
		if m.list.NumInstances() == 0 {
			return m, nil
		}
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Paused() || selected.Status == session.Loading || !selected.TmuxAlive() {
			return m, nil
		}
		m.splitPane.SetFocusedPane(ui.FocusTerminal)
		m.state = stateInlineAttach
		m.menu.SetState(ui.StateInlineAttach)
		return m, tea.WindowSize()
```

**Step 3: Update inline attach hint text**

Change line 1629 from:
```go
		hint := inlineAttachHintStyle.Render("▶ CAPTURING INPUT  ·  Esc to detach  ·  O for fullscreen")
```
to:
```go
		hint := inlineAttachHintStyle.Render("▶ CAPTURING INPUT  ·  ctrl+q to detach  ·  O for fullscreen")
```

**Step 4: Verify build**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/shortcut-input_18a68ad138a56685 && CGO_ENABLED=0 go build -o /dev/null`
Expected: success

**Step 5: Commit**

```
feat(app): add direct attach shortcuts (ctrl+a/ctrl+t) and change exit to ctrl+q
```

---

### Task 6: Update help screen

**Files:**
- Modify: `app/help.go:36-65`

**Step 1: Update general help text**

Update the Managing section to include new shortcuts:

```go
		keyStyle.Render("↵/o")+descStyle.Render("       - Inline attach: interact without leaving UI"),
		keyStyle.Render("O")+descStyle.Render("         - Full-screen attach to session"),
		keyStyle.Render("i")+descStyle.Render("         - Quick input: type and send to focused pane"),
		keyStyle.Render("a")+descStyle.Render("         - Quick input: type and send to agent"),
		keyStyle.Render("t")+descStyle.Render("         - Quick input: type and send to terminal"),
		keyStyle.Render("ctrl+a")+descStyle.Render("    - Attach and capture input to agent"),
		keyStyle.Render("ctrl+t")+descStyle.Render("    - Attach and capture input to terminal"),
		keyStyle.Render("ctrl+q")+descStyle.Render("    - Detach from session"),
```

**Step 2: Verify build**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/shortcut-input_18a68ad138a56685 && CGO_ENABLED=0 go build -o /dev/null`
Expected: success

**Step 3: Run all tests**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/shortcut-input_18a68ad138a56685 && go test ./...`
Expected: all pass

**Step 4: Commit**

```
docs(help): document new shortcut input keybindings
```
