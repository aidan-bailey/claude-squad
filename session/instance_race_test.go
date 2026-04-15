package session

import (
	"sync"
	"testing"
)

// TestInstance_ConcurrentStatusReadWrite exercises the race the audit
// identified between tick-worker goroutines and main-loop status writers.
// Must pass under `go test -race`.
func TestInstance_ConcurrentStatusReadWrite(t *testing.T) {
	inst := &Instance{Title: "race", Status: Ready}

	var wg sync.WaitGroup
	const n = 1000

	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			inst.SetStatus(Running)
			inst.SetStatus(Paused)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			_ = inst.GetStatus()
		}
	}()
	wg.Wait()
}
