//go:build !unix

package projectarchive

// Volumes are restored by the platform's own pods, which run on Linux; on
// another system (the CLI built for Windows) staging answers that it
// cannot, so the package still compiles there.

import (
	"errors"
	"os"
)

var errNotUnix = errors.New("volumes are only restored on Linux")

type stagingTree struct{ root *os.Root }

func openStagingTree(root *os.Root) (*stagingTree, error) { return nil, errNotUnix }
func (t *stagingTree) Close() error                       { return nil }
func (t *stagingTree) create(name string, perm os.FileMode) (*os.File, error) {
	return nil, errNotUnix
}
func (t *stagingTree) symlink(target, name string) error            { return errNotUnix }
func (t *stagingTree) mkdirAll(name string, perm os.FileMode) error { return errNotUnix }
