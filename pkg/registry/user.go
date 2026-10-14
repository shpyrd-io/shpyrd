package registry

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// ImageUser is the non-root identity declared by an image's USER.
type ImageUser struct{ UID, GID int64 }
type imageLayer struct {
	Digest    string `json:"digest"`
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
}

func numericID(s string) (int64, bool) {
	n, err := strconv.ParseUint(s, 10, 31)
	return int64(n), err == nil
}

// resolveUser reads only account files, never extracting image contents onto
// the host. Numeric users without a group already work with runAsNonRoot.
func (c *Client) resolveUser(ctx context.Context, repo, user string, layers []imageLayer) (*ImageUser, error) {
	name, group, explicitGroup := strings.Cut(user, ":")
	uid, numeric := numericID(name)
	if name == "" || (numeric && !explicitGroup) {
		return nil, nil
	}
	if numeric && uid == 0 {
		return nil, nil
	}
	files := map[string]string{}
	found := map[string]bool{}
	budget := int64(2 << 30)
	for i := len(layers) - 1; i >= 0 && (!found["etc/passwd"] || (explicitGroup && !found["etc/group"])); i-- {
		if err := c.accountLayer(ctx, repo, layers[i], files, found, &budget); err != nil {
			return nil, err
		}
	}
	gid := int64(0)
	if !numeric {
		matched := false
		for _, line := range strings.Split(files["etc/passwd"], "\n") {
			f := strings.Split(line, ":")
			if len(f) != 7 || f[0] != name {
				continue
			}
			var valid bool
			uid, valid = numericID(f[2])
			if !valid || uid == 0 {
				return nil, nil
			}
			gid, valid = numericID(f[3])
			if !valid {
				return nil, nil
			}
			matched = true
			break
		}
		if !matched {
			return nil, nil
		}
	}
	if explicitGroup {
		var valid bool
		gid, valid = numericID(group)
		if !valid {
			matched := false
			for _, line := range strings.Split(files["etc/group"], "\n") {
				f := strings.Split(line, ":")
				if len(f) == 4 && f[0] == group {
					gid, matched = numericID(f[2])
					break
				}
			}
			if !matched {
				return nil, nil
			}
		}
	}
	return &ImageUser{UID: uid, GID: gid}, nil
}

func (c *Client) accountLayer(ctx context.Context, repo string, layer imageLayer, files map[string]string, found map[string]bool, budget *int64) error {
	resp, err := c.do(ctx, "GET", "/v2/"+repo+"/blobs/"+layer.Digest, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var reader io.Reader = resp.Body
	switch {
	case strings.HasSuffix(layer.MediaType, "+gzip"), strings.HasSuffix(layer.MediaType, ".gzip"):
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return err
		}
		defer gz.Close()
		reader = gz
	case strings.HasSuffix(layer.MediaType, "+zstd"):
		z, err := zstd.NewReader(reader, zstd.WithDecoderMaxMemory(64<<20))
		if err != nil {
			return err
		}
		defer z.Close()
		reader = z
	case layer.MediaType == "application/vnd.oci.image.layer.v1.tar":
	default:
		return fmt.Errorf("unsupported layer type %q", layer.MediaType)
	}
	limited := &io.LimitedReader{R: reader, N: *budget + 1}
	defer func() { *budget = limited.N - 1 }()
	tr := tar.NewReader(limited)
	values := map[string]string{}
	present, hidden := map[string]bool{}, map[string]bool{}
	for {
		h, err := tr.Next()
		if limited.N == 0 {
			return fmt.Errorf("account lookup exceeded image scan limit")
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := path.Clean(strings.TrimPrefix(h.Name, "/"))
		for _, target := range []string{"etc/passwd", "etc/group"} {
			if found[target] {
				continue
			}
			if name == target {
				present[target] = true
				if h.Typeflag == tar.TypeReg && h.Size <= 1<<20 {
					b, err := io.ReadAll(tr)
					if err != nil {
						return err
					}
					values[target] = string(b)
				} else {
					values[target] = ""
				} // Do not follow links outside the account file.
			}
			if name == ".wh.etc" || name == ".wh..wh..opq" || name == "etc/.wh..wh..opq" || name == "etc/.wh."+path.Base(target) || (name == "etc" && h.Typeflag != tar.TypeDir) {
				hidden[target] = true
			}
		}
	}
	for _, target := range []string{"etc/passwd", "etc/group"} {
		if found[target] {
			continue
		}
		if present[target] {
			files[target], found[target] = values[target], true
		} else if hidden[target] {
			found[target] = true
		}
	}
	return nil
}
