package projectarchive

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"syscall"
)

// FingerprintVolume verifies the copied tree independently from the transfer.
// Length-prefixed JSON metadata separates paths/contents; timestamps are
// excluded because directory writes and reads can change them during a copy.
func FingerprintVolume(ctx context.Context, directory string, out io.Writer) error {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	hash := sha256.New()
	enc := json.NewEncoder(hash)
	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		if name == recoveryDir {
			return fs.SkipDir
		}
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = root.Readlink(name)
			if err != nil {
				return err
			}
		}
		uid, gid := 0, 0
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			uid, gid = int(st.Uid), int(st.Gid)
		}
		size := int64(0)
		if info.Mode().IsRegular() {
			size = info.Size()
		}
		if err := enc.Encode(struct {
			Name, Link string
			Mode       uint32
			UID, GID   int
			Size       int64
		}{name, link, uint32(info.Mode() & (os.ModeType | os.ModePerm)), uid, gid, size}); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := root.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(hash, contextReader{ctx, f})
		return err
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "%x\n", hash.Sum(nil))
	return err
}
