//go:build !unix

package projectarchive

// Archives are made and restored by the platform's own pods, which run on
// Linux; on another system (the CLI built for Windows) these answer that
// they cannot, so the package still compiles there.

import (
	"errors"
	"io"
	"os"
)

func diskCapacity(directory string, out io.Writer) error {
	return errors.New("disk capacity is only read on Linux")
}

func ownerOf(info os.FileInfo) (uid, gid int, ok bool) {
	return 0, 0, false
}
