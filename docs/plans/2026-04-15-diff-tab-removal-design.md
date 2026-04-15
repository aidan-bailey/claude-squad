# Diff Tab Removal — Split Pane Design

## Summary

Replace the tabbed right panel (Preview/Diff/Terminal) with a split pane showing both Agent (formerly Preview) and Terminal simultaneously. Diff becomes a hotkey-triggered overlay. Left panel width reduced from 30% to 20%.

## Layout

### Default View

```
┌─────────────────────────────────────────────────────┐
│ Workspace Tab Bar                                   │
├────────┬────────────────────────────────────────────┤
│        │  Agent (70% height)                        │
│  List  │                                            │
│  (20%) │                                            │
│        ├╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌┤
│        │  Terminal (30% height)                     │
├────────┴────────────────────────────────────────────┤
│ Menu Bar                                            │
└─────────────────────────────────────────────────────┘
```

### Diff Overlay (d pressed)

```
┌─────────────────────────────────────────────────────┐
│ Workspace Tab Bar                                   │
├────────┬────────────────────────────────────────────┤
│        │ ┌─── Diff ──────────────────────────────┐  │
│  List  │ │  +3 additions, -1 deletions           │  │
│  (20%) │ │  --- a/foo.go                         │  │
│        │ │  +++ b/foo.go                         │  │
│        │ │  ...                                  │  │
│        │ └───────────────────────── d/Esc close ─┘  │
├────────┴────────────────────────────────────────────┤
│ Menu Bar                                            │
└─────────────────────────────────────────────────────┘
```

## Component: SplitPane

Replaces `TabbedWindow`. Lives in `ui/split_pane.go`.

### Fields

- `agent *PreviewPane` — top pane (70% height)
- `terminal *TerminalPane` — bottom pane (30% height)
- `diff *DiffPane` — overlay, rendered on demand
- `focusedPane int` — AgentPane or TerminalPane constant
- `diffVisible bool` — overlay toggle
- `height, width int`
- `instance *session.Instance`

### Methods

- `SetSize(w, h)` — distributes height 70/30 between agent and terminal
- `String()` — renders both panes stacked with separator; if `diffVisible`, renders diff overlay instead
- `ToggleFocus()` — swaps `focusedPane` (called on `tab`)
- `ToggleDiff()` — flips `diffVisible` (called on `d`)
- `ScrollUp()/ScrollDown()` — delegates to focused pane, or diff if overlay open
- `UpdateAgent(instance)` / `UpdateTerminal(instance)` — always update both (no lazy skip)
- `UpdateDiff(instance)` — only updates when `diffVisible`

### Focus Indicator

The separator between agent and terminal uses `highlightColor` on the focused pane's edge.

## Keybindings

| Key | New Behavior |
|-----|-------------|
| `tab` | Swap focus between Agent and Terminal |
| `d` | Toggle diff overlay |
| `shift+up/down` | Scroll focused pane (or diff if overlay open) |
| `Esc` | Dismiss diff overlay, then exit scroll mode, then other Esc behaviors |

`d` only fires in default view state (not during text input, inline attach, or other overlays).

## Files Changed

| File | Change |
|------|--------|
| `ui/split_pane.go` | New. SplitPane struct, rendering, focus/overlay logic. |
| `ui/tabbed_window.go` | Deleted. |
| `app/app.go` | Replace `tabbedWindow` with `splitPane`. Update View(), key handlers, SetSize() (20/80), instanceChanged(). Add `d` binding. Update `tab` handler. |
| `ui/menu.go` | Update hints for new layout. Replace `SetActiveTab()` with focus-aware method. |
| `ui/preview.go` | Rename display strings from "Preview" to "Agent". |
| `keys/keys.go` | Add `KeyDiff` binding for `d`. |

`ui/diff.go` and `ui/terminal.go` unchanged — they receive dimensions and render independently.
