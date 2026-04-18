package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoScriptsFlagSkipsUserDir proves --no-scripts still loads the
// embedded defaults (so the TUI is usable) but ignores the user
// scripts directory. A user with a broken script can recover by
// relaunching with --no-scripts.
func TestNoScriptsFlagSkipsUserDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "user.lua"),
		[]byte(`cs.bind("X", function() end)`), 0o644))

	h := &home{}
	initScriptsIn(h, dir, true)
	require.NotNil(t, h.scripts)
	// Defaults are on — "q" is a stock binding from defaults.lua.
	assert.True(t, h.scripts.HasAction("q"))
	// User script was skipped.
	assert.False(t, h.scripts.HasAction("X"))
}

// TestUserParseErrorKeepsDefaults proves a broken user-supplied
// script does not take down the entire keymap. The engine logs a
// warning (loadScripts's per-file error policy) and the stock
// defaults remain bound.
func TestUserParseErrorKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.lua"),
		[]byte(`this is not valid lua`), 0o644))

	h := &home{}
	initScriptsIn(h, dir, false)
	require.NotNil(t, h.scripts)
	assert.True(t, h.scripts.HasAction("q"))
}
