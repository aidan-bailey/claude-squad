package session

import (
	"os/exec"
	"testing"

	"claude-squad/cmd/cmd_test"

	"github.com/stretchr/testify/assert"
)

func TestCheckTmuxAlive_SessionExists(t *testing.T) {
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(c *exec.Cmd) error { return nil }, // has-session succeeds
	}
	assert.True(t, CheckTmuxAlive("test-session", cmdExec))
}

func TestCheckTmuxAlive_SessionDead(t *testing.T) {
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(c *exec.Cmd) error {
			return &exec.ExitError{}
		},
	}
	assert.False(t, CheckTmuxAlive("test-session", cmdExec))
}

func TestCheckWorktreeExists_Exists(t *testing.T) {
	dir := t.TempDir()
	assert.True(t, CheckWorktreeExists(dir))
}

func TestCheckWorktreeExists_Missing(t *testing.T) {
	assert.False(t, CheckWorktreeExists("/nonexistent/path/worktree"))
}

func TestDetermineRecoveryAction(t *testing.T) {
	tests := []struct {
		name      string
		status    Status
		tmuxAlive bool
		wtExists  bool
		isWsTerm  bool
		expected  RecoveryAction
	}{
		{"paused_no_change", Paused, false, false, false, ActionNoChange},
		{"running_all_healthy", Running, true, true, false, ActionRestore},
		{"running_tmux_dead_wt_exists", Running, false, true, false, ActionRestart},
		{"running_tmux_dead_wt_gone", Running, false, false, false, ActionMarkPaused},
		{"running_tmux_alive_wt_gone", Running, true, false, false, ActionKillAndPause},
		{"ws_terminal_tmux_dead", Running, false, false, true, ActionRestartWsTerminal},
		{"ready_tmux_dead", Ready, false, true, false, ActionRestart},
		{"prompting_tmux_dead", Prompting, false, false, false, ActionMarkPaused},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := DetermineRecoveryAction(tt.status, tt.tmuxAlive, tt.wtExists, tt.isWsTerm)
			assert.Equal(t, tt.expected, action)
		})
	}
}
