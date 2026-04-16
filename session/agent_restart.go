package session

import "strings"

// BuildRecoveryCommand modifies a program command string for crash recovery.
// For supported agents (claude), it appends resume flags (--continue).
// Unsupported agents are returned unchanged.
func BuildRecoveryCommand(program string) string {
	parts := strings.Fields(program)
	if len(parts) == 0 {
		return program
	}

	base := parts[0]

	// Only modify claude commands
	if base != "claude" {
		return program
	}

	// Don't add if already has --continue or --resume
	for _, p := range parts[1:] {
		if p == "--continue" || p == "--resume" {
			return program
		}
	}

	// Insert --continue after "claude", preserving remaining flags
	return parts[0] + " --continue" + strings.TrimPrefix(program, parts[0])
}
