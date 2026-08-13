package adapters

import (
	"context"
	"sync"
	"testing"
)

// Protocol scenario P11 (concurrency slice): ten devices race for the floor at
// once against the REAL Valkey Lua. The single-slot atomic script guarantees
// exactly one grant and nine queued — no torn reads, no double-grant — which is
// the property the in-process test can't prove.
func TestIntegration_ScenarioP11_ConcurrentAcquireExactlyOneWinner(t *testing.T) {
	s, room := testFloorStore(t)
	ctx := context.Background()

	const n = 10
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		granted int
		queued  int
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(device string) {
			defer wg.Done()
			r, err := s.Acquire(ctx, room, device)
			if err != nil {
				t.Errorf("acquire %s: %v", device, err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			switch {
			case r.Granted != nil:
				granted++
			case r.Position > 0:
				queued++
			}
		}("u" + string(rune('a'+i)) + ":d1")
	}
	wg.Wait()

	if granted != 1 {
		t.Fatalf("granted = %d, want exactly 1 (atomic floor)", granted)
	}
	if queued != n-1 {
		t.Fatalf("queued = %d, want %d", queued, n-1)
	}
}
