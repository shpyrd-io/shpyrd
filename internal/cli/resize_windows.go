//go:build windows

package cli

import "time"

// watchResize calls onResize twice a second until the returned stop
// function runs: Windows has no SIGWINCH, and onResize sends the size
// only when it can read one.
func watchResize(onResize func()) (stop func()) {
	done := make(chan struct{})
	go func() {
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				onResize()
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}
