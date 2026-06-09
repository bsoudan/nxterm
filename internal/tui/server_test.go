package tui

import (
	"sync"
	"testing"
)

// TestServerSendCloseRace exercises concurrent Send/Close. Closing s.ch while a
// Send was mid-flight let the send land on a closed channel and panic with
// "send on closed channel". Close must only signal shutdown, never close s.ch.
func TestServerSendCloseRace(t *testing.T) {
	for iter := 0; iter < 200; iter++ {
		s := NewServer(4, "test")

		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					s.Send(struct{}{})
				}
			}()
		}

		s.Close()
		wg.Wait()
	}
}
