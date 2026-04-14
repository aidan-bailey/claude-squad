# Interactive Preview (Inline Attach) Design

## Summary

Replace the current enter-to-fullscreen behavior with an inline attach mode. Keystrokes are forwarded to the tmux session while the Bubble Tea UI remains visible. Full-screen attach moves to `O` (shift+o).

## Keybindings

| Key | Action |
|-----|--------|
| `enter`/`o` | Inline attach (new) |
| `O` | Full-screen attach (existing behavior) |
| `Ctrl+Q` | Exit inline attach |

## State Model

New constant `stateInlineAttach` in app.go. When active:

- All `tea.KeyMsg` events are intercepted at the top of `Update()`, before any other key handling
- `Ctrl+Q` exits the mode, transitions back to `stateDefault`
- Everything else is converted to bytes and written to the selected instance's PTY via `tmuxSession.SendKeys()`
- Non-key messages (ticks, window resize, etc.) are processed normally so the UI keeps updating
- Left panel remains visible but frozen (no list navigation)

## Key-to-Bytes Conversion

New function `keyMsgToBytes(msg tea.KeyMsg) []byte` to reconstruct raw terminal bytes from Bubble Tea's parsed key events. Needed because Bubble Tea destroys raw stdin bytes during parsing.

| Input | Output bytes |
|-------|-------------|
| Regular rune (e.g., `a`, `5`, `/`) | UTF-8 encoding of the rune |
| Enter | `\r` (0x0D) |
| Backspace | `\x7f` |
| Tab | `\t` (0x09) |
| Escape | `\x1b` |
| Space | `\x20` |
| Up/Down/Left/Right | `\x1b[A` / `\x1b[B` / `\x1b[D` / `\x1b[C` |
| Home/End | `\x1b[H` / `\x1b[F` |
| Page Up/Down | `\x1b[5~` / `\x1b[6~` |
| Delete | `\x1b[3~` |
| Ctrl+A through Ctrl+Z | `0x01` through `0x1A` (except Ctrl+Q intercepted) |
| Alt+key | `\x1b` + key bytes |
| F1-F12 | Standard xterm sequences (`\x1bOP` through `\x1b[24~`) |
| Unknown/unmappable | `nil` (silently dropped) |

## Tick Frequency

Preview tick drops from 100ms to 50ms while in `stateInlineAttach` for more responsive visual feedback. Restores to 100ms on exit.

## Visual Indicator

- Status/help text at the bottom of the preview pane shows `Ctrl+Q to detach | O for fullscreen`
- Preview pane border gets a distinct highlight style to signal it's receiving input

## Edge Cases

- **Instance dies while inline-attached**: Detected on next tick via existing fallback logic. Exits `stateInlineAttach` back to `stateDefault`.
- **Window resize**: Already handled. `SetDetachedSize()` is called on resize, capture-pane respects pane dimensions.
- **Tab switching while inline-attached**: Disabled. Tab key is forwarded to tmux. User must Ctrl+Q first.
- **Paused/Loading instances**: Same guards as current enter handler. Don't enter inline-attach if instance isn't running.

## Approach

Approach A: KeyMsg forwarding + capture-pane polling. Simplest path that covers 90%+ of real usage. Full-screen attach via `O` remains available for cases needing perfect terminal fidelity.

Alternatives considered:
- **pipe-pane streaming**: Near-instant output but significantly more complex plumbing.
- **Embedded virtual terminal emulator**: Perfect fidelity but massive scope. Overkill when capture-pane already works.
