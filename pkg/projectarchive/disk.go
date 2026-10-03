package projectarchive

import (
	"encoding/json"
	"golang.org/x/sys/unix"
	"io"
)

type DiskCapacity struct {
	Available uint64 `json:"available"`
	Total     uint64 `json:"total"`
}

func diskCapacity(directory string, out io.Writer) error {
	var st unix.Statfs_t
	if err := unix.Statfs(directory, &st); err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(DiskCapacity{Available: uint64(st.Bavail) * uint64(st.Bsize), Total: uint64(st.Blocks) * uint64(st.Bsize)})
}
