//go:build windows

package tui

import (
	"os"
	"time"

	"golang.org/x/term"
)

// watchResize polls the console size: Windows has no SIGWINCH, and polling
// is also how libuv gives Node its 'resize' event there.
func watchResize(onResize func()) func() {
	done := make(chan struct{})
	go func() {
		fd := int(os.Stdout.Fd())
		w, h, _ := term.GetSize(fd)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if nw, nh, err := term.GetSize(fd); err == nil && (nw != w || nh != h) {
					w, h = nw, nh
					onResize()
				}
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}
