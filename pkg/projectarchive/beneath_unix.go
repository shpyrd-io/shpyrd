//go:build unix

package projectarchive

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"golang.org/x/sys/unix"
)

// A staged tree is written without following a symbolic link anywhere in
// the path, so an archive cannot lay a link and then write through it
// (#129). Every entry is made from a handle on its parent directory. On
// Linux the kernel opens that handle refusing links and escapes in one
// call (openat2 with RESOLVE_BENEATH and RESOLVE_NO_SYMLINKS); where that
// call is missing, and on other systems, the path is walked one component
// at a time from the root's handle, refusing a link on the way. Names come
// from the archive and were checked to be clean and relative before.
type stagingTree struct {
	root *os.Root
	dir  *os.File
}

func openStagingTree(root *os.Root) (*stagingTree, error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	return &stagingTree{root: root, dir: dir}, nil
}

func (t *stagingTree) Close() error { return t.dir.Close() }

// walkDir opens the directory name below the root one component at a
// time, never following a link.
func (t *stagingTree) walkDir(name string) (*os.File, error) {
	fd, err := unix.Openat(int(t.dir.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	if name != "." {
		for _, part := range strings.Split(name, "/") {
			next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			unix.Close(fd)
			if err != nil {
				return nil, linkRefusal(name, err)
			}
			fd = next
		}
	}
	return os.NewFile(uintptr(fd), name), nil
}

// linkRefusal words the error of opening a path component without
// following links: a link or a file where a directory was expected is the
// archive writing through something it may not.
func linkRefusal(name string, err error) error {
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.EXDEV) {
		return fmt.Errorf("archive entry %s: %w", name, errLinkInPath)
	}
	return &os.PathError{Op: "open", Path: name, Err: err}
}

// create makes the regular file name, which must not exist yet.
func (t *stagingTree) create(name string, perm os.FileMode) (*os.File, error) {
	dir, err := t.openDir(path.Dir(name))
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	fd, err := unix.Openat(int(dir.Fd()), path.Base(name), unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(perm.Perm()))
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	return os.NewFile(uintptr(fd), name), nil
}

// symlink lays the link name with target as its text, whatever it says.
func (t *stagingTree) symlink(target, name string) error {
	dir, err := t.openDir(path.Dir(name))
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := unix.Symlinkat(target, int(dir.Fd()), path.Base(name)); err != nil {
		return &os.PathError{Op: "symlink", Path: name, Err: err}
	}
	return nil
}

// mkdirAll makes name and the directories above it that are missing, as
// os.MkdirAll does, refusing a link or a file in any position.
func (t *stagingTree) mkdirAll(name string, perm os.FileMode) error {
	if name == "." {
		return nil
	}
	fd, err := unix.Openat(int(t.dir.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return &os.PathError{Op: "open", Path: name, Err: err}
	}
	for _, part := range strings.Split(name, "/") {
		if err := unix.Mkdirat(fd, part, uint32(perm.Perm())); err != nil && !errors.Is(err, unix.EEXIST) {
			unix.Close(fd)
			return &os.PathError{Op: "mkdir", Path: name, Err: err}
		}
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if err != nil {
			return linkRefusal(name, err)
		}
		fd = next
	}
	return unix.Close(fd)
}
