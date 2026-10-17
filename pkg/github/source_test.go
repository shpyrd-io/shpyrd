package github_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

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

func TestABrokenSecretStopsTheOldClient(t *testing.T) {
	for name, breakIt := range map[string]func(*corev1.Secret){
		"incomplete": func(s *corev1.Secret) { delete(s.Data, github.KeyClientSecret) },
		"a bad key":  func(s *corev1.Secret) { s.Data[github.KeyPrivateKey] = []byte("not a key") },
	} {
		t.Run(name, func(t *testing.T) {
			fake := githubtest.NewServer(t)
			fake.SetNow(t0)
			c := crfake.NewClientBuilder().WithObjects(fake.Secret("shpyrd-system")).Build()
			src := fake.Source(c, "shpyrd-system")
			ctx := context.Background()
			if _, err := src.Client(ctx); err != nil {
				t.Fatal(err)
			}
			sec := &corev1.Secret{}
			if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: github.SecretName}, sec); err != nil {
				t.Fatal(err)
			}
			breakIt(sec)
			if err := c.Update(ctx, sec); err != nil {
				t.Fatal(err)
			}
			for _, after := range []time.Duration{31 * time.Second, 32 * time.Second, 45 * time.Second, 70 * time.Second} {
				fake.SetNow(t0.Add(after))
				if got, err := src.Client(ctx); got != nil || !errors.Is(err, github.ErrNotConfigured) {
					t.Errorf("at %v: a client %v, err %v", after, got != nil, err)
				}
			}
		})
	}
}

func TestABrokenSecretIsNotReadAgainWithinTheRecheck(t *testing.T) {
	fake := githubtest.NewServer(t)
	fake.SetNow(t0)
	sec := fake.Secret("shpyrd-system")
	sec.Data[github.KeyPrivateKey] = []byte("not a key")
	reads := 0
	c := crfake.NewClientBuilder().WithObjects(sec).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			reads++
			return cl.Get(ctx, key, obj, opts...)
		},
	}).Build()
	src := fake.Source(c, "shpyrd-system")
	ctx := context.Background()
	first, err := src.Client(ctx)
	if first != nil || !errors.Is(err, github.ErrNotConfigured) {
		t.Fatalf("broken: %v", err)
	}
	for _, after := range []time.Duration{time.Second, 29 * time.Second} {
		fake.SetNow(t0.Add(after))
		if got, again := src.Client(ctx); got != nil || again == nil || again.Error() != err.Error() || reads != 1 {
			t.Errorf("at %v: %d reads, err %v", after, reads, again)
		}
	}
	fake.SetNow(t0.Add(31 * time.Second))
	if _, err := src.Client(ctx); err == nil || reads != 2 {
		t.Errorf("after the recheck: %d reads, err %v", reads, err)
	}
}

func TestInvalidateReadsTheSecretAgainAtOnce(t *testing.T) {
	fake := githubtest.NewServer(t)
	fake.SetNow(t0)
	c := crfake.NewClientBuilder().WithObjects(fake.Secret("shpyrd-system")).Build()
	src := fake.Source(c, "shpyrd-system")
	ctx := context.Background()
	first, err := src.Client(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key := types.NamespacedName{Namespace: "shpyrd-system", Name: github.SecretName}
	sec := &corev1.Secret{}
	if err := c.Get(ctx, key, sec); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, sec); err != nil {
		t.Fatal(err)
	}
	if again, _ := src.Client(ctx); again != first {
		t.Fatal("within 30 s the cached client stands")
	}
	src.Invalidate()
	if _, err := src.Client(ctx); !errors.Is(err, github.ErrNotConfigured) {
		t.Errorf("after Invalidate the deleted Secret is seen at once: %v", err)
	}
	var nilSource *github.Source
	nilSource.Invalidate() // no panic
}
