//go:build linux

package projectarchive

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// openat2Available is cleared the first time the kernel or the container
// runtime answers that it does not offer the call; the walk takes over.
var openat2Available = true

// openDir opens the directory name below the root with no link in its
// path. The kernel does the whole check in one call where openat2 exists
// (Linux 5.6, and the container runtime must allow it); otherwise the walk
// does the same, one component at a time.
func (t *stagingTree) openDir(name string) (*os.File, error) {
	if openat2Available {
		fd, err := unix.Openat2(int(t.dir.Fd()), name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
		switch {
		case err == nil:
			return os.NewFile(uintptr(fd), name), nil
		case errors.Is(err, unix.ENOSYS), errors.Is(err, unix.EPERM), errors.Is(err, unix.E2BIG), errors.Is(err, unix.EINVAL):
			openat2Available = false
		default:
			return nil, linkRefusal(name, err)
		}
	}
	return t.walkDir(name)
}
