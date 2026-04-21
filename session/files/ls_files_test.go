package files

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initGitRepo stages a tempdir as a git repo with a committed base so
// `git ls-files --cached` has content to return. Identity config is
// local-only to avoid leaking into the caller's git config.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = dir
		require.NoError(t, c.Run(), "git %v", args)
	}
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	require.NoError(t, c.Run(), "git %v", args)
}

func TestListInGitRepoIncludesTrackedAndUntracked(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeFile(t, dir, "tracked.go", "package main")
	writeFile(t, dir, "pkg/inner.go", "package pkg")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "init")

	// .gitignore + an ignored file + an untracked-but-not-ignored file
	writeFile(t, dir, ".gitignore", "ignored.log\n")
	writeFile(t, dir, "ignored.log", "noise")
	writeFile(t, dir, "new_file.go", "package new")
	gitIn(t, dir, "add", ".gitignore")
	gitIn(t, dir, "commit", "-m", "add ignore")

	result, err := List(dir)
	require.NoError(t, err)
	assert.True(t, result.FromGit, "git ls-files should have succeeded")
	assert.Contains(t, result.Paths, "tracked.go")
	assert.Contains(t, result.Paths, "pkg/inner.go")
	assert.Contains(t, result.Paths, "new_file.go", "untracked files should still appear")
	assert.Contains(t, result.Paths, ".gitignore")
	assert.NotContains(t, result.Paths, "ignored.log", "gitignored files should be filtered out")
}

func TestListInNonGitDirFallsBackToWalk(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "a")
	writeFile(t, dir, "nested/b.txt", "b")

	result, err := List(dir)
	require.NoError(t, err)
	assert.False(t, result.FromGit, "non-git root should report FromGit=false")
	assert.Contains(t, result.Paths, "a.txt")
	assert.Contains(t, result.Paths, "nested/b.txt")
}

func TestListSkipsGitDirOnWalkFallback(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "visible.txt", "x")
	writeFile(t, dir, ".git/HEAD", "ref: refs/heads/main")
	writeFile(t, dir, ".git/config", "[core]")

	result, err := List(dir)
	require.NoError(t, err)
	// Since there's a .git directory, IsGitRepo may or may not return
	// true depending on git's heuristics — what we verify is that no
	// .git-internal paths leak into the listing regardless of which
	// branch fires.
	for _, p := range result.Paths {
		assert.NotContains(t, p, ".git/", "path %q should not be under .git", p)
	}
	assert.Contains(t, result.Paths, "visible.txt")
}

func TestListEmptyRoot(t *testing.T) {
	_, err := List("")
	assert.Error(t, err)
}

func TestListNonexistentRoot(t *testing.T) {
	_, err := List(filepath.Join(t.TempDir(), "does-not-exist"))
	assert.Error(t, err)
}
