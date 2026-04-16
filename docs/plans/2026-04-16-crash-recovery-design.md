# Crash Recovery Design

## Problem

When claude-squad is killed unexpectedly (OOM, power loss, SIGKILL), the app restarts in a broken state:
- Workspace terminal is unresponsive (can't see agent preview, can't interact)
- Agent session scrollback/chat history is lost (tmux scrollback is in-memory only)
- Agent conversation context is lost (agents restart fresh instead of resuming)
- The only recovery path is `claude-squad reset`, which wipes everything

Root cause: there is no startup reconciliation between persisted state (`instances.json`) and filesystem reality (tmux sessions, worktrees). `FromInstanceData` calls `Start(false)` -> `ts.Restore()` which fails if the tmux session is dead, and the error propagates to `os.Exit(1)`.

## Approach

Approach B: Startup Reconciliation + Scrollback Snapshots + Agent-Aware Restart. Solves the actual pain (broken state, lost history, lost context) with proportional complexity.

## Section 1: Startup Reconciliation

Add a `ReconcileInstance` step between loading instances from storage and adding them to the UI list in `newHome` (app.go:208-224).

### Decision matrix

| Persisted Status | Tmux Alive? | Worktree Exists? | Action |
|---|---|---|---|
| Running/Ready/Prompting | Yes | Yes | Normal restore |
| Running/Ready/Prompting | Yes | No | Kill tmux, mark Paused |
| Running/Ready/Prompting | No | Yes | Restart with scrollback + agent context |
| Running/Ready/Prompting | No | No | Mark Paused (branch preserved) |
| Paused | N/A | N/A | No change |

For workspace terminals: if tmux is dead, recreate it (same as existing runtime behavior at app.go:441-445, but at startup).

### Orphan cleanup

After reconciliation, enumerate all `cs-*` prefixed tmux sessions. Kill any not claimed by a loaded instance.

### Key code changes

- `session/instance.go`: Add `ReconcileAfterCrash()` method that checks tmux + worktree state and returns the appropriate action
- `app/app.go` `newHome()`: Replace direct `FromInstanceData` -> `Start(false)` with reconciliation loop
- `session/tmux/tmux.go`: Extract `DoesSessionExist()` for use without a full TmuxSession object (or construct a lightweight one for the check)

## Section 2: Scrollback Snapshots

Periodically capture tmux scrollback to disk so it survives crashes.

### Capture mechanism

Piggyback on the existing metadata tick (app.go:412-466). Every 30 seconds (configurable via `config.json` field `ScrollbackSnapshotInterval`):

1. For each running instance with alive tmux, call `CapturePaneContentWithOptions("-", "-")`
2. Compute content hash; skip write if unchanged
3. Write atomically via `AtomicWriteFile` to `~/.claude-squad/scrollback/<session-sanitized-name>.log`

### Restoration

On crash recovery, when starting a new tmux session to replace a dead one:

1. Check if `scrollback/<session-name>.log` exists
2. Start tmux with: `bash -c "cat <scrollback-file>; exec <program>"` to prepend history before the agent launches

### Cleanup

- Delete scrollback file in `Kill()`
- Delete all scrollback files in `claude-squad reset`

### Storage characteristics

- 10,000-line scrollback is ~500KB-1MB per session
- 30s interval with hash-based dedup minimizes I/O
- Files stored in `~/.claude-squad/scrollback/` (new directory)

## Section 3: Agent-Aware Restart

When a session restarts after a crash, append resume flags to agents that support it.

### Mechanism

Add a runtime-only `CrashRecovered` flag to `Instance`. Set during reconciliation when a dead-tmux instance gets restarted.

When `CrashRecovered` is true, modify the program command at start time:

| Program prefix | Restart command |
|---|---|
| `claude` | Append `--continue` |
| `aider` | No standard flag, launch fresh |
| Other/unknown | Launch fresh |

### Detection

Simple string prefix match on `Instance.Program`:
- `claude` -> `claude --continue`
- `claude --model sonnet` -> `claude --continue --model sonnet`
- Skip if `--continue` or `--resume` already present

### Scope

- One-shot flag: only on crash recovery, not on normal Pause/Resume
- Only works when worktree still exists (Claude Code context is keyed by project directory)
- `CrashRecovered` is not persisted; reset on next normal startup

## Section 4: Instance State Hardening

Save state at key checkpoints during lifecycle transitions to minimize inconsistency windows.

### Pause sequence (instance.go:611-667)

1. Commit changes
2. **Save `status=Paused` immediately** (via callback)
3. Detach tmux
4. Remove worktree
5. Prune refs

Key insight: once changes are committed, the instance is logically paused. A crash after step 2 leaves a stale worktree on disk, which is harmless (reconciliation cleans it up).

### Resume sequence (instance.go:670-727)

1. Setup worktree
2. Start/restore tmux
3. **Save `status=Running`** (via callback) only after both succeed

A crash after step 1 but before step 3 leaves the instance as Paused with an orphaned worktree. Reconciliation handles this on next startup.

### Coupling strategy

Pass a `SaveFunc func() error` callback to `Pause()` and `Resume()`. The caller (app.go) provides a closure that calls `Storage.UpdateInstance()`. This keeps Instance decoupled from the storage layer.

Signature changes:
```go
func (i *Instance) Pause(saveState func() error) error
func (i *Instance) Resume(saveState func() error) error
```

## Out of Scope

- Write-ahead log / journal for lifecycle operations (Approach C — overkill for crash frequency)
- Persisting agent internal state beyond what agents already persist themselves
- Multi-process locking on instances.json
- Automatic conflict resolution for dirty worktrees with uncommitted changes
