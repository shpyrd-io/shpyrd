package tokenseal

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestASealedValueOpensWithItsKeyAndRowOnly(t *testing.T) {
	box, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	row := Row("github-user-link", "local", "p-1")
	sealed, err := box.Seal("ghr_secret", row)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "ghr_secret") || !strings.HasPrefix(sealed, "v1.") {
		t.Fatalf("sealed %q", sealed)
	}
	if got, err := box.Open(sealed, row); err != nil || got != "ghr_secret" {
		t.Fatalf("open: %q %v", got, err)
	}
	if again, _ := box.Seal("ghr_secret", row); again == sealed {
		t.Error("two seals of one value are alike: the nonce is not fresh")
	}
	if _, err := box.Open(sealed, Row("github-user-link", "local", "p-2")); !errors.Is(err, ErrCannotOpen) {
		t.Errorf("another row's value opened: %v", err)
	}
	other, _ := Generate()
	if _, err := other.Open(sealed, row); !errors.Is(err, ErrCannotOpen) {
		t.Errorf("a wrong key opened it: %v", err)
	}
	for _, bad := range []string{"", "v1.", "v1.!!!", "ghr_secret", sealed[:len(sealed)-4]} {
		if _, err := box.Open(bad, row); !errors.Is(err, ErrCannotOpen) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestAKeyIsThirtyTwoBytes(t *testing.T) {
	if _, err := New(make([]byte, 16)); err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Errorf("16 bytes: %v", err)
	}
	if _, err := New(make([]byte, 32)); err != nil {
		t.Errorf("32 bytes: %v", err)
	}
}

func TestTheKeyIsMadeOnceInTheCluster(t *testing.T) {
	ctx := context.Background()
	cs := k8sfake.NewSimpleClientset()
	box, err := LoadOrCreate(ctx, cs, "shpyrd-system")
	if err != nil {
		t.Fatal(err)
	}
	sec, err := cs.CoreV1().Secrets("shpyrd-system").Get(ctx, SecretName, metav1.GetOptions{})
	if err != nil || len(sec.Data[KeyName]) != 32 || sec.Labels["app.kubernetes.io/managed-by"] != "shpyrd" {
		t.Fatalf("secret %+v %v", sec, err)
	}
	sealed, _ := box.Seal("x", "r")
	again, err := LoadOrCreate(ctx, cs, "shpyrd-system")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := again.Open(sealed, "r"); err != nil || got != "x" {
		t.Errorf("the key read back opens nothing: %q %v", got, err)
	}
	if list, _ := cs.CoreV1().Secrets("shpyrd-system").List(ctx, metav1.ListOptions{}); len(list.Items) != 1 {
		t.Errorf("%d secrets", len(list.Items))
	}
}

func TestABrokenKeySecretIsAnError(t *testing.T) {
	cs := k8sfake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: "shpyrd-system"},
		Data: map[string][]byte{KeyName: []byte("short")}})
	if _, err := LoadOrCreate(context.Background(), cs, "shpyrd-system"); err == nil || !strings.Contains(err.Error(), SecretName) {
		t.Errorf("err = %v", err)
	}
}

func TestAConflictingCreateIsReadOnceNotRetriedForever(t *testing.T) {
	cs := k8sfake.NewSimpleClientset()
	gr := corev1.Resource("secrets")
	cs.PrependReactor("get", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(gr, SecretName)
	})
	cs.PrependReactor("create", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewAlreadyExists(gr, SecretName)
	})
	if _, err := LoadOrCreate(context.Background(), cs, "shpyrd-system"); err == nil {
		t.Error("no error")
	}
}

func TestTheVersionLabelIsAuthenticated(t *testing.T) {
	box, _ := Generate()
	sealed, _ := box.Seal("x", "r")
	if _, err := box.Open("v2."+strings.TrimPrefix(sealed, "v1."), "r"); !errors.Is(err, ErrCannotOpen) {
		t.Errorf("relabelled value opened: %v", err)
	}
}

func TestRowPartsAreUnambiguous(t *testing.T) {
	a, b := Row("a\x00b", "c"), Row("a", "b\x00c")
	if a == b {
		t.Fatal("two rows alike")
	}
	box, _ := Generate()
	sealed, _ := box.Seal("x", a)
	if _, err := box.Open(sealed, b); !errors.Is(err, ErrCannotOpen) {
		t.Errorf("another row's value opened: %v", err)
	}
}
