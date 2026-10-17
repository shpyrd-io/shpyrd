// Package tokenseal seals the secrets shpyrd keeps in its database (a
// person's GitHub refresh token, RFC-0054): AES-256-GCM, under a key in
// the cluster Secret shpyrd-token-key, which the server makes when it is
// absent. A sealed value names the row it belongs to (its additional
// data), so a value copied to another row does not open. A stolen database
// still needs the key; rotating the key means people link again.
package tokenseal

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// SecretName is the key's Secret in the system namespace.
	SecretName = "shpyrd-token-key"
	// KeyName is the key's entry in it: 32 random bytes.
	KeyName = "key"
	keySize = 32
	prefix  = "v1."
)

// ErrCannotOpen: the value was sealed under another key, for another row,
// or is no sealed value.
var ErrCannotOpen = errors.New("the sealed value does not open with this key: it was sealed under another key, or for another row")

// Box seals and opens with one key.
type Box struct{ aead cipher.AEAD }

// New is a box of a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("the token key must be %d bytes, not %d", keySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Generate is a box of a fresh key: tests, and runs without a cluster.
func Generate() (*Box, error) {
	key, err := newKey()
	if err != nil {
		return nil, err
	}
	return New(key)
}

func newKey() ([]byte, error) {
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

// Row names what a value belongs to, as its additional data.
func Row(parts ...string) string { return strings.Join(parts, "\x00") }

// Seal encrypts plain for row: "v1." and base64 of the nonce and the
// ciphertext.
func (b *Box) Seal(plain, row string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return prefix + base64.RawStdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, []byte(plain), []byte(row))), nil
}

// Open decrypts a value sealed for row; ErrCannotOpen when it does not.
func (b *Box) Open(sealed, row string) (string, error) {
	raw, ok := strings.CutPrefix(sealed, prefix)
	if !ok {
		return "", ErrCannotOpen
	}
	data, err := base64.RawStdEncoding.DecodeString(raw)
	n := b.aead.NonceSize()
	if err != nil || len(data) < n {
		return "", ErrCannotOpen
	}
	plain, err := b.aead.Open(nil, data[:n], data[n:], []byte(row))
	if err != nil {
		return "", ErrCannotOpen
	}
	return string(plain), nil
}

// LoadOrCreate reads the key's Secret, making it first when it is absent.
// Another replica making it at the same time wins, and its key is read.
func LoadOrCreate(ctx context.Context, kube kubernetes.Interface, namespace string) (*Box, error) {
	sec, err := kube.CoreV1().Secrets(namespace).Get(ctx, SecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		key, kerr := newKey()
		if kerr != nil {
			return nil, kerr
		}
		sec = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: namespace, Labels: map[string]string{"app.kubernetes.io/managed-by": "shpyrd"}},
			Type:       corev1.SecretTypeOpaque,
			Data:       map[string][]byte{KeyName: key},
		}
		created, cerr := kube.CoreV1().Secrets(namespace).Create(ctx, sec, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(cerr) {
			return LoadOrCreate(ctx, kube, namespace)
		}
		if cerr != nil {
			return nil, fmt.Errorf("could not create the Secret %s: %w", SecretName, cerr)
		}
		sec = created
	} else if err != nil {
		return nil, fmt.Errorf("could not read the Secret %s: %w", SecretName, err)
	}
	box, err := New(sec.Data[KeyName])
	if err != nil {
		return nil, fmt.Errorf("the Secret %s: %w", SecretName, err)
	}
	return box, nil
}
