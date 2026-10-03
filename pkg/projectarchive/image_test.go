package projectarchive

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content/memory"
)

func TestImageArchiveRoundTripIncludesEveryPlatformAndSharedLayers(t *testing.T) {
	ctx := context.Background()
	source := memory.New()
	push := func(media string, data []byte) ocispec.Descriptor {
		t.Helper()
		d := ocispec.Descriptor{MediaType: media, Digest: digest.FromBytes(data), Size: int64(len(data))}
		if err := source.Push(ctx, d, bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		return d
	}
	layer := push(ocispec.MediaTypeImageLayer, []byte("shared layer contents"))
	manifests := []ocispec.Descriptor{}
	for _, arch := range []string{"amd64", "arm64"} {
		config := push(ocispec.MediaTypeImageConfig, []byte(`{"architecture":"`+arch+`","os":"linux"}`))
		data, err := json.Marshal(ocispec.Manifest{Versioned: specs.Versioned{SchemaVersion: 2}, MediaType: ocispec.MediaTypeImageManifest, Config: config, Layers: []ocispec.Descriptor{layer}})
		if err != nil {
			t.Fatal(err)
		}
		d := push(ocispec.MediaTypeImageManifest, data)
		d.Platform = &ocispec.Platform{OS: "linux", Architecture: arch}
		manifests = append(manifests, d)
	}
	data, err := json.Marshal(ocispec.Index{Versioned: specs.Versioned{SchemaVersion: 2}, MediaType: ocispec.MediaTypeImageIndex, Manifests: manifests})
	if err != nil {
		t.Fatal(err)
	}
	root := push(ocispec.MediaTypeImageIndex, data)
	if err := source.Tag(ctx, root, "latest"); err != nil {
		t.Fatal(err)
	}
	b, err := New(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	got, err := b.ExportImage(ctx, source, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != root.Digest {
		t.Fatal("image index changed")
	}
	if len(b.Names()) != 6 {
		t.Fatalf("want 6 unique blobs, got %v", b.Names())
	}
	destination := memory.New()
	if err := b.ImportImage(ctx, destination, root); err != nil {
		t.Fatal(err)
	}
	for _, d := range append(manifests, layer, root) {
		r, err := destination.Fetch(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil || digest.FromBytes(data) != d.Digest {
			t.Fatalf("digest not preserved: %s %v", d.Digest, err)
		}
	}
	bad := root
	bad.Size++
	if err := b.ImportImage(ctx, memory.New(), bad); err == nil {
		t.Fatal("accepted inconsistent descriptor")
	}
}
