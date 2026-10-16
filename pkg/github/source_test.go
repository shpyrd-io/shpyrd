package github_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
)

func TestTheSourceKeepsOneClientUntilTheSecretChanges(t *testing.T) {
	fake := githubtest.NewServer(t)
	fake.SetNow(t0)
	c := crfake.NewClientBuilder().WithObjects(fake.Secret("shpyrd-system")).Build()
	src := fake.Source(c, "shpyrd-system")
	ctx := context.Background()
	first, err := src.Client(ctx)
	if err != nil || first.App().Slug != githubtest.Slug {
		t.Fatalf("first: %v", err)
	}
	if again, _ := src.Client(ctx); again != first {
		t.Error("the same Secret, the same client, and its tokens")
	}
	key := types.NamespacedName{Namespace: "shpyrd-system", Name: github.SecretName}
	sec := &corev1.Secret{}
	if err := c.Get(ctx, key, sec); err != nil {
		t.Fatal(err)
	}
	sec.Data[github.KeySlug] = []byte("renamed")
	if err := c.Update(ctx, sec); err != nil {
		t.Fatal(err)
	}
	if soon, _ := src.Client(ctx); soon != first {
		t.Error("the Secret is read again at most every 30 s")
	}
	fake.SetNow(t0.Add(31 * time.Second))
	changed, err := src.Client(ctx)
	if err != nil || changed == first || changed.App().Slug != "renamed" {
		t.Fatalf("a changed Secret, a new client: %v", err)
	}
	if err := c.Delete(ctx, sec); err != nil {
		t.Fatal(err)
	}
	fake.SetNow(t0.Add(62 * time.Second))
	if _, err := src.Client(ctx); !errors.Is(err, github.ErrNotConfigured) {
		t.Errorf("deleted: %v", err)
	}
}

func TestAnIncompleteSecretIsNoApp(t *testing.T) {
	fake := githubtest.NewServer(t)
	sec := fake.Secret("shpyrd-system")
	delete(sec.Data, github.KeyPrivateKey)
	if _, err := github.AppFromSecret(sec); !errors.Is(err, github.ErrNotConfigured) || !strings.Contains(err.Error(), "private-key") {
		t.Errorf("err = %v", err)
	}
	if _, err := (&github.Source{}).Client(context.Background()); !errors.Is(err, github.ErrNotConfigured) {
		t.Errorf("no reader: %v", err)
	}
}
