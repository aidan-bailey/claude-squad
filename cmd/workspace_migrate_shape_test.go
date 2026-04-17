package cmd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMigrationInstance_MirrorsInstanceData_JSON serves as a drift guard:
// it verifies a hand-crafted JSON document that matches session.InstanceData's
// shape round-trips through migrationInstance without losing fields.
// If session adds a field, update both the struct and this fixture.
func TestMigrationInstance_MirrorsInstanceData_JSON(t *testing.T) {
	src := `{
		"schema_version": 1,
		"title": "t",
		"path": "/p",
		"branch": "b",
		"status": 2,
		"height": 10,
		"width": 20,
		"created_at": "2026-01-01T00:00:00Z",
		"updated_at": "2026-01-02T00:00:00Z",
		"auto_yes": true,
		"program": "claude",
		"worktree": {
			"repo_path": "/r",
			"worktree_path": "/wt",
			"session_name": "t",
			"branch_name": "b",
			"base_commit_sha": "abc",
			"is_existing_branch": false
		},
		"diff_stats": {
			"added": 1,
			"removed": 2,
			"content": "x"
		},
		"is_workspace_terminal": false
	}`

	var mi migrationInstance
	assert.NoError(t, json.Unmarshal([]byte(src), &mi))

	out, err := json.Marshal(mi)
	assert.NoError(t, err)

	// Re-decode both sides into a map and compare — tolerates whitespace
	// and key-order differences between the fixture and our output.
	var srcMap, outMap map[string]any
	assert.NoError(t, json.Unmarshal([]byte(src), &srcMap))
	assert.NoError(t, json.Unmarshal(out, &outMap))
	assert.Equal(t, srcMap, outMap)
}
