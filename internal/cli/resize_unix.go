//go:build !windows

package cli

import (
	"os"
	"os/signal"
	"syscall"
)

// watchResize calls onResize on every SIGWINCH until the returned stop
// function runs.
func watchResize(onResize func()) (stop func()) {
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	go func() {
		for range winch {
			onResize()
		}
	}()
	return func() {
		signal.Stop(winch)
		close(winch)
	}
}
