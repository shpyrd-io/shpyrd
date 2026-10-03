package projectarchive

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
)

// MeasureVolume estimates uncompressed file bytes to copy. It never reads file
// contents or follows symlinks; a running application may change this estimate.
func MeasureVolume(ctx context.Context, directory string, out io.Writer) error {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	var total int64
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if errors.Is(walkErr, fs.ErrNotExist) {
			return nil
		}
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
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(struct {
		Bytes int64 `json:"bytes"`
	}{total})
}
