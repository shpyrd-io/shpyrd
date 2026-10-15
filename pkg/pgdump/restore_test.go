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
