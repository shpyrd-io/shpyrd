// Package ids renders the platform's identifiers for places where a UUID's
// 36 characters and dashes do not fit or do not read: image repositories,
// hostnames, file names. The rendering is base58 (Bitcoin's alphabet: no
// 0/O/I/l, no dashes or underscores), 22 characters for a UUID, and
// reversible.
package ids

import (
	"errors"
	"math/big"
	"strings"

	"github.com/google/uuid"
)

const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

var base = big.NewInt(int64(len(alphabet)))

// Short is the base58 rendering of a UUID (its 16 bytes), padded so it is
// always 22 characters and sorts like the bytes. It panics on a string that
// is not a UUID: identifiers come from the store, never from users.
func Short(id string) string {
	u, err := uuid.Parse(id)
	if err != nil {
		panic("ids.Short: not a UUID: " + id)
	}
	return Encode(u[:])
}

// Encode renders bytes in base58.
func Encode(b []byte) string {
	n := new(big.Int).SetBytes(b)
	var out []byte
	mod := new(big.Int)
	for n.Sign() > 0 {
		n.DivMod(n, base, mod)
		out = append(out, alphabet[mod.Int64()])
	}
	// Leading zero bytes are '1's, as in every base58 in use; a UUID is
	// then padded to a fixed width so renderings line up and sort.
	for _, c := range b {
		if c != 0 {
			break
		}
		out = append(out, alphabet[0])
	}
	for len(out) < 22 && len(b) == 16 {
		out = append(out, alphabet[0])
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

// Decode is the inverse of Short: the UUID a rendering stands for.
func Decode(s string) (string, error) {
	if len(s) != 22 {
		return "", errors.New("not a short id")
	}
	n := new(big.Int)
	for _, r := range s {
		i := strings.IndexRune(alphabet, r)
		if i < 0 {
			return "", errors.New("not a short id")
		}
		n.Mul(n, base)
		n.Add(n, big.NewInt(int64(i)))
	}
	b := n.Bytes()
	if len(b) > 16 {
		return "", errors.New("not a short id")
	}
	var u uuid.UUID
	copy(u[16-len(b):], b)
	return u.String(), nil
}
