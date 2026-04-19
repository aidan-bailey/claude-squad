package git

import (
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingRunner records per-subcommand invocation counts so the cache
// tests can assert whether ls-files / add / diff actually ran.
type countingRunner struct {
	lsFiles atomic.Int32
	addN    atomic.Int32
	diff    atomic.Int32
	// lsFilesOutput is returned from ls-files; "" means no untracked files.
	lsFilesOutput string
	// diffOutput is returned from diff --shortstat.
	diffOutput string
}

func (r *countingRunner) Run(c *exec.Cmd) error { return nil }
func (r *countingRunner) Output(c *exec.Cmd) ([]byte, error) {
	return r.CombinedOutput(c)
}
func (r *countingRunner) CombinedOutput(c *exec.Cmd) ([]byte, error) {
	joined := strings.Join(c.Args, " ")
	switch {
	case strings.Contains(joined, "ls-files"):
		r.lsFiles.Add(1)
		return []byte(r.lsFilesOutput), nil
	case strings.Contains(joined, "add -N"):
		r.addN.Add(1)
		return nil, nil
	case strings.Contains(joined, "diff"):
		r.diff.Add(1)
		return []byte(r.diffOutput), nil
	}
	return nil, nil
}

// TestEnsureUntrackedStaged_Cache verifies that back-to-back diff calls
// skip the ls-files subprocess when the prior probe found no untracked
// files, and that the cache expires after untrackedCacheTTL.
func TestEnsureUntrackedStaged_Cache(t *testing.T) {
	runner := &countingRunner{lsFilesOutput: "", diffOutput: " 1 file changed, 2 insertions(+)\n"}
	g := NewGitWorktreeFromStorageWithRunner("/repo", "/worktree", "session", "branch", "abc123", false, "", runner)

	// First call: ls-files runs, no add -N (nothing untracked), diff runs.
	stats := g.DiffShortStat()
	require.NoError(t, stats.Error)
	assert.EqualValues(t, 1, runner.lsFiles.Load(), "first DiffShortStat runs ls-files")
	assert.EqualValues(t, 0, runner.addN.Load(), "no untracked files means no add -N")
	assert.EqualValues(t, 1, runner.diff.Load())

	// Second call inside TTL: ls-files must be cached.
	_ = g.DiffShortStat()
	assert.EqualValues(t, 1, runner.lsFiles.Load(), "ls-files must be skipped inside TTL")
	assert.EqualValues(t, 2, runner.diff.Load())

	// Expire the cache by rewinding the stored timestamp.
	g.untrackedCacheMu.Lock()
	g.untrackedCheckedAt = time.Now().Add(-2 * untrackedCacheTTL)
	g.untrackedCacheMu.Unlock()

	_ = g.DiffShortStat()
	assert.EqualValues(t, 2, runner.lsFiles.Load(), "ls-files runs again after TTL expiry")
}

// TestEnsureUntrackedStaged_DoesNotCacheHit ensures the cache only
// short-circuits when the prior probe was negative — if untracked files
// were seen, we must re-run ls-files (and potentially add -N) on every
// subsequent call to keep the index in sync with the agent's output.
func TestEnsureUntrackedStaged_DoesNotCacheHit(t *testing.T) {
	runner := &countingRunner{lsFilesOutput: "new_file.txt\n", diffOutput: ""}
	g := NewGitWorktreeFromStorageWithRunner("/repo", "/worktree", "session", "branch", "abc123", false, "", runner)

	_ = g.DiffShortStat()
	_ = g.DiffShortStat()
	assert.EqualValues(t, 2, runner.lsFiles.Load(), "positive ls-files result must not be cached")
	assert.EqualValues(t, 2, runner.addN.Load(), "add -N must run each time untracked files are present")
}

func TestParseShortStat(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		added   int
		removed int
	}{
		{"both", " 3 files changed, 10 insertions(+), 5 deletions(-)\n", 10, 5},
		{"insertions only", " 1 file changed, 3 insertions(+)\n", 3, 0},
		{"deletions only", " 1 file changed, 2 deletions(-)\n", 0, 2},
		{"empty", "", 0, 0},
		{"whitespace", "  \n", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			added, removed := parseShortStat(tt.input)
			assert.Equal(t, tt.added, added)
			assert.Equal(t, tt.removed, removed)
		})
	}
}
