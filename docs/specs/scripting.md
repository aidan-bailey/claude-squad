# Scripting

Claude Squad ships an embedded Lua runtime that lets users bind custom keys in the TUI to user-authored actions. Scripts live in `~/.claude-squad/scripts/`, are loaded at startup, and are dispatched after the built-in keymap misses. The runtime is sandboxed and single-threaded.

The implementation is Lua 5.1 via [`github.com/yuin/gopher-lua`](https://github.com/yuin/gopher-lua), vendored in `vendor/github.com/yuin/gopher-lua`.

## Concepts

### Engine

A single `gopher-lua` state plus the registered action table. One `Engine` lives for the lifetime of the app, owned by the `*home` model.

```go
// script/engine.go
type Engine struct {
    mu       sync.Mutex
    L        *lua.LState
    actions  map[string]*scriptAction
    order    []string        // insertion order for Registrations()
    loading  bool            // true only inside Load()
    curFile  string          // script file currently being compiled
    reserved map[string]bool // raw key strings owned by built-ins
    curHost  Host            // Host active for the current dispatch
    logs     []LogEntry      // buffered ctx:log / cs.log output
}
```

`*lua.LState` is not goroutine-safe, so every entry point takes `e.mu`. See [Concurrency](#concurrency).

### Host

An interface implemented by `app/app_scripts.go#scriptHost` that lets the engine touch live TUI state without importing `app/` (which would be a cycle).

```go
// script/host.go
type Host interface {
    SelectedInstance() *session.Instance
    Instances() []*session.Instance
    Workspaces() *config.WorkspaceRegistry
    ConfigDir() string
    RepoPath() string
    DefaultProgram() string
    BranchPrefix() string
    QueueInstance(inst *session.Instance)
    Notify(msg string)
}
```

A fresh `scriptHost` is allocated per dispatch so pending instances and notices from one script can't leak into another.

### Script Action

The unit of registration: a key binding plus optional precondition and required run function.

```go
// script/engine.go
type scriptAction struct {
    key          string
    help         string
    file         string // source file, cited in error logs
    precondition *lua.LFunction
    run          *lua.LFunction
}
```

### Context (`ctx`)

A userdata value handed to `precondition` and `run` on every dispatch. Lives for the duration of a single call, then is discarded.

```lua
cs.register_action{
  key = "ctrl+shift+p",
  run = function(ctx)
    local inst = ctx:selected()
    if inst then
      ctx:notify("hello from " .. inst:title())
    end
  end,
}
```

`ctx` exposes [methods](#ctx-methods) that forward to the `Host` interface.

## Directory Layout

Scripts are **always global** — stored at `~/.claude-squad/scripts/`, not inside a workspace's `.claude-squad/scripts/`. This is a deliberate choice (see [Design Decisions](#design-decisions)).

```
~/.claude-squad/
├── config.json
├── state.json
├── workspaces.json
└── scripts/
    ├── push_branch.lua
    ├── resume_all.lua
    └── ...
```

`app/app_scripts.go#scriptsDir` resolves this path via `config.GetConfigDir()` — the global variant, not workspace-scoped. Files are loaded in alphabetical order (`loader.go#loadScripts`).

## Security

This is an **allow-list sandbox**. New `gopher-lua` versions cannot widen the attack surface without an explicit code change in `script/sandbox.go`.

### Allowed Standard Libraries

Only these five libraries are opened:

| Library | Purpose |
|---------|---------|
| `base` | Arithmetic, type introspection, `print`, `tostring`, `error`, `pcall`, etc. |
| `string` | String manipulation, pattern matching (minus `string.dump`). |
| `table` | Table manipulation. |
| `math` | Arithmetic and trig. |
| `coroutine` | Cooperative multitasking primitives. |

**Not opened**: `io`, `os`, `debug`, `package`. Script code has no way to read files, execute shell commands, access environment variables, or introspect the Go runtime.

### Stripped Globals

Even inside the allowed set, these escape hatches are nil'd out after library load:

| Name | Why it's removed |
|------|-----------------|
| `load`, `loadstring` | Execute arbitrary source at runtime. |
| `loadfile`, `dofile` | Pull source from disk outside the loader. |
| `require` | Module loading via `package` (which is never opened, but defense-in-depth). |
| `collectgarbage` | Could be used to probe the Go runtime; no legitimate script use. |
| `string.dump` | Serializes a function to bytecode, which `gopher-lua` can execute — bypasses our source-only load path. |

Source: `script/sandbox.go:42-58`.

### Userdata Boundary

All host objects (`session.Instance`, `git.GitWorktree`, `ctx`) are exposed as opaque userdata with metatables that restrict access to an explicit method list. Scripts cannot read Go struct fields directly or reach into unexposed methods.

### Untrusted Scripts

Scripts are user-provided, not downloaded. The sandbox protects against an author's *mistake* (e.g. accidentally calling a destructive API in a wide-matching precondition) rather than a malicious script — a malicious script can still kill every instance, spam the log, or consume CPU. Users should treat `~/.claude-squad/scripts/` the same way they treat `~/.bashrc`.

## API Reference

### Global `cs` Table

Installed as a global at engine construction (`script/api.go`).

| Symbol | Signature | Description |
|--------|-----------|-------------|
| `cs.register_action` | `{key, help, precondition?, run}` → void | Register a key binding. **Load-time only.** Raises a Lua error if called from inside a dispatched action. |
| `cs.log` | `(level: string, msg: string)` → void | Buffer a log entry. Drained by the app into the main log file. `level` is free-form (`"info"`, `"warn"`, `"error"` are conventional). |
| `cs.notify` | `(msg: string)` → void | Send a transient message to the error/info bar. When called at load time (no active dispatch), downgrades to a log entry. |
| `cs.now` | `()` → number | Unix time in seconds. |
| `cs.sprintf` | `(fmt, ...)` → string | Alias for `string.format`. Forgiving — non-string args are `tostring`'d before substitution. |

### `ctx` Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `ctx:selected()` | → instance\|nil | The focused instance in the list panel. |
| `ctx:instances()` | → instance[] | 1-indexed array of every tracked instance. Mutating the array does nothing; use per-instance methods. |
| `ctx:find(title)` | → instance\|nil | First instance with a matching title. |
| `ctx:config_dir()` | → string | Resolved config directory for the active workspace. |
| `ctx:repo_path()` | → string | Repo root new instances should be created against. |
| `ctx:default_program()` | → string | The configured default agent command (e.g. `"claude"`). |
| `ctx:branch_prefix()` | → string | The branch prefix for the active workspace (e.g. `"alice/"`). |
| `ctx:new_instance{title=, ...}` | → instance | Create a new session. Required: `title`. Optional: `program`, `path`, `prompt`, `branch`, `auto_yes`. The instance is queued — actual addition to the list happens on the main goroutine after the script returns. |
| `ctx:log(level, msg)` | → void | Equivalent to `cs.log`. |
| `ctx:notify(msg)` | → void | Equivalent to `cs.notify` when dispatch is active. |

### `instance` Methods

Wraps `*session.Instance`. Obtained from `ctx:selected()`, `ctx:instances()`, `ctx:find()`, or `ctx:new_instance{}`.

| Method | Returns | Description |
|--------|---------|-------------|
| `inst:title()` | string | Session title. |
| `inst:status()` | string | Lowercase status (`"ready"`, `"loading"`, `"running"`, `"paused"`). |
| `inst:branch()` | string | Git branch name. |
| `inst:path()` | string | Repo path for this session. |
| `inst:program()` | string | Agent command. |
| `inst:auto_yes()` | bool | Auto-yes flag. |
| `inst:started()` | bool | True once the tmux session has been created. |
| `inst:paused()` | bool | True while the worktree is torn down. |
| `inst:diff_stats()` | {added, removed, content} \| nil | Diff stats. Nil if not yet computed. |
| `inst:preview()` | string, err? | Tmux pane contents. Returns `(nil, errmsg)` on failure. |
| `inst:send_keys(keys)` | void | Raw tmux `send-keys`. Raises on error. |
| `inst:send_prompt(text)` | void | Send text followed by Enter. Raises on error. |
| `inst:tap_enter()` | void | Send a single Enter keystroke. |
| `inst:pause()` | void | Pause the session. Raises on error. |
| `inst:resume()` | void | Resume a paused session. Raises on error. |
| `inst:kill()` | void | Kill and clean up. Raises on error. |
| `inst:worktree()` | worktree\|nil | The git worktree, or nil if none is attached (paused / unstarted / workspace terminal). |
| `tostring(inst)` | string | `instance(title, status)` for debugging. |

### `worktree` Methods

Wraps `*git.GitWorktree`. Obtained from `inst:worktree()`.

| Method | Returns | Description |
|--------|---------|-------------|
| `wt:branch_name()` | string | Branch name (e.g. `"alice/my_feature"`). |
| `wt:path()` | string | Absolute worktree path on disk. |
| `wt:repo_path()` | string | Absolute repo root (parent of the worktree). |
| `wt:is_dirty()` | bool, err? | True if the worktree has uncommitted changes. Returns `(nil, errmsg)` on git failure. |
| `wt:is_checked_out()` | bool, err? | True if the branch is checked out elsewhere. |
| `wt:commit(msg)` | void | Commit all changes with `msg`. Raises on error. |
| `wt:push(msg, open?)` | void | Commit and push. `open=true` opens a browser to the push URL; defaults to `false`. Raises on error. |

## Registration Rules

`cs.register_action` is **load-time only** — the engine sets `loading=true` inside `Load()` and rejects registration outside that window with a Lua error.

### Key Collisions

Collision handling is deterministic and logged, never fatal:

| Collision | Behavior |
|-----------|----------|
| Against a built-in key (in `keys.GlobalKeyStringsMap` or `ctrl+q`) | Registration skipped, warning logged. Built-ins always win. |
| Against an already-registered script key | Registration skipped, warning logged. First-loaded wins (files load alphabetically). |

Scripts cannot rebind built-in keys. If you need `n` to do something custom, pick a different key.

### Required vs Optional Fields

```lua
cs.register_action{
  key = "ctrl+shift+r",       -- required, string
  help = "Resume all",        -- optional, string; shown in help panel
  precondition = function(ctx) -- optional, function(ctx) -> bool
    return #ctx:instances() > 0
  end,
  run = function(ctx) ... end, -- required, function(ctx) -> void
}
```

A `precondition` that returns falsy silently skips the action (useful for contextual keys that should no-op when selection is empty). A precondition that raises an error surfaces as a dispatch error.

## Dispatch Flow

```
User keystroke
     │
     ▼
app/app.go: handleKeyPress
     │
     ▼
state_default.go: ActionRegistry.Dispatch
     │
     ├── hit  → built-in action runs on main goroutine
     │
     └── miss ──► app_scripts.go: dispatchScript(key)
                      │
                      ├── Engine.HasAction(key) false → return (nil, false)
                      │                                     │
                      │                                     ▼
                      │                            caller no-ops key
                      │
                      └── true → return tea.Cmd, true
                                      │
                                      ▼
                               goroutine: Engine.Dispatch(key, host)
                                      │ (holds engine.mu)
                                      │
                                      ├── precondition → bail if falsy
                                      ├── run(ctx)
                                      │
                                      ▼
                               scriptDoneMsg{err, pending, notices}
                                      │
                                      ▼
                               Update: handleScriptDone
                                      │
                                      ├── for each pending inst: list.AddInstance
                                      ├── for each notice: errBox
                                      └── if err: errBox
```

Source: `app/app_scripts.go#dispatchScript`, `app/app_scripts.go#handleScriptDone`.

## Concurrency

`gopher-lua` is not goroutine-safe. The engine guarantees serialized Lua execution through `Engine.mu`:

1. `HasAction` — takes the mutex, cheap map lookup, releases. Called on the main goroutine to decide whether to schedule a dispatch.
2. `Dispatch` — takes the mutex, runs the precondition and run function under it, releases. Called from a `tea.Cmd` goroutine so the Bubble Tea main loop stays responsive while Lua executes.

**What this means for scripts**:
- A slow script blocks other scripts but not the TUI.
- Two keys bound to the same long-running script serialize.
- Scripts see a consistent view of the host state *between* host method calls, but not *across* the whole dispatch — a script that reads `ctx:instances()` twice may see different results if the main goroutine mutated the list in between.

**What this means for the app**:
- `h.list.AddInstance` must run on the main goroutine. Scripts queue instances via `Host.QueueInstance`; finalization happens in `handleScriptDone`. Never call `AddInstance` from inside the Lua VM.
- Notices are buffered and surfaced through `scriptDoneMsg` so the error-bar update happens on the main loop.

## Error Handling

| Failure mode | Surfaced as |
|--------------|-------------|
| Script file fails to parse | Warning in the main log; load continues with remaining files. |
| `run` function raises a Lua error | Wrapped as `<file>: <error>`, returned from `Dispatch`, shown in the error bar. |
| `precondition` raises | Wrapped as `<file>: precondition: <error>`, shown in the error bar. |
| Go panic inside userdata (shouldn't happen) | Recovered, Lua stack drained, wrapped as `script <file> panic: ...`. |
| Host method returns an error (e.g. `inst:send_keys` on a dead tmux session) | The userdata method raises a Lua error, which becomes a dispatch error via the above. |

Script log output via `cs.log` / `ctx:log` is buffered and drained asynchronously by the app — no dispatch-time coupling to the log subsystem.

## Example Scripts

Three reference scripts ship in `script/testdata/`. Copy to `~/.claude-squad/scripts/` to activate.

- `push_message.lua` — push the selected branch with a timestamped commit.
- `resume_all.lua` — resume every paused session.
- `spawn_instance.lua` — create a new session with a prefilled prompt.

## Key Source Files

| File | Role |
|------|------|
| `script/engine.go` | `Engine` lifecycle, `Dispatch`, `Load`, registration bookkeeping. |
| `script/sandbox.go` | Allow-list lib loader, escape-hatch stripping. See [Security](#security). |
| `script/api.go` | Installs the `cs` global table. |
| `script/loader.go` | Walks `~/.claude-squad/scripts/`, runs each `.lua` file under `loading=true`. |
| `script/host.go` | The `Host` interface. |
| `script/userdata_ctx.go` | `ctx` userdata metatable and methods. |
| `script/userdata_instance.go` | `instance` userdata metatable and methods. |
| `script/userdata_worktree.go` | `worktree` userdata metatable and methods. |
| `app/app_scripts.go` | `scriptHost` adapter, `initScripts`, `dispatchScript`, `handleScriptDone`. |
| `app/state_default.go` | Dispatch fallthrough to the script engine after built-in miss. |
| `script/testdata/` | Sample scripts. |

## Design Decisions

**Lua, not JS/Python/a custom DSL.** `gopher-lua` is pure Go (no cgo, matches our `CGO_ENABLED=0` build), small, and embeddable with a single import. Lua 5.1's surface is small enough that a new user can skim the API reference and be productive; a bigger language would make the sandbox audit hard to keep honest.

**Scripts are global, not per-workspace.** Users think of custom keybindings as personal ergonomics, not project-specific config. A script that pushes branches or spawns review sessions should work across every repo the user opens. If per-workspace scripts are requested later, they can be loaded *in addition to* globals — the engine's registration model already supports multiple sources.

**Allow-list sandbox, not deny-list.** Deny-lists silently widen when the underlying library gains new features. The allow-list in `sandbox.go` means a future `gopher-lua` release that adds a new standard library has no effect on us until someone changes that file.

**Load-time-only registration.** Letting scripts mutate the key map at runtime opens a pit of complexity: hot-reloading, conflict resolution mid-dispatch, state leakage between actions. Scripts register once at startup and are immutable thereafter. To change bindings, edit the file and restart.

**Built-ins always win on collision.** Users who install a third-party script should never find that `n` (new instance) has been silently rebound to something else. The warning log tells the script author what happened without breaking the user's mental model of the TUI.

**First-loaded wins on script-vs-script collision.** Alphabetical load order means collisions are deterministic — `a_script.lua` beats `b_script.lua`. Users can rename files to resolve conflicts without editing content.

**No `io`, `os`, or shell execution in the sandbox.** If a script needs to shell out, it should do it via an instance's tmux session (where the user already has agent output visible) rather than forking a subprocess the user cannot observe. This keeps the surface of "what scripts can do" bounded to "what the TUI already shows."

**Instance creation is queued, not immediate.** `ctx:new_instance{}` returns a userdata handle, but the actual `list.AddInstance` call happens on the main goroutine in `handleScriptDone`. This preserves the invariant that `h.list` is only mutated from the Bubble Tea loop, even though scripts execute in a `tea.Cmd` goroutine.
