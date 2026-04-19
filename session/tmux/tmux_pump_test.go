package tmux

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestOutputPump_UnblocksViaSetReadDeadline verifies that a blocked
// pump goroutine exits promptly when signalPumpStop fires — the key
// guarantee Phase 2.1 is adding. Prior to this change, the pump's only
// exit was a ptmx.Close causing Read to return an error; on platforms
// where Close does not interrupt a blocked Read the goroutine would
// sit until the pumpWaitTimeout watchdog gave up, then leak.
//
// os.Pipe is used as a stand-in for the PTY because (a) Read on a pipe
// with no pending bytes and an open writer blocks indefinitely — exactly
// the pathological case — and (b) pipes implement SetReadDeadline, so
// this exercises the same interrupt path the production PTY uses on
// Linux.
func TestOutputPump_UnblocksViaSetReadDeadline(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })
	t.Cleanup(func() { _ = r.Close() })

	ts := NewTmuxSession("pump-cancel", "prog")
	ts.startOutputPump(r)

	// Give the goroutine a tick to reach its blocked Read.
	time.Sleep(10 * time.Millisecond)

	start := time.Now()
	ts.signalPumpStop(r)
	ts.waitPumpExit()
	elapsed := time.Since(start)

	// pumpWaitTimeout is 2s; if SetReadDeadline worked, exit is near-instant.
	// 500ms is a generous bound that still catches the "fell through to the
	// watchdog" regression (which would take ~2s).
	require.Less(t, elapsed, 500*time.Millisecond,
		"pump must exit promptly after signalPumpStop; elapsed=%s", elapsed)
}

// TestSignalPumpStop_NilPtmxIsSafe guards the Close-after-Close path:
// if callers ever invoke signalPumpStop twice, the second call has
// ptmx=nil and must not panic. The pumpCancel field is also cleared so
// a later Restore starts with a fresh state.
func TestSignalPumpStop_NilPtmxIsSafe(t *testing.T) {
	ts := NewTmuxSession("nil-ptmx", "prog")
	require.NotPanics(t, func() { ts.signalPumpStop(nil) })
	require.Nil(t, ts.pumpCancel)
}
