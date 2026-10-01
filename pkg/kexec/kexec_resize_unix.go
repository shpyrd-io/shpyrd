//go:build !windows

package kexec

import (
	"os"
	"os/signal"
	"syscall"
)

// watch forwards the terminal's size on every SIGWINCH until the queue stops.
func (q *sizeQueue) watch() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	defer signal.Stop(sig)
	for {
		select {
		case <-sig:
			q.push()
		case <-q.done:
			return
		}
	}
}
