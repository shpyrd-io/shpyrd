//go:build windows

package kexec

import "time"

// watch polls the terminal's size: Windows has no SIGWINCH, and push only
// queues a size when it can read one.
func (q *sizeQueue) watch() {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			q.push()
		case <-q.done:
			return
		}
	}
}
