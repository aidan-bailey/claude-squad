package session

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Compile-time assertions keep the two backend types honest. A method
// signature drift surfaces here before app code feels it.
var (
	_ SessionBackend = (*worktreeBackend)(nil)
	_ SessionBackend = (*workspaceTerminalBackend)(nil)
)

func TestInstanceBackendRoutingHonorsIsWorkspaceTerminal(t *testing.T) {
	normal := &Instance{IsWorkspaceTerminal: false}
	wt := &Instance{IsWorkspaceTerminal: true}

	if _, ok := normal.backend().(*worktreeBackend); !ok {
		t.Errorf("expected worktreeBackend for normal instance, got %T", normal.backend())
	}
	if _, ok := wt.backend().(*workspaceTerminalBackend); !ok {
		t.Errorf("expected workspaceTerminalBackend for IsWorkspaceTerminal=true, got %T", wt.backend())
	}
}

func TestWorkspaceTerminalBackendRepoNameIsPathBase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "my-cool-repo")
	inst := &Instance{Path: path, IsWorkspaceTerminal: true}

	name, err := inst.backend().RepoName()
	assert.NoError(t, err)
	assert.Equal(t, "my-cool-repo", name)
}

func TestWorkspaceTerminalBackendWorkTreePathIsRootPath(t *testing.T) {
	inst := &Instance{Path: "/tmp/repo", IsWorkspaceTerminal: true}
	assert.Equal(t, "/tmp/repo", inst.backend().WorkTreePath())
}

func TestWorktreeBackendRepoNameRequiresStarted(t *testing.T) {
	inst := &Instance{IsWorkspaceTerminal: false}
	_, err := inst.backend().RepoName()
	assert.Error(t, err, "worktree backend should refuse when the instance hasn't started — worktree isn't populated yet")
}

func TestWorktreeBackendWorkTreePathReturnsEmptyWhenNoWorktree(t *testing.T) {
	inst := &Instance{IsWorkspaceTerminal: false}
	assert.Equal(t, "", inst.backend().WorkTreePath(),
		"pre-start worktree instances have no worktree path yet")
}

func TestWorktreeBackendRefreshBranchReturnsEmpty(t *testing.T) {
	// Worktrees carry a fixed branch — no refresh needed.
	inst := &Instance{IsWorkspaceTerminal: false}
	assert.Equal(t, "", inst.backend().RefreshBranch())
}
