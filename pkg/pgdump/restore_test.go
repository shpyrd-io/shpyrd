package pgdump

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type fakeGetter struct{ objects map[string]string }

func (g fakeGetter) Get(_ context.Context, key string) (io.ReadCloser, error) {
	body, ok := g.objects[key]
	if !ok {
		return nil, errors.New("the copy " + key + ": not found")
	}
	return io.NopCloser(strings.NewReader(body)), nil
}

type fakeRestorer struct{ got bytes.Buffer }

func (r *fakeRestorer) Restore(_ context.Context, dump io.Reader) error {
	_, err := io.Copy(&r.got, dump)
	return err
}

func TestRestoreLoadsTheNamedCopy(t *testing.T) {
	from := fakeGetter{objects: map[string]string{"db_a/20261014T030700Z.dump": "the rows"}}
	into := &fakeRestorer{}
	if err := Restore(context.Background(), from, "db_a/20261014T030700Z.dump", into); err != nil || into.got.String() != "the rows" {
		t.Errorf("restore = %v, loaded %q", err, into.got.String())
	}
	if err := Restore(context.Background(), from, "db_a/20261001T030700Z.dump", into); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("a missing copy: %v", err)
	}
}

// An upload of unknown length must say its part size, or the client
// buffers parts sized for a 5 TiB object and the pod is OOM-killed.
func TestUploadsUseSmallParts(t *testing.T) {
	if o := putOptions(); o.PartSize != 16<<20 {
		t.Errorf("part size = %d", o.PartSize)
	}
}

// The restore list keeps everything but the comments on extensions.
func TestRestoreListLeavesExtensionCommentsOut(t *testing.T) {
	toc := `;
; Archive created at 2026-10-15 09:00:00 UTC
;
3; 2615 16400 SCHEMA - public pg_database_owner
2; 3079 16401 EXTENSION - vector
4; 0 0 COMMENT - EXTENSION vector
5; 3079 16402 EXTENSION - pgcrypto
6; 0 0 COMMENT - EXTENSION pgcrypto
7; 1259 16403 TABLE public orders u_abc
8; 0 0 COMMENT - TABLE public orders u_abc
`
	got := RestoreList(toc)
	if strings.Contains(got, "COMMENT - EXTENSION") || !strings.Contains(got, "EXTENSION - vector") || !strings.Contains(got, "COMMENT - TABLE public orders") || !strings.Contains(got, "Archive created") {
		t.Errorf("list = %q", got)
	}
}
