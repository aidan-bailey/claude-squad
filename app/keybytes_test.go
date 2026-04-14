package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestKeyMsgToBytes_Runes(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	assert.Equal(t, []byte("a"), keyMsgToBytes(msg))

	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}}
	assert.Equal(t, []byte("5"), keyMsgToBytes(msg))

	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}
	assert.Equal(t, []byte("/"), keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_Enter(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	assert.Equal(t, []byte{0x0D}, keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_Backspace(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyBackspace}
	assert.Equal(t, []byte{0x7F}, keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_Tab(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyTab}
	assert.Equal(t, []byte{0x09}, keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_Escape(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyEscape}
	assert.Equal(t, []byte{0x1B}, keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_Space(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	assert.Equal(t, []byte{0x20}, keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_ArrowKeys(t *testing.T) {
	tests := []struct {
		name     string
		keyType  tea.KeyType
		expected []byte
	}{
		{"Up", tea.KeyUp, []byte("\x1b[A")},
		{"Down", tea.KeyDown, []byte("\x1b[B")},
		{"Left", tea.KeyLeft, []byte("\x1b[D")},
		{"Right", tea.KeyRight, []byte("\x1b[C")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tea.KeyMsg{Type: tt.keyType}
			assert.Equal(t, tt.expected, keyMsgToBytes(msg))
		})
	}
}

func TestKeyMsgToBytes_CtrlKeys(t *testing.T) {
	// Ctrl+A = 0x01
	msg := tea.KeyMsg{Type: tea.KeyCtrlA}
	assert.Equal(t, []byte{0x01}, keyMsgToBytes(msg))

	// Ctrl+C = 0x03
	msg = tea.KeyMsg{Type: tea.KeyCtrlC}
	assert.Equal(t, []byte{0x03}, keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_HomeEnd(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyHome}
	assert.Equal(t, []byte("\x1b[H"), keyMsgToBytes(msg))

	msg = tea.KeyMsg{Type: tea.KeyEnd}
	assert.Equal(t, []byte("\x1b[F"), keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_Delete(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyDelete}
	assert.Equal(t, []byte("\x1b[3~"), keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_PageUpDown(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyPgUp}
	assert.Equal(t, []byte("\x1b[5~"), keyMsgToBytes(msg))

	msg = tea.KeyMsg{Type: tea.KeyPgDown}
	assert.Equal(t, []byte("\x1b[6~"), keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_FunctionKeys(t *testing.T) {
	tests := []struct {
		name     string
		keyType  tea.KeyType
		expected []byte
	}{
		{"F1", tea.KeyF1, []byte("\x1bOP")},
		{"F2", tea.KeyF2, []byte("\x1bOQ")},
		{"F5", tea.KeyF5, []byte("\x1b[15~")},
		{"F12", tea.KeyF12, []byte("\x1b[24~")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tea.KeyMsg{Type: tt.keyType}
			assert.Equal(t, tt.expected, keyMsgToBytes(msg))
		})
	}
}

func TestKeyMsgToBytes_AltKey(t *testing.T) {
	// Alt+a should produce ESC followed by 'a'
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}, Alt: true}
	assert.Equal(t, []byte{0x1B, 'a'}, keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_MultiByteRune(t *testing.T) {
	// "日" is a 3-byte UTF-8 character (U+65E5)
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'日'}}
	assert.Equal(t, []byte("日"), keyMsgToBytes(msg))
}

func TestKeyMsgToBytes_Unknown(t *testing.T) {
	// KeyInsert is not in our mapping table, should return nil
	msg := tea.KeyMsg{Type: tea.KeyInsert}
	assert.Nil(t, keyMsgToBytes(msg))
}
