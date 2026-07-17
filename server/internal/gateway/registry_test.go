package gateway

import (
	"fmt"
	"sync"
	"testing"
)

func mkConn(deviceID string) *Conn { return &Conn{deviceID: deviceID} }

func TestRegistry_AddReplaceReturnsOld(t *testing.T) {
	r := NewRegistry()
	a := mkConn("d1")
	b := mkConn("d1")

	if displaced := r.Add(a); displaced != nil {
		t.Fatal("first Add should not displace anything")
	}
	displaced := r.Add(b)
	if displaced != a {
		t.Fatalf("second Add should return the first conn to close, got %v", displaced)
	}
	got, ok := r.Get("d1")
	if !ok || got != b {
		t.Fatal("registry should now hold the newer connection")
	}
}

func TestRegistry_RemoveOnlyIfCurrent(t *testing.T) {
	r := NewRegistry()
	a := mkConn("d1")
	b := mkConn("d1")
	r.Add(a)
	r.Add(b) // displaces a; registry holds b

	// a's cleanup must NOT evict b.
	if r.Remove(a) {
		t.Fatal("removing a displaced connection must return false")
	}
	if _, ok := r.Get("d1"); !ok {
		t.Fatal("the current connection was wrongly removed")
	}
	// b's cleanup evicts.
	if !r.Remove(b) {
		t.Fatal("removing the current connection must return true")
	}
	if _, ok := r.Get("d1"); ok {
		t.Fatal("registry should be empty after removing the current conn")
	}
}

func TestRegistry_Count(t *testing.T) {
	r := NewRegistry()
	for i := 0; i < 100; i++ {
		r.Add(mkConn(fmt.Sprintf("d%d", i)))
	}
	if r.Count() != 100 {
		t.Fatalf("count = %d, want 100", r.Count())
	}
}

// TestRegistry_Concurrent hammers the registry from many goroutines; run with
// -race (CI does) it proves the sharded locking is data-race free.
func TestRegistry_Concurrent(t *testing.T) {
	r := NewRegistry()
	const devices, iterations = 64, 500
	var wg sync.WaitGroup

	for i := 0; i < devices; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			dev := fmt.Sprintf("d%d", id)
			for j := 0; j < iterations; j++ {
				c := mkConn(dev)
				if old := r.Add(c); old != nil {
					_ = old.deviceID
				}
				r.Get(dev)
				r.Remove(c)
			}
		}(i)
	}
	// Concurrent readers.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = r.Count()
			}
		}()
	}
	wg.Wait()
}
