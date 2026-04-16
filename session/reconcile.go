package session

import (
	"claude-squad/cmd"
	"claude-squad/session/tmux"
	"os"
	"os/exec"
)

// RecoveryAction describes what to do with an instance during startup reconciliation.
type RecoveryAction int

const (
	// ActionNoChange means the instance is already in a consistent state (e.g. Paused).
	ActionNoChange RecoveryAction = iota
	// ActionRestore means tmux + worktree are healthy; do a normal restore.
	ActionRestore
	// ActionRestart means tmux is dead but worktree exists; restart with scrollback + agent context.
	ActionRestart
	// ActionMarkPaused means both tmux and worktree are gone; mark as Paused (branch is preserved).
	ActionMarkPaused
	// ActionKillAndPause means tmux is alive but worktree is gone; kill tmux, mark Paused.
	ActionKillAndPause
	// ActionRestartWsTerminal means workspace terminal's tmux is dead; recreate it.
	ActionRestartWsTerminal
)

// CheckTmuxAlive checks if a tmux session exists by its sanitized name.
func CheckTmuxAlive(sessionTitle string, cmdExec cmd.Executor) bool {
	sanitized := tmux.ToClaudeSquadTmuxName(sessionTitle)
	existsCmd := exec.Command("tmux", "has-session", "-t="+sanitized)
	return cmdExec.Run(existsCmd) == nil
}

// CheckWorktreeExists checks if the worktree directory exists on disk.
func CheckWorktreeExists(worktreePath string) bool {
	if worktreePath == "" {
		return false
	}
	_, err := os.Stat(worktreePath)
	return err == nil
}

// DetermineRecoveryAction decides what to do with a loaded instance based on
// its persisted status and current filesystem/tmux state.
func DetermineRecoveryAction(status Status, tmuxAlive, worktreeExists, isWorkspaceTerminal bool) RecoveryAction {
	if status == Paused {
		return ActionNoChange
	}

	if isWorkspaceTerminal {
		if tmuxAlive {
			return ActionRestore
		}
		return ActionRestartWsTerminal
	}

	switch {
	case tmuxAlive && worktreeExists:
		return ActionRestore
	case tmuxAlive && !worktreeExists:
		return ActionKillAndPause
	case !tmuxAlive && worktreeExists:
		return ActionRestart
	default: // !tmuxAlive && !worktreeExists
		return ActionMarkPaused
	}
}
