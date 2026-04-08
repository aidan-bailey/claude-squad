# Workspaces

Workspaces let users manage multiple git repositories as separate environments within Claude Squad. Each workspace gets its own isolated set of instances, worktrees, config, and state — all stored in a `.claude-squad/` directory inside the repo itself.

## Concepts

### Workspace

A registered git repository. Stored as a name + absolute path in the global registry.

```go
// config/workspace.go
type Workspace struct {
    Name    string    `json:"name"`
    Path    string    `json:"path"`
    AddedAt time.Time `json:"added_at"`
}
```

### Workspace Registry

The global index of all registered workspaces. Always stored at `~/.claude-squad/workspaces.json`, regardless of `CLAUDE_SQUAD_HOME`. Tracks which workspace was last used.

```go
// config/workspace.go
type WorkspaceRegistry struct {
    Workspaces []Workspace `json:"workspaces"`
    LastUsed   string      `json:"last_used"`
}
```

Example file:

```json
{
  "workspaces": [
    {
      "name": "myproject",
      "path": "/home/alice/repos/myproject",
      "added_at": "2025-06-15T10:30:00Z"
    }
  ],
  "last_used": "myproject"
}
```

## Directory Layout

### Global (no workspaces, or "Global" mode)

All state lives in `~/.claude-squad/`:

```
~/.claude-squad/
├── config.json            # User configuration
├── state.json             # App state (instances, help-seen flags)
├── workspaces.json        # Workspace registry (always here)
└── worktrees/             # Git worktrees for sessions
```

### Per-Workspace

When a workspace is active, state lives in `{repo}/.claude-squad/`:

```
/home/alice/repos/myproject/
├── .claude-squad/         # Workspace-local data (gitignored)
│   ├── config.json        # Workspace-specific config
│   ├── state.json         # Workspace-specific state & instances
│   └── worktrees/         # Worktrees for this workspace's sessions
├── .gitignore             # Contains ".claude-squad/" entry
└── ... (repo files)
```

The `.claude-squad/` directory is automatically added to the repo's `.gitignore` on registration (`EnsureGitignore()`).

## Isolation Mechanism

Workspaces achieve isolation through explicit `WorkspaceContext` propagation.

1. On startup, `ResolveWorkspace(cwd, registry)` returns a `WorkspaceContext` with the matching workspace's `ConfigDir`.
2. The `WorkspaceContext` is threaded through `app.Run` → `newHome` → all downstream functions (storage, worktree creation, daemon).
3. All state reads/writes use the context's `ConfigDir` directly via `LoadConfigFrom(dir)` / `LoadStateFrom(dir)`.
4. `GetConfigDir()` still reads `CLAUDE_SQUAD_HOME` as a backward-compatible fallback for external tooling, but internal code passes config directories explicitly.

This means there is no explicit instance filtering — each workspace simply loads from its own state file. Switching workspaces swaps the active `WorkspaceContext`.

The workspace registry (`workspaces.json`) is the one exception: it always reads from `~/.claude-squad/` via `GetGlobalConfigDir()`, since it needs to be accessible regardless of which workspace is active.

## CLI Commands

All under `claude-squad workspace`:

| Command | Description |
|---------|-------------|
| `workspace add [path]` | Register a git repo as a workspace. Defaults to `.`. Flag `--name` overrides the auto-derived name (directory basename). |
| `workspace list` | List registered workspaces with name, path, and status (`[last used]` or `[missing]`). |
| `workspace remove <name>` | Unregister a workspace by name. Does not delete the `.claude-squad/` directory. |
| `workspace use <name>` | Set the default workspace (`LastUsed`) for future invocations. |
| `workspace rename <old> <new>` | Rename a workspace in the registry. |
| `workspace status [name]` | Show instance counts for a workspace (defaults to cwd-matched workspace). |
| `workspace migrate` | Move global instances to their matching workspaces (see [Migration](#migration)). |

The root command also accepts `--workspace <name>` (`-w`) to select a workspace by name, bypassing cwd auto-detection.

Source: `cmd/workspace.go`, `main.go`.

### `workspace add` Details

1. Resolves path to absolute.
2. Validates it's a git repo (checks for `.git`).
3. Ensures name and path are both unique in the registry.
4. Calls `EnsureGitignore()` to add `.claude-squad/` to the repo's `.gitignore`.
5. Saves to `~/.claude-squad/workspaces.json`.

### `workspace remove` Details

1. Finds workspace by name.
2. Removes from registry slice.
3. Clears `LastUsed` if this was the last-used workspace.
4. Saves registry. Does **not** delete on-disk data.

## Startup Behavior

Source: `main.go`, `config/workspace.go` (`ResolveWorkspace`).

```
┌─────────────────────────────────┐
│ Load workspace registry         │
└──────────┬──────────────────────┘
           │
     ┌─────▼─────────────────┐
     │ --workspace flag set? │──── yes ──► Look up by name
     └─────┬─────────────────┘             → WorkspaceContext
           │ no
     ┌─────▼─────┐
     │ Any       │──── no ──► Require cwd is a git repo
     │ workspaces│            (original behavior)
     │ registered?│
     └─────┬─────┘
           │ yes
     ┌─────▼──────────────────┐
     │ Does cwd match a       │──── yes ──► Auto-select that workspace
     │ registered workspace?  │             → WorkspaceContext
     └─────┬──────────────────┘
           │ no
     ┌─────▼──────────────────────┐
     │ Show TUI workspace picker  │
     │ (includes "Global" option) │
     │ inside Bubble Tea          │
     └─────┬──────────────────────┘
           │
     ┌─────▼──────────────────┐
     │ Update LastUsed        │
     │ Load config & continue │
     └────────────────────────┘
```

Path matching uses `FindByPath()`, which matches exact paths or parent directories (with separator check to avoid `/repo` matching `/repo-fork`).

## In-App Workspace Switching

Users press `W` (shift+w) to open the workspace picker overlay.

Source: `app/app.go`, `ui/overlay/workspacePicker.go`.

### Picker UI

- Lists all registered workspaces with names and paths.
- Marks the current workspace with `*`.
- Includes a "Global (default)" option at the bottom.
- Navigation: `j`/`k` or arrow keys. `Enter` to select, `Esc` to cancel.

### Switch Sequence

When a workspace is selected:

1. **Save current state** — persists instances to the current workspace's state file.
2. **Stop daemon** — shuts down the auto-yes daemon for the current workspace.
3. **Swap `CLAUDE_SQUAD_HOME`** — set to the new workspace's config dir (or unset for Global).
4. **Update `LastUsed`** — in the global registry.
5. **Full reload** — reloads config, state, and instances from the new workspace. Reinitializes all UI components.

After reload, the app displays only the new workspace's instances. The workspace name appears in the list header.

## Migration

`workspace migrate` moves instances from the global `~/.claude-squad/state.json` to workspace-specific state files.

Source: `cmd/workspace.go`.

### Process

1. Load all instances from the global state file.
2. For each instance, match its `worktree.repo_path` to a registered workspace via `FindByPath()`.
3. Group matched instances by workspace.
4. For each workspace:
   - Load existing workspace state (if any).
   - Skip instances that already exist (by title) to avoid duplicates.
   - Update worktree paths: `~/.claude-squad/worktrees/{name}` becomes `{workspace_path}/.claude-squad/worktrees/{name}`.
   - Move the worktree directories on the filesystem.
   - Merge and save to workspace state.
5. Update global state to contain only unmatched (orphan) instances.
6. Print a summary of what was migrated.

### Path Rewriting

The migration rewrites the `worktree_path` field on each instance:

```
Before: ~/.claude-squad/worktrees/alice/my_feature_abc123
After:  /home/alice/repos/myproject/.claude-squad/worktrees/alice/my_feature_abc123
```

The actual directories are moved on disk via `os.Rename()`.

## Key Source Files

| File | Role |
|------|------|
| `config/workspace.go` | `Workspace`, `WorkspaceRegistry`, CRUD operations, `EnsureGitignore` |
| `cmd/workspace.go` | CLI commands: `add`, `list`, `remove`, `migrate` |
| `ui/overlay/workspacePicker.go` | Workspace picker overlay (Bubble Tea component) |
| `app/app.go` | Workspace detection on init, switch logic, reload |
| `config/config.go` | `GetConfigDir()` — respects `CLAUDE_SQUAD_HOME` |
| `config/state.go` | State loading from config directory |
| `session/git/worktree.go` | `getWorktreeDirectory()` — uses config directory |
| `main.go` | Startup workspace detection and prompt |
| `keys/keys.go` | `KeyWorkspace` binding (`W`) |

## Design Decisions

**Isolation via explicit context, not filtering.** Rather than loading all instances globally and filtering by workspace, each workspace has its own state file. A `WorkspaceContext` value object carries the config directory and is threaded through all function calls. `CLAUDE_SQUAD_HOME` is retained as a backward-compatible fallback for external tooling.

**Registry always global.** The workspace registry must be accessible before any workspace is selected, so it lives at `~/.claude-squad/workspaces.json` regardless of `CLAUDE_SQUAD_HOME`.

**`.claude-squad/` is gitignored.** Workspace data (worktrees, state, config) lives inside the repo but is excluded from version control via an automatic `.gitignore` entry.

**No auto-migration.** Migration from global to workspace-scoped instances is a manual `workspace migrate` command. This avoids surprising users who haven't opted into workspaces yet.

**`workspace remove` is non-destructive.** Removing a workspace from the registry does not delete its `.claude-squad/` directory or any sessions. The data remains on disk.
