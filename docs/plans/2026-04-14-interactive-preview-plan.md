# Interactive Preview (Inline Attach) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace enter-to-fullscreen with an inline attach mode where keystrokes forward to tmux while the Bubble Tea UI stays visible. Full-screen attach moves to `O`.

**Architecture:** New `stateInlineAttach` app state intercepts all `tea.KeyMsg` in `Update()`, converts them to terminal bytes via a `keyMsgToBytes()` function, and writes them to the tmux PTY via `SendKeys()`. Preview continues updating via the existing tick loop (reduced to 50ms during inline attach). Ctrl+Q exits the mode.

**Tech Stack:** Go, Bubble Tea, tmux PTY (`SendKeys`), existing `capture-pane` polling

---

### Task 1: Add `keyMsgToBytes` conversion function

**Files:**
- Create: `app/keybytes.go`
- Test: `app/keybytes_test.go`

**Step 1: Write the failing tests**

```go
// app/keybytes_test.go
package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestKeyMsgToBytes_Runes(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}
	assert.Equal(t, []byte("a"), keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_Enter(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	assert.Equal(t, []byte{0x0D}, keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_Backspace(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyBackspace}
	assert.Equal(t, []byte{0x7F}, keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_Tab(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyTab}
	assert.Equal(t, []byte{0x09}, keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_Escape(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyEsc}
	assert.Equal(t, []byte{0x1B}, keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_Space(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeySpace}
	assert.Equal(t, []byte{0x20}, keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_ArrowKeys(t *testing.T) {
	assert.Equal(t, []byte("\x1b[A"), keyMsgToBytes(tea.KeyMsg{Type: tea.KeyUp}))
	assert.Equal(t, []byte("\x1b[B"), keyMsgToBytes(tea.KeyMsg{Type: tea.KeyDown}))
	assert.Equal(t, []byte("\x1b[C"), keyMsgToBytes(tea.KeyMsg{Type: tea.KeyRight}))
	assert.Equal(t, []byte("\x1b[D"), keyMsgToBytes(tea.KeyMsg{Type: tea.KeyLeft}))
}

func TestKeyMsgToBytes_CtrlKeys(t *testing.T) {
	// Ctrl+A = 0x01, Ctrl+C = 0x03
	msg := tea.KeyMsg{Type: tea.KeyCtrlA}
	assert.Equal(t, []byte{0x01}, keyMsgToBytes(msg))

	msg = tea.KeyMsg{Type: tea.KeyCtrlC}
	assert.Equal(t, []byte{0x03}, keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_HomeEnd(t *testing.T) {
	assert.Equal(t, []byte("\x1b[H"), keyMsgToBytes(tea.KeyMsg{Type: tea.KeyHome}))
	assert.Equal(t, []byte("\x1b[F"), keyMsgToBytes(tea.KeyMsg{Type: tea.KeyEnd}))
}

func TestKeyMsgToBytes_Delete(t *testing.T) {
	assert.Equal(t, []byte("\x1b[3~"), keyMsgToBytes(tea.KeyMsg{Type: tea.KeyDelete}))
}

func TestKeyMsgToBytes_PageUpDown(t *testing.T) {
	assert.Equal(t, []byte("\x1b[5~"), keyMsgToBytes(tea.KeyMsg{Type: tea.KeyPgUp}))
	assert.Equal(t, []byte("\x1b[6~"), keyMsgToBytes(tea.KeyMsg{Type: tea.KeyPgDown}))
}

func TestKeyMsgToBytes_FunctionKeys(t *testing.T) {
	assert.Equal(t, []byte("\x1bOP"), keyMsgToBytes(tea.KeyMsg{Type: tea.KeyF1}))
	assert.Equal(t, []byte("\x1bOQ"), keyMsgToBytes(tea.KeyMsg{Type: tea.KeyF2}))
	assert.Equal(t, []byte("\x1b[15~"), keyMsgToBytes(tea.KeyMsg{Type: tea.KeyF5}))
	assert.Equal(t, []byte("\x1b[24~"), keyMsgToBytes(tea.KeyMsg{Type: tea.KeyF12}))
}

func TestKeyMsgToBytes_AltKey(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Alt: true}
	assert.Equal(t, []byte("\x1bx"), keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_MultiByteRune(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("日")}
	assert.Equal(t, []byte("日"), keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_Unknown(t *testing.T) {
	// An unrecognized key type should return nil
	msg := tea.KeyMsg{Type: tea.KeyType(9999)}
	assert.Nil(t, keyMsgToBytes(msg))
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/interactive-preview_18a637fe66fd43b3 && CGO_ENABLED=0 go test -v ./app/ -run TestKeyMsgToBytes -count=1`
Expected: FAIL — `keyMsgToBytes` not defined

**Step 3: Write the implementation**

```go
// app/keybytes.go
package app

import (
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// keyMsgToBytes converts a Bubble Tea KeyMsg back into raw terminal bytes
// suitable for writing to a tmux PTY. Returns nil for unmappable keys.
func keyMsgToBytes(msg tea.KeyMsg) []byte {
	// Ctrl keys: Ctrl+A (0x01) through Ctrl+Z (0x1A)
	if msg.Type >= tea.KeyCtrlA && msg.Type <= tea.KeyCtrlZ {
		return []byte{byte(msg.Type - tea.KeyCtrlA + 1)}
	}

	switch msg.Type {
	case tea.KeyRunes:
		if len(msg.Runes) == 0 {
			return nil
		}
		buf := make([]byte, 0, len(msg.Runes)*4)
		for _, r := range msg.Runes {
			b := make([]byte, utf8.RuneLen(r))
			utf8.EncodeRune(b, r)
			buf = append(buf, b...)
		}
		if msg.Alt {
			return append([]byte{0x1b}, buf...)
		}
		return buf

	case tea.KeyEnter:
		return []byte{0x0D}
	case tea.KeyBackspace:
		return []byte{0x7F}
	case tea.KeyTab:
		return []byte{0x09}
	case tea.KeyEsc:
		return []byte{0x1B}
	case tea.KeySpace:
		return []byte{0x20}

	// Arrow keys
	case tea.KeyUp:
		return []byte("\x1b[A")
	case tea.KeyDown:
		return []byte("\x1b[B")
	case tea.KeyRight:
		return []byte("\x1b[C")
	case tea.KeyLeft:
		return []byte("\x1b[D")

	// Navigation
	case tea.KeyHome:
		return []byte("\x1b[H")
	case tea.KeyEnd:
		return []byte("\x1b[F")
	case tea.KeyPgUp:
		return []byte("\x1b[5~")
	case tea.KeyPgDown:
		return []byte("\x1b[6~")
	case tea.KeyDelete:
		return []byte("\x1b[3~")

	// Function keys
	case tea.KeyF1:
		return []byte("\x1bOP")
	case tea.KeyF2:
		return []byte("\x1bOQ")
	case tea.KeyF3:
		return []byte("\x1bOR")
	case tea.KeyF4:
		return []byte("\x1bOS")
	case tea.KeyF5:
		return []byte("\x1b[15~")
	case tea.KeyF6:
		return []byte("\x1b[17~")
	case tea.KeyF7:
		return []byte("\x1b[18~")
	case tea.KeyF8:
		return []byte("\x1b[19~")
	case tea.KeyF9:
		return []byte("\x1b[20~")
	case tea.KeyF10:
		return []byte("\x1b[21~")
	case tea.KeyF11:
		return []byte("\x1b[23~")
	case tea.KeyF12:
		return []byte("\x1b[24~")

	default:
		return nil
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/interactive-preview_18a637fe66fd43b3 && CGO_ENABLED=0 go test -v ./app/ -run TestKeyMsgToBytes -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add app/keybytes.go app/keybytes_test.go
git commit -m "feat: add keyMsgToBytes conversion for inline attach"
```

---

### Task 2: Add `KeyFullScreenAttach` keybinding and `stateInlineAttach` state

**Files:**
- Modify: `keys/keys.go`
- Modify: `app/app.go:42-56` (state constants)

**Step 1: Add `KeyFullScreenAttach` to keys/keys.go**

Add to the `KeyName` const block after `KeyQuickInteract`:

```go
KeyFullScreenAttach // Key for full-screen attach (existing attach behavior)
```

Add to `GlobalKeyStringsMap`:

```go
"O": KeyFullScreenAttach,
```

Add to `GlobalkeyBindings`:

```go
KeyFullScreenAttach: key.NewBinding(
    key.WithKeys("O"),
    key.WithHelp("O", "fullscreen"),
),
```

**Step 2: Add `stateInlineAttach` to app.go**

Add after `stateQuickInteract`:

```go
// stateInlineAttach is the state when keystrokes are forwarded to the tmux session
// while the UI remains visible.
stateInlineAttach
```

**Step 3: Verify build compiles**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/interactive-preview_18a637fe66fd43b3 && CGO_ENABLED=0 go build -o /dev/null`
Expected: Build succeeds

**Step 4: Commit**

```bash
git add keys/keys.go app/app.go
git commit -m "feat: add KeyFullScreenAttach keybinding and stateInlineAttach state"
```

---

### Task 3: Add `StateInlineAttach` menu state with inline attach indicator

**Files:**
- Modify: `ui/menu.go:38-44` (MenuState constants)
- Modify: `ui/menu.go:106-125` (updateOptions)

**Step 1: Add `StateInlineAttach` to MenuState**

Add after `StateQuickInteract`:

```go
StateInlineAttach
```

**Step 2: Add menu options for inline attach state**

Add a new options slice near the existing ones (around line 60):

```go
var inlineAttachMenuOptions = []keys.KeyName{}
```

Add the case in `updateOptions()`:

```go
case StateInlineAttach:
    m.options = inlineAttachMenuOptions
```

**Step 3: Verify build compiles**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/interactive-preview_18a637fe66fd43b3 && CGO_ENABLED=0 go build -o /dev/null`
Expected: Build succeeds

**Step 4: Commit**

```bash
git add ui/menu.go
git commit -m "feat: add StateInlineAttach menu state"
```

---

### Task 4: Wire up enter key to inline attach and `O` to full-screen attach

**Files:**
- Modify: `app/app.go:971-1002` (KeyEnter handler)
- Modify: `app/app.go` (add new KeyFullScreenAttach case)
- Modify: `app/app.go:486` (handleMenuHighlighting — add stateInlineAttach to skip list)

**Step 1: Change enter/o to enter `stateInlineAttach`**

Replace the `case keys.KeyEnter:` block (lines 971-1002) with:

```go
case keys.KeyEnter:
    if m.list.NumInstances() == 0 {
        return m, nil
    }
    selected := m.list.GetSelectedInstance()
    if selected == nil || selected.Paused() || selected.Status == session.Loading || !selected.TmuxAlive() {
        return m, nil
    }
    m.state = stateInlineAttach
    m.menu.SetState(ui.StateInlineAttach)
    return m, tea.WindowSize()
```

**Step 2: Add `KeyFullScreenAttach` case for full-screen attach**

Add a new case before the `default:` in the key switch (after the KeyQuickInteract case around line 1042):

```go
case keys.KeyFullScreenAttach:
    if m.list.NumInstances() == 0 {
        return m, nil
    }
    selected := m.list.GetSelectedInstance()
    if selected == nil || selected.Paused() || selected.Status == session.Loading || !selected.TmuxAlive() {
        return m, nil
    }
    // Terminal tab: attach to terminal session
    if m.tabbedWindow.IsInTerminalTab() {
        m.showHelpScreen(helpTypeInstanceAttach{}, func() {
            ch, err := m.tabbedWindow.AttachTerminal()
            if err != nil {
                m.handleError(err)
                return
            }
            <-ch
            m.state = stateDefault
        })
        return m, nil
    }
    // Preview/diff tab: attach to main instance
    m.showHelpScreen(helpTypeInstanceAttach{}, func() {
        ch, err := m.list.Attach()
        if err != nil {
            m.handleError(err)
            return
        }
        <-ch
        m.state = stateDefault
    })
    return m, nil
```

**Step 3: Add `stateInlineAttach` to the handleMenuHighlighting skip list**

At `app/app.go:486`, add `stateInlineAttach` to the early-return condition:

```go
if m.state == statePrompt || m.state == stateHelp || m.state == stateConfirm || m.state == stateWorkspace || m.state == stateQuickInteract || m.state == stateInlineAttach {
```

**Step 4: Verify build compiles**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/interactive-preview_18a637fe66fd43b3 && CGO_ENABLED=0 go build -o /dev/null`
Expected: Build succeeds

**Step 5: Commit**

```bash
git add app/app.go
git commit -m "feat: wire enter to inline attach, O to fullscreen attach"
```

---

### Task 5: Handle keystrokes in `stateInlineAttach`

**Files:**
- Modify: `app/app.go` (add inline attach key handling block, similar placement to stateQuickInteract at line 693)

**Step 1: Add the inline attach key dispatch block**

Add this block **before** the `stateQuickInteract` handling (before line 693 in the current code). This goes inside the `tea.KeyMsg` case in `Update()`, before other state-specific handlers:

```go
if m.state == stateInlineAttach {
    selected := m.list.GetSelectedInstance()
    if selected == nil || selected.Paused() || !selected.TmuxAlive() {
        // Instance died or was paused — exit inline attach
        m.state = stateDefault
        m.menu.SetState(ui.StateDefault)
        return m, tea.WindowSize()
    }

    // Ctrl+Q exits inline attach
    if msg.Type == tea.KeyCtrlQ {
        m.state = stateDefault
        m.menu.SetState(ui.StateDefault)
        return m, tea.WindowSize()
    }

    // Convert key to bytes and forward to tmux
    b := keyMsgToBytes(msg)
    if b != nil {
        if err := selected.SendKeysRaw(b); err != nil {
            log.ErrorLog.Printf("inline attach send error: %v", err)
        }
    }
    return m, nil
}
```

**Step 2: Add `SendKeysRaw` method to Instance**

Add to `session/instance.go`:

```go
// SendKeysRaw writes raw bytes to the tmux PTY. Used by inline attach mode.
func (i *Instance) SendKeysRaw(b []byte) error {
	if !i.started || i.tmuxSession == nil {
		return fmt.Errorf("instance not started or tmux session not initialized")
	}
	return i.tmuxSession.SendKeysRaw(b)
}
```

**Step 3: Add `SendKeysRaw` method to TmuxSession**

Add to `session/tmux/tmux.go`:

```go
// SendKeysRaw writes raw bytes directly to the tmux PTY.
func (t *TmuxSession) SendKeysRaw(b []byte) error {
	_, err := t.ptmx.Write(b)
	return err
}
```

**Step 4: Verify build compiles**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/interactive-preview_18a637fe66fd43b3 && CGO_ENABLED=0 go build -o /dev/null`
Expected: Build succeeds

**Step 5: Commit**

```bash
git add app/app.go session/instance.go session/tmux/tmux.go
git commit -m "feat: handle keystroke forwarding in stateInlineAttach"
```

---

### Task 6: Faster tick in inline attach mode

**Files:**
- Modify: `app/app.go:304-312` (previewTickMsg handler)

**Step 1: Make tick interval state-dependent**

Replace the `previewTickMsg` handler:

```go
case previewTickMsg:
    cmd := m.instanceChanged()

    // Use faster tick during inline attach for responsive feedback
    tickDuration := 100 * time.Millisecond
    if m.state == stateInlineAttach {
        tickDuration = 50 * time.Millisecond
    }

    return m, tea.Batch(
        cmd,
        func() tea.Msg {
            time.Sleep(tickDuration)
            return previewTickMsg{}
        },
    )
```

**Step 2: Also update Init() tick to use same pattern (no change needed — Init always starts at 100ms, which is correct)**

**Step 3: Verify build compiles**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/interactive-preview_18a637fe66fd43b3 && CGO_ENABLED=0 go build -o /dev/null`
Expected: Build succeeds

**Step 4: Commit**

```bash
git add app/app.go
git commit -m "feat: faster preview tick (50ms) during inline attach"
```

---

### Task 7: Visual indicator for inline attach mode

**Files:**
- Modify: `ui/tabbed_window.go` (add method to check/set inline attach visual state)
- Modify: `app/app.go:1444-1451` (View — render inline attach hint)
- Modify: `app/app.go` (window size handling for inline attach)

**Step 1: Add inline attach hint rendering in View()**

In `app/app.go` `View()` method, modify the right content rendering. Currently (around line 1447):

```go
rightContent := m.tabbedWindow.String()
if m.state == stateQuickInteract && m.quickInputBar != nil {
    rightContent = lipgloss.JoinVertical(lipgloss.Left, rightContent, m.quickInputBar.View())
}
```

Change to:

```go
rightContent := m.tabbedWindow.String()
if m.state == stateQuickInteract && m.quickInputBar != nil {
    rightContent = lipgloss.JoinVertical(lipgloss.Left, rightContent, m.quickInputBar.View())
} else if m.state == stateInlineAttach {
    hint := inlineAttachHintStyle.Render("Ctrl+Q to detach · O for fullscreen")
    rightContent = lipgloss.JoinVertical(lipgloss.Left, rightContent, hint)
}
```

**Step 2: Add the hint style**

Add near the top of `app/app.go` or in a nearby styling section:

```go
var inlineAttachHintStyle = lipgloss.NewStyle().
    Foreground(lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}).
    Bold(true)
```

**Step 3: Account for hint height in window sizing**

In the window size handler (around line 265-270), add inline attach case:

```go
quickInputHeight := 0
if m.state == stateQuickInteract && m.quickInputBar != nil {
    quickInputHeight = m.quickInputBar.Height()
    m.quickInputBar.SetWidth(ui.AdjustPreviewWidth(tabsWidth))
} else if m.state == stateInlineAttach {
    quickInputHeight = 1 // hint takes 1 line
}
m.tabbedWindow.SetSize(tabsWidth, contentHeight-quickInputHeight)
```

**Step 4: Add `stateInlineAttach` to the `SetInstance` guard in menu.go**

In `ui/menu.go:89`, add `StateInlineAttach`:

```go
if m.state != StateNewInstance && m.state != StatePrompt && m.state != StateQuickInteract && m.state != StateInlineAttach {
```

**Step 5: Verify build compiles**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/interactive-preview_18a637fe66fd43b3 && CGO_ENABLED=0 go build -o /dev/null`
Expected: Build succeeds

**Step 6: Commit**

```bash
git add app/app.go ui/menu.go
git commit -m "feat: visual indicator hint bar for inline attach mode"
```

---

### Task 8: Handle edge case — instance death during inline attach

**Files:**
- Modify: `app/app.go` (instanceChanged or previewTickMsg handler)

**Step 1: Add instance death detection in the tick handler**

In the `previewTickMsg` handler, after `m.instanceChanged()`, add:

```go
case previewTickMsg:
    // Check if inline-attached instance is still alive
    if m.state == stateInlineAttach {
        selected := m.list.GetSelectedInstance()
        if selected == nil || selected.Paused() || !selected.TmuxAlive() {
            m.state = stateDefault
            m.menu.SetState(ui.StateDefault)
        }
    }

    cmd := m.instanceChanged()
    // ... rest of tick handler
```

**Step 2: Verify build compiles**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/interactive-preview_18a637fe66fd43b3 && CGO_ENABLED=0 go build -o /dev/null`
Expected: Build succeeds

**Step 3: Commit**

```bash
git add app/app.go
git commit -m "fix: exit inline attach when instance dies"
```

---

### Task 9: Update CLAUDE.md keybindings table and help text

**Files:**
- Modify: `CLAUDE.md` (keybindings table)
- Modify: `app/help.go` (help screen text for inline attach)

**Step 1: Update keybindings in CLAUDE.md**

Change the `enter`/`o` row and add `O` row:

```
| `enter`/`o` | Inline attach (interactive preview) |
| `O` | Full-screen attach |
```

**Step 2: Update help.go — modify `helpTypeInstanceAttach` text**

In `app/help.go:87`, update the help text to mention Ctrl+Q for inline mode and that `O` opens full-screen:

Update the `helpTypeInstanceAttach` `toContent()` to reflect the new fullscreen-only context (since it's only shown for `O` now).

**Step 3: Verify build compiles**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/interactive-preview_18a637fe66fd43b3 && CGO_ENABLED=0 go build -o /dev/null`
Expected: Build succeeds

**Step 4: Commit**

```bash
git add CLAUDE.md app/help.go
git commit -m "docs: update keybindings for inline attach and fullscreen"
```

---

### Task 10: Run full test suite and lint

**Step 1: Run all tests**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/interactive-preview_18a637fe66fd43b3 && CGO_ENABLED=0 go test -v ./...`
Expected: All tests pass

**Step 2: Run linter**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/interactive-preview_18a637fe66fd43b3 && golangci-lint run --timeout=3m --fast`
Expected: No lint errors

**Step 3: Run formatter**

Run: `cd /tb/Source/Personal/claude-squad/.claude-squad/worktrees/aidanb/interactive-preview_18a637fe66fd43b3 && gofmt -w .`

**Step 4: Fix any issues found, commit if needed**

```bash
git add -A
git commit -m "chore: fix lint and formatting issues"
```

---

### Task 11: Manual smoke test

**Step 1:** Run `claude-squad` and create or resume an instance.

**Step 2:** Press `enter` on a running instance. Verify:
- UI stays visible (list on left, preview on right)
- Hint bar shows "Ctrl+Q to detach · O for fullscreen"
- Typing sends text to the agent session
- Enter key submits input to the agent
- Arrow keys work
- Ctrl+C sends interrupt
- Preview updates within ~50ms showing agent response

**Step 3:** Press `Ctrl+Q`. Verify:
- Returns to normal navigation mode
- Hint bar disappears
- List is navigable again

**Step 4:** Press `O` on a running instance. Verify:
- Full-screen attach works as before
- Ctrl+Q detaches back to normal UI

**Step 5:** Test edge cases:
- Press enter on a paused instance → should do nothing
- Kill instance while inline-attached → should exit to default state
- Switch to terminal tab, press enter → should inline-attach to terminal session
