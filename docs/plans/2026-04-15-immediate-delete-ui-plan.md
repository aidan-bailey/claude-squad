# Immediate UI Feedback on Session Deletion — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** When the user confirms session deletion, immediately show "Deleting..." status in the list and run cleanup in the background, reverting on failure.

**Architecture:** Add a `Deleting` status to the Instance enum. On kill confirmation, set status synchronously in the main goroutine via a new `pendingPreAction` hook, then run cleanup as an async Cmd. A new `killFailedMsg` handles rollback.

**Tech Stack:** Go, Bubble Tea (charmbracelet), testify

---

### Task 1: Add `Deleting` status to the Instance enum

**Files:**
- Modify: `session/instance.go:17-30`

**Step 1: Add the status constant**

In `session/instance.go`, add `Deleting` to the Status const block after `Prompting`:

```go
const (
	Running Status = iota
	Ready
	Loading
	Paused
	Prompting
	// Deleting is a transient status set immediately when the user confirms
	// deletion. Cleanup runs asynchronously; on failure the status reverts.
	Deleting
)
```

**Step 2: Run tests to verify no regressions**

Run: `go build ./...`
Expected: PASS

**Step 3: Commit**

```bash
git add session/instance.go
git commit -m "feat(session): add Deleting status to Instance enum"
```

---

### Task 2: Render `Deleting` status in the list UI

**Files:**
- Modify: `ui/list.go:15-34` (icons and styles)
- Modify: `ui/list.go:220-243` (Render switch)

**Step 1: Add the icon and style**

In `ui/list.go`, after the existing icon/style constants:

```go
const deletingIcon = "✕ "

var deletingStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#cc6666", Dark: "#cc6666"})
```

**Step 2: Add the Deleting case to the Render switch**

In the `Render` method, in the `switch i.Status` block (the non-workspace-terminal branch), add before `default:`:

```go
case session.Deleting:
	join = deletingStyle.Render(deletingIcon)
```

Also handle workspace terminals (in the `if i.IsWorkspaceTerminal` branch) — this shouldn't happen in practice but prevents a blank icon:

```go
} else if i.Status == session.Deleting {
	join = deletingStyle.Render(deletingIcon)
}
```

**Step 3: Run tests**

Run: `go build ./...`
Expected: PASS

**Step 4: Commit**

```bash
git add ui/list.go
git commit -m "feat(ui): render Deleting status with ✕ icon in instance list"
```

---

### Task 3: Add `killFailedMsg`, `pendingPreAction`, and modify the kill/confirmation flow

**Files:**
- Modify: `app/app.go:140-143` (add `pendingPreAction` field)
- Modify: `app/app.go:508-512` (keep existing `killInstanceMsg` handler)
- Modify: `app/app.go:914-923` (confirmation handler — run `pendingPreAction`)
- Modify: `app/app.go:1031-1073` (KeyKill handler — capture previous status, create preAction, modify killAction to return `killFailedMsg` on error)
- Modify: `app/app.go:1309-1314` (add `killFailedMsg` type)
- Modify: `app/app.go:1412-1428` (confirmAction — also clear pendingPreAction on cancel)

**Step 1: Add `pendingPreAction` field to `home` struct**

After line 143 (`pendingAction tea.Cmd`), add:

```go
// pendingPreAction runs synchronously in the main goroutine before the
// pendingAction Cmd is dispatched. Used to set Deleting status immediately.
pendingPreAction func()
```

**Step 2: Add `killFailedMsg` type**

After `killInstanceMsg` (line 1312):

```go
// killFailedMsg is returned when background cleanup fails. The main event
// loop reverts the instance status so the user can retry.
type killFailedMsg struct {
	title          string
	previousStatus session.Status
	err            error
}
```

**Step 3: Modify the confirmation handler to run `pendingPreAction`**

Replace lines 914-923:

```go
if m.state == stateConfirm {
	shouldClose := m.confirmationOverlay.HandleKeyPress(msg)
	if shouldClose {
		if m.pendingPreAction != nil {
			m.pendingPreAction()
			m.pendingPreAction = nil
		}
		cmd := m.pendingAction
		m.pendingAction = nil
		m.confirmationOverlay = nil
		m.state = stateDefault
		return m, tea.Batch(cmd, m.instanceChanged())
	}
	return m, nil
}
```

Note: `tea.Batch(cmd, m.instanceChanged())` ensures the UI refreshes immediately after the preAction sets Deleting status.

**Step 4: Clear `pendingPreAction` on cancel**

In `confirmAction` (line 1423), in the `OnCancel` callback, add:

```go
m.confirmationOverlay.OnCancel = func() {
	m.pendingAction = nil
	m.pendingPreAction = nil
}
```

**Step 5: Modify the KeyKill handler**

Replace the kill flow at lines 1031-1073:

```go
case keys.KeyKill:
	selected := m.list.GetSelectedInstance()
	if selected == nil || selected.Status == session.Loading || selected.Status == session.Deleting || selected.IsWorkspaceTerminal {
		return m, nil
	}

	title := selected.Title
	previousStatus := selected.Status

	// preAction runs synchronously in the main goroutine when the user
	// confirms. It marks the instance as Deleting immediately.
	preAction := func() {
		selected.SetStatus(session.Deleting)
	}

	killAction := func() tea.Msg {
		// Get worktree and check if branch is checked out
		worktree, err := selected.GetGitWorktree()
		if err != nil {
			return killFailedMsg{title: title, previousStatus: previousStatus, err: err}
		}

		checkedOut, err := worktree.IsBranchCheckedOut()
		if err != nil {
			return killFailedMsg{title: title, previousStatus: previousStatus, err: err}
		}

		if checkedOut {
			return killFailedMsg{
				title:          title,
				previousStatus: previousStatus,
				err:            fmt.Errorf("instance %s is currently checked out", selected.Title),
			}
		}

		// Kill the instance (tmux + worktree cleanup)
		if err := selected.Kill(); err != nil {
			log.ErrorLog.Printf("could not kill instance: %v", err)
		}

		// Delete from persistent storage
		if err := m.storage.DeleteInstance(selected.Title); err != nil {
			return killFailedMsg{title: title, previousStatus: previousStatus, err: err}
		}

		return killInstanceMsg{title: title}
	}

	message := fmt.Sprintf("[!] Kill session '%s'?", selected.Title)
	m.pendingPreAction = preAction
	return m, m.confirmAction(message, killAction)
```

**Step 6: Add `killFailedMsg` handler in Update**

After the `killInstanceMsg` case (line 512), add:

```go
case killFailedMsg:
	// Revert instance status on failed deletion
	for _, inst := range m.list.GetInstances() {
		if inst.Title == msg.title {
			inst.SetStatus(msg.previousStatus)
			break
		}
	}
	log.ErrorLog.Printf("failed to delete session %q: %v", msg.title, msg.err)
	return m, tea.Batch(m.handleError(msg.err), m.instanceChanged())
```

**Step 7: Run tests**

Run: `go test -v ./app/`
Expected: PASS (existing confirmation tests still pass)

**Step 8: Commit**

```bash
git add app/app.go
git commit -m "feat(app): set Deleting status immediately on kill confirm, revert on failure"
```

---

### Task 4: Skip `Deleting` instances in key handlers and metadata tick

**Files:**
- Modify: `app/app.go` (key handler guard checks + metadata tick)

**Step 1: Add `Deleting` guard to all interactive key handlers**

Add `|| selected.Status == session.Deleting` to the guard conditions for these keys:

- **KeySubmit** (line 1076): `selected == nil || selected.Status == session.Loading || selected.Status == session.Deleting || selected.IsWorkspaceTerminal`
- **KeyCheckout** (line 1099): same pattern
- **KeyResume** (line 1119): same pattern
- **KeyEnter** (line 1131): `selected == nil || selected.Paused() || selected.Status == session.Loading || selected.Status == session.Deleting || !selected.TmuxAlive()`
- **KeyDirectAttachAgent** (line 1142): same as KeyEnter
- **KeyDirectAttachTerminal** (line 1154): same as KeyEnter
- **KeyQuickInteract** (line 1193): `selected == nil || selected.Paused() || !selected.TmuxAlive() || selected.Status == session.Loading || selected.Status == session.Deleting`
- **KeyQuickInputAgent** (line 1205): same as KeyQuickInteract
- **KeyQuickInputTerminal** (line 1217): same as KeyQuickInteract
- **KeyFullScreenAttach** (line 1232): same as KeyEnter

**Step 2: Skip `Deleting` instances in the metadata tick**

In the metadata tick filter (line 403-406), add the Deleting check:

```go
for _, inst := range allInstances {
	if inst.Started() && !inst.Paused() && inst.Status != session.Deleting {
		active = append(active, inst)
	}
}
```

**Step 3: Run tests**

Run: `go test -v ./app/`
Expected: PASS

**Step 4: Commit**

```bash
git add app/app.go
git commit -m "fix(app): skip Deleting instances in key handlers and metadata tick"
```

---

### Task 5: Don't persist `Deleting` instances on quit

**Files:**
- Modify: `app/app.go:572-583` (`handleQuit`)

**Step 1: Filter Deleting instances before saving**

Replace the save logic in `handleQuit` with filtered saves. Add a helper function:

```go
// persistableInstances filters out instances with transient Deleting status.
func persistableInstances(instances []*session.Instance) []*session.Instance {
	var result []*session.Instance
	for _, inst := range instances {
		if inst.Status != session.Deleting {
			result = append(result, inst)
		}
	}
	return result
}
```

Then in `handleQuit`, use it:

```go
if len(m.slots) > 0 {
	m.saveCurrentSlot()
	for _, slot := range m.slots {
		if err := slot.storage.SaveInstances(persistableInstances(slot.list.GetInstances())); err != nil {
			log.ErrorLog.Printf("failed to save workspace %s: %v", slot.wsCtx.Name, err)
		}
	}
} else {
	if err := m.storage.SaveInstances(persistableInstances(m.list.GetInstances())); err != nil {
		return m, m.handleError(err)
	}
}
```

**Step 2: Run tests**

Run: `go test -v ./app/`
Expected: PASS

**Step 3: Commit**

```bash
git add app/app.go
git commit -m "fix(app): exclude Deleting instances from persistence on quit"
```

---

### Task 6: Write tests

**Files:**
- Modify: `app/app_test.go`

**Step 1: Test that kill confirmation sets Deleting status immediately**

```go
func TestKillSetsStatusToDeletingImmediately(t *testing.T) {
	s := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	list := ui.NewList(&s, false)

	instance, err := session.NewInstance(session.InstanceOptions{
		Title:   "test-delete",
		Path:    t.TempDir(),
		Program: "claude",
	})
	require.NoError(t, err)
	instance.SetStatus(session.Running)
	_ = list.AddInstance(instance)
	list.SetSelectedInstance(0)

	h := &home{
		ctx:       context.Background(),
		state:     stateDefault,
		appConfig: config.DefaultConfig(),
		list:      list,
		menu:      ui.NewMenu(),
		splitPane: ui.NewSplitPane(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane()),
	}

	// Set up a preAction like the kill handler does
	h.pendingPreAction = func() {
		instance.SetStatus(session.Deleting)
	}
	h.confirmAction("[!] Kill session 'test-delete'?", func() tea.Msg {
		return killInstanceMsg{title: "test-delete"}
	})

	// Simulate confirming (pressing 'y')
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}
	_, _ = h.handleKeyPress(keyMsg)

	// preAction should have run — status should be Deleting
	assert.Equal(t, session.Deleting, instance.Status)
}
```

**Step 2: Test that killFailedMsg reverts status**

```go
func TestKillFailedMsgRevertsStatus(t *testing.T) {
	s := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	list := ui.NewList(&s, false)

	instance, err := session.NewInstance(session.InstanceOptions{
		Title:   "test-revert",
		Path:    t.TempDir(),
		Program: "claude",
	})
	require.NoError(t, err)
	instance.SetStatus(session.Deleting)
	_ = list.AddInstance(instance)

	h := &home{
		ctx:       context.Background(),
		state:     stateDefault,
		appConfig: config.DefaultConfig(),
		list:      list,
		menu:      ui.NewMenu(),
		splitPane: ui.NewSplitPane(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane()),
		errBox:    ui.NewErrBox(),
	}

	// Process killFailedMsg
	msg := killFailedMsg{
		title:          "test-revert",
		previousStatus: session.Running,
		err:            fmt.Errorf("branch is checked out"),
	}
	h.Update(msg)

	assert.Equal(t, session.Running, instance.Status)
}
```

**Step 3: Test that Deleting instances are filtered from persistence**

```go
func TestPersistableInstancesFiltersDeleting(t *testing.T) {
	running, _ := session.NewInstance(session.InstanceOptions{
		Title: "running", Path: t.TempDir(), Program: "claude",
	})
	running.SetStatus(session.Running)

	deleting, _ := session.NewInstance(session.InstanceOptions{
		Title: "deleting", Path: t.TempDir(), Program: "claude",
	})
	deleting.SetStatus(session.Deleting)

	paused, _ := session.NewInstance(session.InstanceOptions{
		Title: "paused", Path: t.TempDir(), Program: "claude",
	})
	paused.SetStatus(session.Paused)

	result := persistableInstances([]*session.Instance{running, deleting, paused})
	assert.Len(t, result, 2)
	assert.Equal(t, "running", result[0].Title)
	assert.Equal(t, "paused", result[1].Title)
}
```

**Step 4: Run all tests**

Run: `go test -v ./...`
Expected: PASS

**Step 5: Commit**

```bash
git add app/app_test.go
git commit -m "test(app): add tests for Deleting status, kill failure revert, and persistence filtering"
```
