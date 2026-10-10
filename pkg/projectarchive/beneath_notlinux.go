//go:build unix && !linux

package projectarchive

import "os"

// openDir opens the directory name below the root with no link in its
// path; without openat2 the walk does it one component at a time.
func (t *stagingTree) openDir(name string) (*os.File, error) { return t.walkDir(name) }
