package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// These target pure formatters that back the pane-title scroll
// indicator. Integration with the live viewport is exercised by the
// manual smoke test documented in the plan; here we lock in the
// "no indicator at bottom" invariant and the format string so drift
// in either surfaces immediately.

func TestScrollSuffix(t *testing.T) {
	cases := []struct {
		name    string
		percent float64
		want    string
	}{
		{"at bottom returns empty", 1.0, ""},
		{"above one clamps to empty", 1.5, ""},
		{"zero renders 0 percent up-arrow", 0.0, " (0% ↑)"},
		{"midway rounds down", 0.427, " (42% ↑)"},
		{"negative clamps to zero", -0.2, " (0% ↑)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, scrollSuffix(tc.percent))
		})
	}
}

func TestDiffTitle(t *testing.T) {
	// At bottom, only the close hint is shown.
	assert.Equal(t, " Diff (d/Esc to close) ", diffTitle(1.0))
	// Scrolled: scroll percentage slots in before the close hint.
	assert.Equal(t, " Diff (42% ↑ · d/Esc to close) ", diffTitle(0.42))
	// Negative clamps like scrollSuffix.
	assert.Equal(t, " Diff (0% ↑ · d/Esc to close) ", diffTitle(-0.1))
}
