package projectarchive

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
)

const recoveryDir = ".shpyrd-restore"

var operationName = regexp.MustCompile(`^[a-f0-9]{32}$`)

// WarningPrefix marks, on the helper's error stream, a line that is a
// warning for the operation's report rather than part of a failure.
const WarningPrefix = "warning: "

// errLinkInPath says an archive entry would be written through a link.
var errLinkInPath = errors.New("the path goes through a link and was refused")

// ExportVolume archives the tree as it is. A link is written as a link with
// its target text, never followed, whatever it points at: browser profiles
// leave lock links into a temporary directory and virtual environments link
// their interpreter to the system's (#129). A link that points outside the
// volume, or at nothing in it, is said on report, one line per link, and
// the export goes on. Recovery workspaces are platform-owned and never part
// of a user backup.
func ExportVolume(ctx context.Context, directory string, w io.Writer, report io.Writer) error {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	tw := tar.NewWriter(w)
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
			if sentence := linkWarning(root, name, link); sentence != "" {
				fmt.Fprintln(report, WarningPrefix+sentence)
			}
		} else if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported volume file: %s", name)
		}
		h, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		h.Name = name
		h.Mode &= 0777 // never transfer setuid, setgid or sticky privileges
		if u, g, ok := ownerOf(info); ok {
			h.Uid, h.Gid = u, g
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := root.Open(name)
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, contextReader{ctx, f})
		f.Close()
		return err
	})
	if err != nil {
		return err
	}
	return tw.Close()
}

func safeVolumeName(name string) bool {
	return validName(name) && name != recoveryDir && !strings.HasPrefix(name, recoveryDir+"/")
}

// linkWarning is the sentence for a link the volume cannot answer for: it
// names the file and its target, so the person knows what was copied as it
// is. Empty for a link that points at something inside the volume.
func linkWarning(root *os.Root, name, target string) string {
	if target == "" || strings.HasPrefix(target, "/") {
		return fmt.Sprintf("the link %s points outside the volume, at %s; it was copied as it is", name, target)
	}
	resolved := path.Clean(path.Join(path.Dir(name), target))
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return fmt.Sprintf("the link %s points outside the volume, at %s; it was copied as it is", name, target)
	}
	if _, err := root.Lstat(resolved); err != nil {
		return fmt.Sprintf("the link %s points at %s, which is not in the volume; it was copied as it is", name, target)
	}
	return ""
}

// ValidateVolume checks a nested tar without extraction. Extraction repeats
// these checks and writes every entry from a handle on its parent directory
// that was opened without following a link, so no entry is written through
// one, whatever a link in the archive says (#129).
func ValidateVolume(ctx context.Context, r io.Reader, limit int64) error {
	return readVolume(ctx, r, limit, nil)
}

func readVolume(ctx context.Context, r io.Reader, limit int64, tree *stagingTree, rootHeader ...*bool) error {
	if limit <= 0 {
		limit = DefaultLimit
	}
	tr := tar.NewReader(contextReader{ctx, r})
	seen := map[string]byte{}
	parents := map[string]bool{}
	directories := map[string]tar.Header{}
	var size int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			if err := finishTar(contextReader{ctx, r}); err != nil {
				return err
			}
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(h.Name, "/")
		if (!safeVolumeName(name) && !(name == "." && h.Typeflag == tar.TypeDir)) || seen[name] != 0 || len(seen) >= 100000 {
			return fmt.Errorf("invalid/duplicate volume path: %q", name)
		}
		if parents[name] && h.Typeflag == tar.TypeSymlink {
			return fmt.Errorf("archive entry %s: %w", name, errLinkInPath)
		}
		if parents[name] && h.Typeflag != tar.TypeDir {
			return fmt.Errorf("archive entry %s has entries below it but is not a folder", name)
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if kind, ok := seen[parent]; ok && kind == tar.TypeSymlink {
				return fmt.Errorf("archive entry %s: %w", name, errLinkInPath)
			} else if ok && kind != tar.TypeDir {
				return fmt.Errorf("archive entry %s is below %s, which is not a folder", name, parent)
			}
			parents[parent] = true
		}
		if name == "." && len(rootHeader) > 0 {
			*rootHeader[0] = true
		}
		seen[name] = h.Typeflag
		if h.Size < 0 || h.Size > limit-size {
			return errors.New("volume exceeds size limit")
		}
		size += h.Size
		if h.Mode & ^int64(0777) != 0 {
			return errors.New("privileged file permissions in volume archive")
		}
		if h.Uid < 0 || h.Gid < 0 {
			return errors.New("invalid volume file ownership")
		}
		switch h.Typeflag {
		case tar.TypeDir, tar.TypeReg:
		case tar.TypeSymlink:
			if h.Linkname == "" || strings.ContainsRune(h.Linkname, 0) {
				return fmt.Errorf("archive entry %s is a link without a target", name)
			}
		default:
			return fmt.Errorf("unsupported volume entry type: %s", name)
		}
		if tree == nil {
			continue
		}
		if err := tree.mkdirAll(path.Dir(name), 0700); err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			err = tree.mkdirAll(name, os.FileMode(h.Mode)|0700)
			directories[name] = *h
		case tar.TypeSymlink:
			err = tree.symlink(h.Linkname, name)
			if err == nil && os.Geteuid() == 0 {
				err = tree.root.Lchown(name, h.Uid, h.Gid)
			}
		case tar.TypeReg:
			var f *os.File
			f, err = tree.create(name, os.FileMode(h.Mode))
			if err == nil {
				_, err = io.Copy(f, tr)
				if err == nil && os.Geteuid() == 0 {
					err = f.Chown(h.Uid, h.Gid)
				}
				if err == nil {
					err = f.Chmod(os.FileMode(h.Mode))
				}
				if err == nil {
					err = f.Sync()
				}
				closeErr := f.Close()
				if err == nil {
					err = closeErr
				}
			}
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeReg {
			if err := tree.root.Chtimes(name, h.ModTime, h.ModTime); err != nil {
				return err
			}
		}
	}
	// Apply directory permissions last, children first. A read-only parent
	// must not prevent extraction; creating children must not change the
	// restored directory timestamp after it has been set. These paths were
	// all just made above without a link in them.
	names := make([]string, 0, len(directories))
	for name := range directories {
		names = append(names, name)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for _, name := range names {
		h := directories[name]
		if os.Geteuid() == 0 {
			if err := tree.root.Chown(name, h.Uid, h.Gid); err != nil {
				return err
			}
		}
		if err := tree.root.Chmod(name, os.FileMode(h.Mode)); err != nil {
			return err
		}
		if err := tree.root.Chtimes(name, h.ModTime, h.ModTime); err != nil {
			return err
		}
	}
	return nil
}

type volumeRoot struct {
	Mode uint32 `json:"mode"`
	UID  int    `json:"uid"`
	GID  int    `json:"gid"`
}

func rootMetadata(root *os.Root) (*volumeRoot, error) {
	info, err := root.Stat(".")
	if err != nil {
		return nil, err
	}
	m := &volumeRoot{Mode: uint32(info.Mode().Perm())}
	if u, g, ok := ownerOf(info); ok {
		m.UID, m.GID = u, g
	}
	return m, nil
}
func applyRootMetadata(root *os.Root, m *volumeRoot) error {
	if m == nil {
		return nil
	}
	if m.Mode & ^uint32(0777) != 0 || m.UID < 0 || m.GID < 0 {
		return errors.New("invalid volume root metadata")
	}
	if os.Geteuid() == 0 {
		if err := root.Chown(".", m.UID, m.GID); err != nil {
			return err
		}
	}
	return root.Chmod(".", os.FileMode(m.Mode))
}

type volumeState struct {
	OriginalRoot       *volumeRoot `json:"originalRoot,omitempty"`
	IncomingRoot       *volumeRoot `json:"incomingRoot,omitempty"`
	Phase              string      `json:"phase"`
	Original, Incoming []string
}

// VolumeTransaction preserves the previous tree on the same PVC through
// commit and health verification. State makes each step restartable.
type VolumeTransaction struct {
	root *os.Root
	base string
}

func OpenVolumeTransaction(directory, operation string) (*VolumeTransaction, error) {
	if !operationName.MatchString(operation) {
		return nil, errors.New("invalid operation id")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	return &VolumeTransaction{root: root, base: recoveryDir + "/" + operation}, nil
}
func (v *VolumeTransaction) Close() error { return v.root.Close() }
func (v *VolumeTransaction) readState() (volumeState, error) {
	var s volumeState
	f, err := v.root.Open(v.base + "/state.json")
	if err != nil {
		return s, err
	}
	defer f.Close()
	err = json.NewDecoder(io.LimitReader(f, metadataLimit)).Decode(&s)
	if err != nil {
		return s, err
	}
	switch s.Phase {
	case "staged", "installing", "committed", "removing-new", "restoring-old", "rolled-back":
	default:
		return s, errors.New("invalid volume transaction phase")
	}
	for _, names := range [][]string{s.Original, s.Incoming} {
		seen := map[string]bool{}
		for _, name := range names {
			if !safeVolumeName(name) || path.Base(name) != name || seen[name] {
				return s, errors.New("invalid volume transaction inventory")
			}
			seen[name] = true
		}
	}
	return s, err
}
func (v *VolumeTransaction) save(s volumeState) error {
	f, err := v.root.OpenFile(v.base+"/state.tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	err = json.NewEncoder(f).Encode(s)
	if err == nil {
		err = f.Sync()
	}
	f.Close()
	if err != nil {
		return err
	}
	if err := v.root.Rename(v.base+"/state.tmp", v.base+"/state.json"); err != nil {
		return err
	}
	f, err = v.root.Open(v.base)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func (v *VolumeTransaction) Stage(ctx context.Context, r io.Reader, limit int64) error {
	if _, err := v.readState(); err == nil {
		return errors.New("volume operation already staged")
	} else if !os.IsNotExist(err) {
		return err
	}
	// A failed/crashed extraction has not touched original files. Discard
	// only its incomplete staging tree before retrying the same operation.
	if err := v.root.RemoveAll(v.base); err != nil {
		return err
	}
	if err := v.root.MkdirAll(v.base+"/incoming", 0700); err != nil {
		return err
	}
	if err := v.root.MkdirAll(v.base+"/previous", 0700); err != nil {
		return err
	}
	root, err := v.root.OpenRoot(v.base + "/incoming")
	if err != nil {
		return err
	}
	defer root.Close()
	tree, err := openStagingTree(root)
	if err != nil {
		return err
	}
	defer tree.Close()
	hasRoot := false
	if err := readVolume(ctx, r, limit, tree, &hasRoot); err != nil {
		return err
	}
	original, err := fs.ReadDir(v.root.FS(), ".")
	if err != nil {
		return err
	}
	incoming, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	s := volumeState{Phase: "staged"}
	s.OriginalRoot, err = rootMetadata(v.root)
	if err != nil {
		return err
	}
	s.IncomingRoot, err = rootMetadata(root)
	if err != nil {
		return err
	}
	if !hasRoot {
		s.IncomingRoot = s.OriginalRoot
	}
	for _, e := range original {
		if e.Name() != recoveryDir {
			s.Original = append(s.Original, e.Name())
		}
	}
	for _, e := range incoming {
		s.Incoming = append(s.Incoming, e.Name())
	}
	return v.save(s)
}
func (v *VolumeTransaction) moveIfPresent(from, to string) error {
	if _, err := v.root.Lstat(from); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err := v.root.Rename(from, to); err != nil {
		return err
	}
	for _, directory := range []string{path.Dir(from), path.Dir(to)} {
		f, err := v.root.Open(directory)
		if err != nil {
			return err
		}
		err = f.Sync()
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
func (v *VolumeTransaction) Commit() error {
	s, err := v.readState()
	if err != nil {
		return err
	}
	if s.Phase == "committed" {
		return nil
	}
	if s.Phase == "staged" {
		for _, name := range s.Original {
			if _, err := v.root.Lstat(v.base + "/previous/" + name); err == nil {
				continue
			}
			if err := v.moveIfPresent(name, v.base+"/previous/"+name); err != nil {
				return err
			}
		}
		s.Phase = "installing"
		if err := v.save(s); err != nil {
			return err
		}
	}
	if s.Phase != "installing" {
		return errors.New("volume transaction cannot commit in this phase")
	}
	for _, name := range s.Incoming {
		if err := v.moveIfPresent(v.base+"/incoming/"+name, name); err != nil {
			return err
		}
	}
	if err := applyRootMetadata(v.root, s.IncomingRoot); err != nil {
		return err
	}
	s.Phase = "committed"
	return v.save(s)
}
func (v *VolumeTransaction) Rollback() error {
	s, err := v.readState()
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.Phase == "rolled-back" {
		return nil
	}
	if s.Phase == "installing" || s.Phase == "committed" || s.Phase == "removing-new" {
		s.Phase = "removing-new"
		if err := v.save(s); err != nil {
			return err
		}
		for _, name := range s.Incoming {
			if err := v.root.RemoveAll(name); err != nil {
				return err
			}
		}
	}
	s.Phase = "restoring-old"
	if err := v.save(s); err != nil {
		return err
	}
	for _, name := range s.Original {
		if err := v.moveIfPresent(v.base+"/previous/"+name, name); err != nil {
			return err
		}
	}
	if err := applyRootMetadata(v.root, s.OriginalRoot); err != nil {
		return err
	}
	s.Phase = "rolled-back"
	return v.save(s)
}
func (v *VolumeTransaction) Finish() error { return v.root.RemoveAll(v.base) }

// VolumeMain is also used in a short-lived helper Pod mounting exactly one
// project PVC. The helper has no service-account token or host mount. Its
// warnings go to stderr, each on a line that starts with WarningPrefix.
func VolumeMain(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 1 && args[0] == "wait" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		<-ctx.Done()
		return nil
	}
	if len(args) < 2 {
		return errors.New("project-volume needs action and mounted directory")
	}
	directory, err := filepath.Abs(args[1])
	if err != nil {
		return err
	}
	if args[0] == "usage" {
		return MeasureVolume(context.Background(), directory, stdout)
	}
	if args[0] == "fingerprint" {
		return FingerprintVolume(context.Background(), directory, stdout)
	}
	if args[0] == "capacity" {
		return diskCapacity(directory, stdout)
	}
	if args[0] == "export" {
		return ExportVolume(context.Background(), directory, stdout, stderr)
	}
	if len(args) != 3 {
		return errors.New("project-volume needs operation id")
	}
	v, err := OpenVolumeTransaction(directory, args[2])
	if err != nil {
		return err
	}
	defer v.Close()
	switch args[0] {
	case "stage":
		return v.Stage(context.Background(), stdin, DefaultLimit)
	case "commit":
		return v.Commit()
	case "rollback":
		return v.Rollback()
	case "finish":
		return v.Finish()
	default:
		return errors.New("unknown project-volume action")
	}
}
