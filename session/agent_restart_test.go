package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildRecoveryCommand_Claude(t *testing.T) {
	assert.Equal(t, "claude --continue", BuildRecoveryCommand("claude"))
}

func TestBuildRecoveryCommand_ClaudeWithFlags(t *testing.T) {
	assert.Equal(t, "claude --continue --model sonnet", BuildRecoveryCommand("claude --model sonnet"))
}

func TestBuildRecoveryCommand_ClaudeAlreadyHasContinue(t *testing.T) {
	assert.Equal(t, "claude --continue", BuildRecoveryCommand("claude --continue"))
}

func TestBuildRecoveryCommand_ClaudeAlreadyHasResume(t *testing.T) {
	assert.Equal(t, "claude --resume", BuildRecoveryCommand("claude --resume"))
}

func TestBuildRecoveryCommand_Aider(t *testing.T) {
	assert.Equal(t, "aider --model gemma", BuildRecoveryCommand("aider --model gemma"))
}

func TestBuildRecoveryCommand_Unknown(t *testing.T) {
	assert.Equal(t, "codex", BuildRecoveryCommand("codex"))
}

func TestBuildRecoveryCommand_ClaudeSubstring(t *testing.T) {
	// "claudette" should NOT match
	assert.Equal(t, "claudette", BuildRecoveryCommand("claudette"))
}
