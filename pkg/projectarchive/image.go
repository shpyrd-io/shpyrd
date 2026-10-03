package projectarchive

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
)

// imageStorage stores OCI blobs as ordinary checksummed bundle entries. ORAS
// handles distribution authentication, image indexes and blob verification;
// archives do not depend on the original registry remaining available.
type imageStorage struct{ bundle *Bundle }

func imageEntry(d ocispec.Descriptor) (string, error) {
	if err := d.Digest.Validate(); err != nil {
		return "", err
	}
	if d.Digest.Algorithm() != digest.SHA256 || d.Size < 0 {
		return "", errors.New("unsupported image blob digest or size")
	}
	return "image/blobs/sha256/" + d.Digest.Encoded(), nil
}

func (s imageStorage) Exists(_ context.Context, d ocispec.Descriptor) (bool, error) {
	name, err := imageEntry(d)
	if err != nil {
		return false, err
	}
	e, ok := s.bundle.entries[name]
	if ok && (e.Size != d.Size || e.SHA256 != d.Digest.Encoded()) {
		return false, fmt.Errorf("image blob descriptor mismatch: %s", d.Digest)
	}
	return ok, nil
}

func (s imageStorage) Fetch(ctx context.Context, d ocispec.Descriptor) (io.ReadCloser, error) {
	if ok, err := s.Exists(ctx, d); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("missing image blob: %s", d.Digest)
	}
	name, _ := imageEntry(d)
	return s.bundle.Open(name)
}

func (s imageStorage) Push(ctx context.Context, d ocispec.Descriptor, r io.Reader) error {
	name, err := imageEntry(d)
	if err != nil {
		return err
	}
	if exists, err := s.Exists(ctx, d); err != nil || exists {
		return err
	}
	if err := s.bundle.Add(ctx, name, r); err != nil {
		return err
	}
	_, err = s.Exists(ctx, d)
	return err
}

// ExportImage includes the entire manifest graph, including every platform of
// an image index. One transfer at a time bounds both RAM and temporary disk.
func (b *Bundle) ExportImage(ctx context.Context, source oras.ReadOnlyTarget, reference string) (ocispec.Descriptor, error) {
	d, err := source.Resolve(ctx, reference)
	if err != nil {
		return d, err
	}
	err = oras.CopyGraph(ctx, source, imageStorage{b}, d, oras.CopyGraphOptions{Concurrency: 1, MaxMetadataBytes: metadataLimit})
	return d, err
}

func (b *Bundle) ImportImage(ctx context.Context, destination content.Storage, root ocispec.Descriptor) error {
	return oras.CopyGraph(ctx, imageStorage{b}, destination, root, oras.CopyGraphOptions{Concurrency: 1, MaxMetadataBytes: metadataLimit})
}
