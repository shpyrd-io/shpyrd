//go:build unix

package projectarchive

import (
	"encoding/json"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func diskCapacity(directory string, out io.Writer) error {
	var st unix.Statfs_t
	if err := unix.Statfs(directory, &st); err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(DiskCapacity{Available: uint64(st.Bavail) * uint64(st.Bsize), Total: uint64(st.Blocks) * uint64(st.Bsize)})
}

// ownerOf is the owner of a file as the system keeps it: archives carry it,
// so a volume comes back with the ownership its app expects.
func ownerOf(info os.FileInfo) (uid, gid int, ok bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(st.Uid), int(st.Gid), true
}
