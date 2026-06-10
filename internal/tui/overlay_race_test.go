package tui

import (
	"sync"
	"testing"
)

// TestOverlayConcurrentAccessRaceFree exercises the data race the upgrade task
// hits: a task goroutine mutates the overlay's fields (Lines/Help/StatusText)
// between blocking calls while the render goroutine reads them via View/Status
// every frame. Run with -race; before the fix (direct field writes + unlocked
// reads) the detector fires. The mutations now go through edit() and the reads
// snapshot under RLock.
func TestOverlayConcurrentAccessRaceFree(t *testing.T) {
	o := &Overlay{Title: "Upgrade", Lines: []string{"start"}, Help: "h", StatusText: "s"}
	rs := &RenderState{}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Render goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			o.View(80, 24, rs)
			o.Status(rs)
		}
	}()

	// Task goroutine mutating overlay state.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			o.edit(func() {
				o.Lines = []string{"a", "b"}
				o.Lines = append(o.Lines, "c")
				o.Help = "q/esc: cancel"
				o.StatusText = "working..."
			})
		}
		close(stop)
	}()

	wg.Wait()
}

// TestNextToastIDConcurrentUnique verifies the toast ID counter is safe to bump
// from multiple goroutines (ShowToast on the main goroutine vs the upgrade task)
// and never hands out a duplicate.
func TestNextToastIDConcurrentUnique(t *testing.T) {
	nextToastID.Store(0)

	const goroutines, perG = 8, 500
	var wg sync.WaitGroup
	results := make([][]int, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			ids := make([]int, perG)
			for i := range ids {
				ids[i] = int(nextToastID.Add(1))
			}
			results[g] = ids
		}(g)
	}
	wg.Wait()

	seen := make(map[int]bool, goroutines*perG)
	for _, ids := range results {
		for _, id := range ids {
			if seen[id] {
				t.Fatalf("duplicate toast id handed out: %d", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != goroutines*perG {
		t.Fatalf("expected %d unique ids, got %d", goroutines*perG, len(seen))
	}
}
