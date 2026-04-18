# Scriptable Hotkeys Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the hardcoded Go hotkey registry with a Lua-driven keymap. An embedded `defaults.lua` reproduces the current keymap via `cs.bind` + `cs.actions.*`; user scripts freely override, unbind, or compose bindings; every existing built-in runs via the same awaitable-intent path.

**Architecture:** The `script` package gains intent types and coroutine tracking so a single Lua handler can `cs.await` overlay-driven flows. Every built-in hotkey becomes either a sync primitive (cursor moves, diff toggle) or a deferred primitive that enqueues an intent the main goroutine handles by calling the existing `runXYZ` implementation. `defaults.lua` is embedded via `go:embed` and loaded before user scripts; `ctrl+c` is hard-reserved in `state_default.go` as an engine-bypass safety rail.

**Tech Stack:** Go 1.23, `github.com/yuin/gopher-lua` (vendored, Lua 5.1), `github.com/charmbracelet/bubbletea`, `go:embed`, `testify/assert`.

**Design doc:** `docs/plans/2026-04-18-scriptable-hotkeys-design.md`

---

## Ground Rules

- TDD: write the failing test first, see it fail, then implement.
- Commit at the end of every task. Each task must end green.
- Run `go test -v ./script/... ./app/... ./keys/...` before every commit.
- Use `cmp.Diff` only where already idiomatic; prefer `assert.Equal`.
- Never call `h.list.AddInstance` from a goroutine. Queue through the host.
- Never touch `*lua.LState` outside `engine.mu`.

---

## Task 1: Intent types and IDs

**Files:**
- Create: `script/intent.go`
- Create: `script/intent_test.go`

**Step 1: Write the failing test**

```go
package script

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIntentIDIsUnique(t *testing.T) {
	a := newIntentID()
	b := newIntentID()
	assert.NotEqual(t, a, b)
	assert.NotZero(t, a)
}

func TestIntentTypesImplementInterface(t *testing.T) {
	var _ Intent = QuitIntent{}
	var _ Intent = PushSelectedIntent{Confirm: true}
	var _ Intent = KillSelectedIntent{Confirm: true}
	var _ Intent = CheckoutIntent{Confirm: true, Help: true}
	var _ Intent = ResumeIntent{}
	var _ Intent = NewInstanceIntent{Prompt: true}
	var _ Intent = ShowHelpIntent{}
	var _ Intent = WorkspacePickerIntent{}
	var _ Intent = InlineAttachIntent{Pane: AttachPaneAgent}
	var _ Intent = FullscreenAttachIntent{Pane: AttachPaneTerminal}
	var _ Intent = QuickInputIntent{Pane: AttachPaneAgent}
}
```

**Step 2: Run test**

`go test -v ./script/ -run TestIntent`
Expected: FAIL (`Intent` undefined).

**Step 3: Implement**

```go
package script

import "sync/atomic"

type IntentID uint64

var nextIntentID uint64

func newIntentID() IntentID {
	return IntentID(atomic.AddUint64(&nextIntentID, 1))
}

type Intent interface{ intent() }

type AttachPane int

const (
	AttachPaneAgent AttachPane = iota
	AttachPaneTerminal
)

type QuitIntent struct{}
type PushSelectedIntent struct{ Confirm bool }
type KillSelectedIntent struct{ Confirm bool }
type CheckoutIntent struct{ Confirm, Help bool }
type ResumeIntent struct{}
type NewInstanceIntent struct {
	Prompt bool
	Title  string
}
type ShowHelpIntent struct{}
type WorkspacePickerIntent struct{}
type InlineAttachIntent struct{ Pane AttachPane }
type FullscreenAttachIntent struct{ Pane AttachPane }
type QuickInputIntent struct{ Pane AttachPane }

func (QuitIntent) intent()              {}
func (PushSelectedIntent) intent()      {}
func (KillSelectedIntent) intent()      {}
func (CheckoutIntent) intent()          {}
func (ResumeIntent) intent()            {}
func (NewInstanceIntent) intent()       {}
func (ShowHelpIntent) intent()          {}
func (WorkspacePickerIntent) intent()   {}
func (InlineAttachIntent) intent()      {}
func (FullscreenAttachIntent) intent()  {}
func (QuickInputIntent) intent()        {}
```

**Step 4: Run test**

`go test -v ./script/ -run TestIntent`
Expected: PASS.

**Step 5: Commit**

```bash
git add script/intent.go script/intent_test.go
git commit -m "feat(script): add Intent types and IntentID generator"
```

---

## Task 2: Engine coroutine tracking and Resume

**Files:**
- Modify: `script/engine.go` (add coroutine map, `Enqueue`, `Resume`)
- Modify: `script/host.go` (add `Enqueue(Intent) IntentID` method)
- Create: `script/engine_coroutine_test.go`

**Step 1: Write the failing test**

```go
package script

import (
	"testing"

	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
)

func TestEngineResumeContinuesCoroutine(t *testing.T) {
	e := NewEngine(nil)
	defer e.Close()

	// Compile a coroutine body that yields with an IntentID and
	// returns the resume value.
	err := e.L.DoString(`
		co_fn = function(id)
			local v = coroutine.yield(id)
			return v + 1
		end
	`)
	assert.NoError(t, err)

	id := newIntentID()
	co, _ := e.L.NewThread()
	e.track(id, co)

	// First run: should yield with the id we passed in.
	fn := e.L.GetGlobal("co_fn").(*lua.LFunction)
	st, rerr, vals := e.L.Resume(co, fn, lua.LNumber(id))
	assert.Equal(t, lua.ResumeYield, st)
	assert.NoError(t, rerr)
	assert.Equal(t, lua.LNumber(id), vals[0])

	// Resume with 41 → coroutine returns 42 and terminates.
	out, err := e.Resume(id, lua.LNumber(41))
	assert.NoError(t, err)
	assert.Equal(t, lua.LNumber(42), out)
}
```

**Step 2: Run test**

`go test -v ./script/ -run TestEngineResume`
Expected: FAIL (`track`, `Resume` undefined).

**Step 3: Implement**

Add to `script/engine.go`:

```go
// coroutines tracks suspended handler coroutines awaiting a host
// Resume. Access always under e.mu.
type coroutineSlot struct {
	co *lua.LState
}

// Inside Engine struct — add:
//   coroutines map[IntentID]coroutineSlot

// In NewEngine body, after action map init:
//   e.coroutines = map[IntentID]coroutineSlot{}

// track registers co under id. Caller holds e.mu.
func (e *Engine) track(id IntentID, co *lua.LState) {
	e.coroutines[id] = coroutineSlot{co: co}
}

// Resume wakes the coroutine for id with value and runs it to the
// next yield or completion. On completion the return value (if any)
// is returned; on yield the coroutine is re-tracked under a new id
// the cs.await machinery exposes.
func (e *Engine) Resume(id IntentID, value lua.LValue) (lua.LValue, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	slot, ok := e.coroutines[id]
	if !ok {
		return lua.LNil, fmt.Errorf("no coroutine for intent %d", id)
	}
	delete(e.coroutines, id)

	st, rerr, vals := e.L.Resume(slot.co, nil, value)
	switch st {
	case lua.ResumeOK:
		if len(vals) > 0 {
			return vals[0], nil
		}
		return lua.LNil, nil
	case lua.ResumeYield:
		// The yielded value is the next IntentID; track and wait.
		nextID, _ := vals[0].(lua.LNumber)
		e.coroutines[IntentID(nextID)] = slot
		return lua.LNil, nil
	default:
		return lua.LNil, rerr
	}
}
```

Add to `script/host.go`:

```go
// Enqueue hands an intent to the host for main-loop processing and
// returns an IntentID scripts can match against a later Resume.
Enqueue(intent Intent) IntentID
```

**Step 4: Run test**

`go test -v ./script/ -run TestEngineResume`
Expected: PASS.

**Step 5: Commit**

```bash
git add script/engine.go script/host.go script/engine_coroutine_test.go
git commit -m "feat(script): add coroutine tracking and Engine.Resume"
```

---

## Task 3: `cs.await` Lua API

**Files:**
- Modify: `script/api.go` (add `csAwait`)
- Create: `script/api_await_test.go`

**Step 1: Write the failing test**

Use a fake host implementing `Enqueue` to drive an `await` round-trip:

```go
package script

import (
	"testing"

	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
)

type fakeHost struct {
	enqueued []Intent
	ids      []IntentID
}

func (h *fakeHost) SelectedInstance() *session.Instance          { return nil }
func (h *fakeHost) Instances() []*session.Instance               { return nil }
func (h *fakeHost) Workspaces() *config.WorkspaceRegistry        { return nil }
func (h *fakeHost) ConfigDir() string                            { return "" }
func (h *fakeHost) RepoPath() string                             { return "" }
func (h *fakeHost) DefaultProgram() string                       { return "" }
func (h *fakeHost) BranchPrefix() string                         { return "" }
func (h *fakeHost) QueueInstance(*session.Instance)              {}
func (h *fakeHost) Notify(string)                                {}
func (h *fakeHost) Enqueue(intent Intent) IntentID {
	id := newIntentID()
	h.enqueued = append(h.enqueued, intent)
	h.ids = append(h.ids, id)
	return id
}

func TestAwaitYieldsAndResumes(t *testing.T) {
	e := NewEngine(nil)
	defer e.Close()
	h := &fakeHost{}

	// Fake cs.actions.quit via direct Enqueue from Lua.
	e.L.SetGlobal("test_intent", e.L.NewFunction(func(L *lua.LState) int {
		id := h.Enqueue(QuitIntent{})
		e.lastEnqueued = id // helper set up in Task 3 source
		L.Push(lua.LNumber(id))
		return 1
	}))

	err := e.L.DoString(`
		handler = function()
			local r = cs.await(test_intent())
			return r
		end
	`)
	assert.NoError(t, err)

	// Run the handler inside a coroutine to honor cs.await's yield.
	co, _ := e.L.NewThread()
	fn := e.L.GetGlobal("handler").(*lua.LFunction)
	st, _, _ := e.L.Resume(co, fn)
	assert.Equal(t, lua.ResumeYield, st)

	// Host resumes with 7.
	out, err := e.Resume(h.ids[0], lua.LNumber(7))
	assert.NoError(t, err)
	assert.Equal(t, lua.LNumber(7), out)
}
```

**Step 2: Run test**

`go test -v ./script/ -run TestAwaitYields`
Expected: FAIL (`cs.await` undefined).

**Step 3: Implement**

Add to `script/api.go` inside `installAPI`:

```go
cs.RawSetString("await", L.NewFunction(csAwait))
```

Add `csAwait` in `script/api.go`:

```go
// csAwait(intent_id) yields the current coroutine with the id; the
// host resumes the coroutine via Engine.Resume once the intent
// completes, and the resume value flows back as cs.await's return.
func csAwait(L *lua.LState) int {
	id := L.CheckNumber(1)
	// yield the id so Resume can route back to this coroutine
	return L.Yield(lua.LNumber(id))
}
```

Also record `lastEnqueued` on Engine for the test harness (kept internal, `e.lastEnqueued IntentID`).

**Step 4: Run test**

`go test -v ./script/ -run TestAwaitYields`
Expected: PASS.

**Step 5: Commit**

```bash
git add script/api.go script/api_await_test.go
git commit -m "feat(script): add cs.await for coroutine-based intent waiting"
```

---

## Task 4: `cs.bind` and `cs.unbind`

**Files:**
- Modify: `script/api.go` (add `csBind`, `csUnbind`)
- Modify: `script/engine.go` (wrap bound fn in an implicit coroutine; expose `bind`/`unbind` internals)
- Create: `script/api_bind_test.go`

**Step 1: Write the failing test**

```go
func TestCsBindRegistersAndOverrides(t *testing.T) {
	e := NewEngine(map[string]bool{"ctrl+c": true})
	defer e.Close()
	e.BeginLoad("test.lua")
	err := e.L.DoString(`cs.bind("x", function() _G.hit = 1 end, {help="x"})`)
	assert.NoError(t, err)
	e.EndLoad()

	assert.True(t, e.HasAction("x"))
	reg := e.Registrations()
	assert.Equal(t, "x", reg[0].Key)
	assert.Equal(t, "x", reg[0].Help)
}

func TestCsUnbindRemovesBinding(t *testing.T) {
	e := NewEngine(nil)
	defer e.Close()
	e.BeginLoad("t.lua")
	assert.NoError(t, e.L.DoString(`cs.bind("x", function() end)`))
	assert.NoError(t, e.L.DoString(`cs.unbind("x")`))
	e.EndLoad()
	assert.False(t, e.HasAction("x"))
}

func TestCsUnbindCtrlCIsNoop(t *testing.T) {
	e := NewEngine(map[string]bool{"ctrl+c": true})
	defer e.Close()
	e.BeginLoad("t.lua")
	assert.NoError(t, e.L.DoString(`cs.unbind("ctrl+c")`))
	e.EndLoad()
	// ctrl+c stays reserved; no binding existed so nothing to check
	// except: no Lua error propagated.
}
```

**Step 2: Run test**

`go test -v ./script/ -run TestCsBind`
Expected: FAIL.

**Step 3: Implement**

In `script/api.go` `installAPI`, add:

```go
cs.RawSetString("bind",   L.NewFunction(csBind))
cs.RawSetString("unbind", L.NewFunction(csUnbind))
```

Add `csBind`/`csUnbind`. `csBind` wraps the supplied fn in a coroutine-starter via `Engine.register` (`run` becomes the raw fn; the engine's `runAction` starts the coroutine so `cs.await` works inside):

```go
func csBind(L *lua.LState) int {
	key := L.CheckString(1)
	fn := L.CheckFunction(2)
	opts := L.OptTable(3, L.NewTable())
	help := opts.RawGetString("help")
	e := engineFromState(L)
	act := &scriptAction{
		key:  key,
		help: lua.LVAsString(help),
		file: e.curFile,
		run:  fn,
	}
	if err := e.register(act); err != nil {
		L.RaiseError("%s", err.Error())
	}
	return 0
}

func csUnbind(L *lua.LState) int {
	key := L.CheckString(1)
	e := engineFromState(L)
	e.unbind(key)
	return 0
}
```

In `script/engine.go` add:

```go
// unbind removes key from the action map. ctrl+c (and anything else in
// e.reserved) is a no-op with a warning.
func (e *Engine) unbind(key string) {
	if e.reserved[key] {
		log.WarningLog.Printf("script: cs.unbind(%q) is reserved; ignoring", key)
		return
	}
	if _, ok := e.actions[key]; !ok {
		return
	}
	delete(e.actions, key)
	for i, k := range e.order {
		if k == key {
			e.order = append(e.order[:i], e.order[i+1:]...)
			break
		}
	}
}

// BeginLoad / EndLoad bracket a load session so tests (and the
// loader) can call register/unbind.
func (e *Engine) BeginLoad(file string) {
	e.mu.Lock()
	e.loading = true
	e.curFile = file
}
func (e *Engine) EndLoad() {
	e.loading = false
	e.curFile = ""
	e.mu.Unlock()
}
```

Modify `runAction` to execute `act.run` inside a fresh coroutine so `cs.await` yields cleanly. The coroutine is stored on the coroutine map under a dispatch id; when the first `Resume` arrives from the host, it continues the coroutine.

**Step 4: Run test**

`go test -v ./script/ -run TestCsBind`
Expected: PASS.

**Step 5: Commit**

```bash
git add script/api.go script/engine.go script/api_bind_test.go
git commit -m "feat(script): add cs.bind and cs.unbind with coroutine-run actions"
```

---

## Task 5: `cs.register_action` alias

**Files:**
- Modify: `script/api.go`
- Create: `script/api_register_alias_test.go`

**Step 1: Write the failing test**

```go
func TestRegisterActionIsAliasForBind(t *testing.T) {
	e := NewEngine(nil)
	defer e.Close()
	e.BeginLoad("t.lua")
	err := e.L.DoString(`
		cs.register_action{key="y", help="hello", run=function() end}
	`)
	assert.NoError(t, err)
	e.EndLoad()

	reg := e.Registrations()
	assert.Equal(t, "y", reg[0].Key)
	assert.Equal(t, "hello", reg[0].Help)
}

func TestRegisterActionPreconditionGatesRun(t *testing.T) {
	e := NewEngine(nil)
	defer e.Close()
	e.BeginLoad("t.lua")
	err := e.L.DoString(`
		_G.ran = false
		cs.register_action{
			key="z",
			precondition = function() return false end,
			run          = function() _G.ran = true end,
		}
	`)
	assert.NoError(t, err)
	e.EndLoad()

	ok, err := e.Dispatch("z", &fakeHost{})
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, lua.LFalse, e.L.GetGlobal("ran"))
}
```

**Step 2: Run**

`go test -v ./script/ -run TestRegisterAction`
Expected: FAIL (alias not yet wired to new cs.bind path).

**Step 3: Implement**

Rewrite `csRegisterAction` to construct a wrapper fn that runs the precondition then the original `run`, and register it via the same internal path as `cs.bind`. Remove the old direct-register logic.

**Step 4: Run**

`go test -v ./script/ -run TestRegisterAction`
Expected: PASS.

Also run the existing `script/testdata/*.lua` smoke tests (e.g. `push_message.lua`) still load without error.

**Step 5: Commit**

```bash
git add script/api.go script/api_register_alias_test.go
git commit -m "refactor(script): cs.register_action becomes thin alias over cs.bind"
```

---

## Task 6: Sync primitives on Host (CursorUp/Down, ToggleDiff, Workspace*)

**Files:**
- Modify: `script/host.go` (add sync primitive methods)
- Modify: `app/app_scripts.go` (implement on `scriptHost`)
- Create: `script/api_actions.go` (register `cs.actions` table with sync primitives)
- Create: `script/api_actions_test.go`

**Step 1: Write the failing test**

```go
func TestCsActionsSyncPrimitivesCallHost(t *testing.T) {
	e := NewEngine(nil)
	defer e.Close()
	h := &recordingHost{fakeHost: fakeHost{}}
	e.BeginLoad("t.lua")
	assert.NoError(t, e.L.DoString(`
		cs.bind("a", function() cs.actions.cursor_up() end)
		cs.bind("b", function() cs.actions.cursor_down() end)
		cs.bind("c", function() cs.actions.toggle_diff() end)
		cs.bind("d", function() cs.actions.workspace_prev() end)
		cs.bind("e", function() cs.actions.workspace_next() end)
	`))
	e.EndLoad()

	for _, k := range []string{"a", "b", "c", "d", "e"} {
		_, err := e.Dispatch(k, h)
		assert.NoError(t, err)
	}
	assert.Equal(t, []string{"CursorUp", "CursorDown", "ToggleDiff", "WorkspacePrev", "WorkspaceNext"}, h.calls)
}
```

Where `recordingHost` embeds `fakeHost` and appends the method name to `h.calls` for each primitive.

**Step 2: Run**

`go test -v ./script/ -run TestCsActionsSync`
Expected: FAIL.

**Step 3: Implement**

Add to `Host`:

```go
CursorUp()
CursorDown()
ToggleDiff()
WorkspacePrev()
WorkspaceNext()
```

Implement on `scriptHost` in `app/app_scripts.go` by calling into `m.list` and `m.workspaces*` helpers (match the current `runCursorUp`/`runCursorDown`/`runToggleDiff`/`runWorkspaceLeft`/`runWorkspaceRight` bodies in `app/actions_nav.go` and `app/actions_workspace.go`). These calls must be safe on the dispatch goroutine — inspect existing functions; if they post `tea.Cmd`s, extract the pure-state portion here and keep the cmd-building work for the main loop via intents where needed. For the five listed, all state changes are local to `m.list`/registry and require no `tea.Cmd`.

Add `script/api_actions.go`:

```go
func installActions(L *lua.LState, e *Engine) {
	actions := L.NewTable()
	actions.RawSetString("cursor_up", L.NewFunction(func(L *lua.LState) int {
		e.curHost.CursorUp(); return 0
	}))
	// ... cursor_down, toggle_diff, workspace_prev, workspace_next
	L.GetGlobal("cs").(*lua.LTable).RawSetString("actions", actions)
}
```

Call `installActions(L, e)` from `NewEngine` after `installAPI`.

**Step 4: Run**

`go test -v ./script/... ./app/...`
Expected: PASS.

**Step 5: Commit**

```bash
git add script/host.go script/api_actions.go script/api_actions_test.go app/app_scripts.go
git commit -m "feat(script): expose sync cs.actions primitives via Host"
```

---

## Task 7: Deferred cs.actions (quit, push, kill, checkout, resume)

**Files:**
- Modify: `script/api_actions.go`
- Modify: `script/api_actions_test.go`

**Step 1: Write the failing test**

```go
func TestCsActionsQuitEnqueuesIntent(t *testing.T) {
	e := NewEngine(nil)
	defer e.Close()
	h := &fakeHost{}
	e.BeginLoad("t.lua")
	assert.NoError(t, e.L.DoString(`
		cs.bind("q", function() cs.actions.quit() end)
	`))
	e.EndLoad()

	_, err := e.Dispatch("q", h)
	assert.NoError(t, err)
	assert.Len(t, h.enqueued, 1)
	_, ok := h.enqueued[0].(QuitIntent)
	assert.True(t, ok)
}

func TestCsActionsPushSelectedRespectsConfirmOpt(t *testing.T) {
	e := NewEngine(nil)
	defer e.Close()
	h := &fakeHost{}
	e.BeginLoad("t.lua")
	assert.NoError(t, e.L.DoString(`
		cs.bind("p", function() cs.actions.push_selected{confirm=false} end)
	`))
	e.EndLoad()

	_, err := e.Dispatch("p", h)
	assert.NoError(t, err)
	intent := h.enqueued[0].(PushSelectedIntent)
	assert.False(t, intent.Confirm)
}
```

Add similar tests for kill, checkout (`{confirm, help}`), resume.

**Step 2: Run** → FAIL.

**Step 3: Implement**

In `script/api_actions.go` add one Lua function per primitive. Pattern:

```go
actions.RawSetString("push_selected", L.NewFunction(func(L *lua.LState) int {
	opts := L.OptTable(1, L.NewTable())
	confirm := true
	if v := opts.RawGetString("confirm"); v != lua.LNil {
		confirm = lua.LVAsBool(v)
	}
	id := e.curHost.Enqueue(PushSelectedIntent{Confirm: confirm})
	// yield the id so cs.await routes back here on Resume
	return L.Yield(lua.LNumber(id))
}))
```

Note: because every bound handler runs inside an implicit coroutine (Task 4), a plain `L.Yield` here works. The handler that *calls* `cs.actions.quit()` without `cs.await` still yields — that's fine; quit never resolves so the coroutine is dropped when the app exits.

Implement `quit`, `push_selected`, `kill_selected`, `checkout_selected`, `resume_selected` identically with their opt-flag parsing.

**Step 4: Run** → PASS.

**Step 5: Commit**

```bash
git add script/api_actions.go script/api_actions_test.go
git commit -m "feat(script): add deferred cs.actions primitives (quit/push/kill/checkout/resume)"
```

---

## Task 8: Deferred cs.actions (new_instance, show_help, workspace_picker)

**Files:**
- Modify: `script/api_actions.go`
- Modify: `script/api_actions_test.go`

**Step 1: Write the failing test**

Cover `new_instance{prompt=true, title="x"}`, `show_help`, `open_workspace_picker` — each must enqueue the matching intent with the right fields.

**Step 2: Run** → FAIL.

**Step 3: Implement**

Same pattern as Task 7. `new_instance` parses both `prompt` (bool) and `title` (string); `show_help` and `open_workspace_picker` take no opts.

**Step 4: Run** → PASS.

**Step 5: Commit**

```bash
git add script/api_actions.go script/api_actions_test.go
git commit -m "feat(script): add cs.actions.new_instance/show_help/open_workspace_picker"
```

---

## Task 9: Deferred cs.actions (attach + quick_input)

**Files:**
- Modify: `script/api_actions.go`
- Modify: `script/api_actions_test.go`

**Step 1: Write the failing test**

Cover `inline_attach_agent`, `inline_attach_terminal`, `fullscreen_attach_agent`, `fullscreen_attach_terminal`, `quick_input_agent`, `quick_input_terminal`. Assert the enqueued intent has the right pane constant.

**Step 2: Run** → FAIL.

**Step 3: Implement**

Register the six primitives; each enqueues the matching intent with `Pane: AttachPaneAgent` or `AttachPaneTerminal`.

**Step 4: Run** → PASS.

**Step 5: Commit**

```bash
git add script/api_actions.go script/api_actions_test.go
git commit -m "feat(script): add cs.actions attach + quick_input primitives"
```

---

## Task 10: App intent dispatch — message types and router

**Files:**
- Modify: `app/app_scripts.go` (add `scriptResumeMsg`, intent channel in `scriptDoneMsg`)
- Modify: `app/app.go` (route `scriptResumeMsg` in `Update`)
- Create: `app/app_scripts_intent_test.go`

**Step 1: Write the failing test**

```go
func TestHandleScriptResumeReroutesToEngine(t *testing.T) {
	m := newTestHome(t)
	e := m.scripts
	e.BeginLoad("t.lua")
	assert.NoError(t, e.L.DoString(`
		cs.bind("q", function()
			local v = cs.await(cs.actions.quit())
			_G.after = v
		end)
	`))
	e.EndLoad()

	cmd, ok := m.dispatchScript("q")
	assert.True(t, ok)

	msg := cmd().(scriptDoneMsg)
	assert.Len(t, msg.pendingIntents, 1)
	_, ok = msg.pendingIntents[0].intent.(QuitIntent)
	assert.True(t, ok)
}
```

**Step 2: Run** → FAIL (`pendingIntents` field missing).

**Step 3: Implement**

Expand the `scriptDoneMsg` struct:

```go
type pendingIntent struct {
	id     script.IntentID
	intent script.Intent
}

type scriptDoneMsg struct {
	err              error
	pendingInstances []*session.Instance
	notices          []string
	pendingIntents   []pendingIntent
}

type scriptResumeMsg struct {
	id    script.IntentID
	value lua.LValue
}
```

Add `Enqueue(intent) IntentID` on `scriptHost`:

```go
func (s *scriptHost) Enqueue(intent script.Intent) script.IntentID {
	id := script.NewIntentID()
	s.mu.Lock()
	s.intents = append(s.intents, pendingIntent{id: id, intent: intent})
	s.mu.Unlock()
	return id
}
```

Drain intents alongside pending instances and notices. Add a new `Update` case for `scriptResumeMsg` that calls `m.scripts.Resume(msg.id, msg.value)`, then drains any newly emitted intents and continues the dispatch loop.

**Step 4: Run** → PASS.

**Step 5: Commit**

```bash
git add app/app_scripts.go app/app.go app/app_scripts_intent_test.go
git commit -m "feat(app): add scriptResumeMsg and intent queue drain"
```

---

## Task 11: App intent handlers — wire each intent to existing runXYZ

**Files:**
- Modify: `app/app.go` (add `handleScriptIntent(pendingIntent) tea.Cmd`)
- Create: `app/app_scripts_dispatch_test.go`

**Step 1: Write the failing test**

For each intent type, assert `handleScriptIntent` returns a `tea.Cmd` whose first message matches the corresponding legacy flow. Use the existing `runXYZ` functions as the source of truth — e.g. `PushSelectedIntent{Confirm:true}` should match the cmd returned by `m.runSubmitSelected()`.

```go
func TestHandleScriptIntentPushSelected(t *testing.T) {
	m := newTestHome(t)
	m.list.AddInstance(testInstance(t))
	cmd := m.handleScriptIntent(pendingIntent{
		id:     script.NewIntentID(),
		intent: script.PushSelectedIntent{Confirm: true},
	})
	msg := cmd()
	// runSubmitSelected enters an overlay via stateConfirm — assert
	// that the same confirm overlay was triggered.
	_, ok := msg.(overlay.ConfirmMsg)
	assert.True(t, ok)
}
```

Add one test per intent type (11 intents → ~11 subtests; use `t.Run`).

**Step 2: Run** → FAIL.

**Step 3: Implement**

Write `handleScriptIntent` as a type switch:

```go
func (m *home) handleScriptIntent(p pendingIntent) tea.Cmd {
	switch i := p.intent.(type) {
	case script.QuitIntent:
		return tea.Quit
	case script.PushSelectedIntent:
		if !i.Confirm {
			return m.runSubmitSelectedNoConfirm()
		}
		return m.runSubmitSelected()
	case script.KillSelectedIntent:
		if !i.Confirm {
			return m.runKillSelectedNoConfirm()
		}
		return m.runKillSelected()
	// ... etc for every intent type
	}
	return nil
}
```

For intents whose corresponding `runXYZ` doesn't currently accept an opt-flag (`confirm=false`, `help=false`), add a sibling `runXYZNoConfirm`/`runXYZSilent` helper that skips the overlay and calls the underlying action directly. Keep the original runXYZ for now — it's still called from ActionRegistry until Task 15.

After each intent completes (its overlay closes, push finishes, etc.), post a `scriptResumeMsg` with the resolution value. For `QuitIntent`, `tea.Quit` ends the program, so no resume is necessary. For flows that resolve (push/kill/checkout/new_instance/workspace_picker/quick_input), wrap the existing overlay's confirm path to emit `scriptResumeMsg{id: p.id, value: luaResult}` after dismissal.

**Step 4: Run** → PASS.

**Step 5: Commit**

```bash
git add app/app.go app/app_scripts_dispatch_test.go
git commit -m "feat(app): dispatch script intents via existing runXYZ handlers"
```

---

## Task 12: Embed `defaults.lua` and load before user scripts

**Files:**
- Create: `script/defaults.lua`
- Modify: `script/loader.go` (embed + load defaults first)
- Create: `script/loader_defaults_test.go`

**Step 1: Write the failing test**

```go
func TestEngineLoadsEmbeddedDefaults(t *testing.T) {
	e := NewEngine(nil)
	defer e.Close()
	e.LoadDefaults()
	keys := e.actionKeys()
	// All 21 stock keys from defaults.lua should now be bound.
	for _, k := range []string{"up","k","down","j","n","N","D","p","c","r","?","q","W","[","l","]",";","alt+a","alt+t","ctrl+a","ctrl+t","a","t","d"} {
		assert.Contains(t, keys, k)
	}
}

func TestUserScriptOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "user.lua"), []byte(`
		cs.unbind("j")
		cs.bind("j", function() _G.user_j = true end)
	`), 0o644))

	e := NewEngine(nil)
	defer e.Close()
	e.LoadDefaults()
	e.Load(dir)

	// j should point to the user handler.
	_, err := e.Dispatch("j", &fakeHost{})
	assert.NoError(t, err)
	assert.Equal(t, lua.LTrue, e.L.GetGlobal("user_j"))
}
```

**Step 2: Run** → FAIL (`LoadDefaults` undefined, `defaults.lua` missing).

**Step 3: Implement**

Create `script/defaults.lua` as shown in the design doc — all 21 stock bindings via `cs.bind`. Embed via `//go:embed defaults.lua` in `script/loader.go`:

```go
//go:embed defaults.lua
var defaultsLua []byte

// LoadDefaults compiles the embedded defaults. A parse error here is
// a build-time bug, so it panics.
func (e *Engine) LoadDefaults() {
	e.BeginLoad("<defaults>")
	defer e.EndLoad()
	if err := e.L.DoString(string(defaultsLua)); err != nil {
		panic(fmt.Errorf("defaults.lua: %w", err))
	}
}
```

Call `LoadDefaults` in `initScripts` *before* `Load(dir)` so user scripts can override via `cs.unbind` + `cs.bind`.

**Step 4: Run** → PASS.

**Step 5: Commit**

```bash
git add script/defaults.lua script/loader.go script/loader_defaults_test.go
git commit -m "feat(script): embed defaults.lua and load before user scripts"
```

---

## Task 13: `--no-scripts` CLI flag + user parse-error fallback

**Files:**
- Modify: `main.go`
- Modify: `app/app_scripts.go` (accept `skipUserScripts` bool)
- Create: `app/app_scripts_fallback_test.go`

**Step 1: Write the failing test**

```go
func TestNoScriptsFlagSkipsUserDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "user.lua"),
		[]byte(`cs.bind("x", function() end)`), 0o644))

	h := &home{/* ...minimal setup... */}
	initScriptsIn(h, dir, true /* skipUserScripts */)
	assert.False(t, h.scripts.HasAction("x"))
}

func TestUserParseErrorKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.lua"),
		[]byte(`this is not valid lua`), 0o644))

	h := &home{/* ... */}
	initScriptsIn(h, dir, false)
	// Defaults should still be loaded; no panic.
	assert.True(t, h.scripts.HasAction("q"))
}
```

**Step 2: Run** → FAIL.

**Step 3: Implement**

Add `--no-scripts` persistent flag on the root cobra command in `main.go`; pass it through to `home`. Refactor `initScripts` into `initScriptsIn(h, dir, skipUser)`:

```go
func initScriptsIn(h *home, dir string, skipUser bool) {
	h.scripts = script.NewEngine(buildReserved())
	h.scripts.LoadDefaults()
	if !skipUser {
		h.scripts.Load(dir)
	}
}
```

Existing `script.Engine.Load` already tolerates per-file errors, so the parse-error test should pass immediately once defaults are loading first.

**Step 4: Run** → PASS.

**Step 5: Commit**

```bash
git add main.go app/app_scripts.go app/app_scripts_fallback_test.go
git commit -m "feat(app): add --no-scripts flag and keep defaults on user parse error"
```

---

## Task 14: Hard-reserve `ctrl+c` in `state_default`

**Files:**
- Modify: `app/state_default.go`
- Create: `app/state_default_ctrlc_test.go`

**Step 1: Write the failing test**

```go
func TestCtrlCQuitsEvenAfterUnbind(t *testing.T) {
	m := newTestHome(t)
	m.scripts.BeginLoad("t.lua")
	assert.NoError(t, m.scripts.L.DoString(`cs.unbind("ctrl+c")`))
	m.scripts.EndLoad()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.Equal(t, "tea.quitMsg", fmt.Sprintf("%T", cmd()))
}
```

**Step 2: Run** → FAIL.

**Step 3: Implement**

At the top of `(m *home).updateDefault` in `state_default.go`, before any engine dispatch:

```go
if msg, ok := msg.(tea.KeyMsg); ok && msg.String() == "ctrl+c" {
	return m, tea.Quit
}
```

**Step 4: Run** → PASS.

**Step 5: Commit**

```bash
git add app/state_default.go app/state_default_ctrlc_test.go
git commit -m "feat(app): hard-reserve ctrl+c ahead of script dispatch"
```

---

## Task 15: Retire `ActionRegistry` and shrink `keys/keys.go`

**Files:**
- Delete: `app/actions.go`, `app/actions_lifecycle.go`, `app/actions_attach.go`, `app/actions_nav.go`, `app/actions_quick.go`, `app/actions_workspace.go` (move surviving `runXYZ` helpers to `app/intents.go`)
- Create: `app/intents.go` (houses the helpers still called from `handleScriptIntent`)
- Modify: `keys/keys.go` (shrink to `KeySubmitName`)
- Modify: `app/state_default.go` (remove `ActionRegistry.Dispatch`; route keys straight to `dispatchScript`)
- Modify: `app/help.go` (rebuild help overlay from `Engine.Registrations()`)

**Step 1: Write the failing test**

Smoke test the dispatcher:

```go
func TestDefaultStateRoutesKeysThroughScripts(t *testing.T) {
	m := newTestHome(t)
	_, cmd := m.updateDefault(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	// defaults.lua's "n" → cs.actions.new_instance{} → NewInstanceIntent
	msg := cmd().(scriptDoneMsg)
	assert.Len(t, msg.pendingIntents, 1)
	_, ok := msg.pendingIntents[0].intent.(script.NewInstanceIntent)
	assert.True(t, ok)
}
```

**Step 2: Run** → FAIL (`ActionRegistry` still intercepts the key).

**Step 3: Implement**

- Move every `runXYZ` function the intent router still needs into `app/intents.go`. Delete the rest.
- Delete every `app/actions*.go` file except any that still contain live helpers.
- In `app/state_default.go`, remove the `ActionRegistry.Dispatch` call; fall through directly to `m.dispatchScript(key)`.
- In `keys/keys.go`, reduce to:
  ```go
  type KeyName int
  const (
      KeySubmitName KeyName = iota
  )
  var GlobalkeyBindings = map[KeyName]key.Binding{
      KeySubmitName: key.NewBinding(
          key.WithKeys("enter"),
          key.WithHelp("enter", "submit name"),
      ),
  }
  ```
  Remove `GlobalKeyStringsMap`, `HelpPanelDescriptions`, and all retired `KeyName` entries.
- Update `initScripts`'s `buildReserved` to reserve only `ctrl+c` (since the user-facing keymap is now fully Lua-owned).
- Update `app/help.go` to pull entries from `m.scripts.Registrations()` rather than `HelpPanelDescriptions`.

**Step 4: Run** → PASS. Also run the full suite: `go test -v ./...`.

**Step 5: Commit**

```bash
git add app/ keys/
git commit -m "refactor: retire Go ActionRegistry in favor of Lua-driven keymap"
```

---

## Task 16: Migration parity tests

**Files:**
- Create: `app/migration_parity_test.go`

**Step 1: Write the failing test**

For every retired key, assert the Lua dispatch path emits the same intent the legacy runXYZ would have triggered. Example table-driven test:

```go
func TestMigrationParity(t *testing.T) {
	cases := []struct {
		name       string
		key        string
		wantIntent script.Intent
	}{
		{"quit",       "q", script.QuitIntent{}},
		{"new",        "n", script.NewInstanceIntent{}},
		{"new_prompt", "N", script.NewInstanceIntent{Prompt: true}},
		{"kill",       "D", script.KillSelectedIntent{Confirm: true}},
		{"push",       "p", script.PushSelectedIntent{Confirm: true}},
		{"checkout",   "c", script.CheckoutIntent{Confirm: true, Help: true}},
		{"resume",     "r", script.ResumeIntent{}},
		{"help",       "?", script.ShowHelpIntent{}},
		{"workspace",  "W", script.WorkspacePickerIntent{}},
		{"ws_prev",    "[", nil /* sync primitive — assert CursorUp side-effect */},
		// ...
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestHome(t)
			_, cmd := m.updateDefault(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.key)})
			if tc.wantIntent == nil {
				return // sync primitive tested separately
			}
			msg := cmd().(scriptDoneMsg)
			require.Len(t, msg.pendingIntents, 1)
			assert.Equal(t, tc.wantIntent, msg.pendingIntents[0].intent)
		})
	}
}
```

**Step 2: Run** → Should PASS immediately given defaults.lua wires everything 1:1. If anything fails, fix it in `defaults.lua` or the matching primitive.

**Step 3-4: N/A (test-only task)**

**Step 5: Commit**

```bash
git add app/migration_parity_test.go
git commit -m "test(app): migration parity for retired hotkeys"
```

---

## Task 17: Update `docs/specs/scripting.md`

**Files:**
- Modify: `docs/specs/scripting.md`

**Step 1:** Rewrite the spec to describe:
- `cs.bind(key, fn, {help})`, `cs.unbind(key)`, `cs.register_action{}` alias
- `cs.actions.*` primitive catalog (sync vs. deferred, opt-flag tables)
- `cs.await(intent)` semantics and coroutine behavior
- Intent lifecycle (6-step enqueue → yield → Cmd → runXYZ → resume → continue)
- Safety rails: `--no-scripts`, embedded defaults + user fallback, hard-reserved `ctrl+c`
- Reference `script/defaults.lua` as the canonical source of truth for the stock keymap

**Step 2: Commit**

```bash
git add docs/specs/scripting.md
git commit -m "docs: update scripting spec for bind/actions/await migration"
```

---

## Task 18: Final verification

**Step 1: Build + test**

```bash
CGO_ENABLED=0 go build -o claude-squad
go test -v ./...
gofmt -w .
golangci-lint run --timeout=3m --fast
```

All green.

**Step 2: Smoke test the TUI**

Run `./claude-squad`. Manually exercise every default binding:

- `j`/`k` navigate
- `n`/`N` create new session (and prompt variant)
- `D` kill with confirm
- `p` push with confirm
- `c` checkout
- `r` resume
- `?` help
- `W` workspace picker, `[`/`]` switch tabs
- `alt+a`/`alt+t` fullscreen attach, `ctrl+a`/`ctrl+t` inline attach
- `a`/`t` quick input
- `d` diff toggle
- `q` quit

Each should behave identically to pre-migration.

**Step 3: Override test**

Drop `~/.claude-squad/scripts/override.lua`:

```lua
cs.unbind("q")
cs.bind("q", function() cs.notify("blocked quit") end)
cs.bind("Q", cs.actions.quit)
```

Verify `q` shows the notice and `Q` actually quits.

**Step 4: Lockout test**

Drop `~/.claude-squad/scripts/broken.lua` with invalid Lua. Relaunch; verify a warning is logged and defaults still work.

Launch with `--no-scripts`; confirm the user override is skipped and `q` quits.

**Step 5: Final commit (if any cleanup)**

---

## Rollback

If the migration needs to be reverted, `git revert 768ac7b..HEAD` on the branch and re-run tests. The design doc stays in place for future reference.
