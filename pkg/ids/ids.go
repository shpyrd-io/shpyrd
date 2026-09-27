// Package ids renders the platform's identifiers for places where a UUID's
// 36 characters and dashes do not fit: image repositories, hostnames, file
// names. The rendering is base36 — digits and lowercase letters, 25
// characters for a UUID, reversible. Lowercase because OCI repository
// names, DNS labels and most file systems demand or fold case (base58,
// the first choice, has uppercase letters and is refused by registries).
package ids

import (
	"errors"
	"math/big"
	"strings"

	"github.com/google/uuid"
)

const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// shortLen is the width of a rendered UUID: 36^25 > 2^128 > 36^24.
const shortLen = 25

var base = big.NewInt(int64(len(alphabet)))

// Short is the base36 rendering of a UUID (its 16 bytes), padded so it is
// always 25 characters and sorts like the bytes. It panics on a string that
// is not a UUID: identifiers come from the store, never from users.
func Short(id string) string {
	u, err := uuid.Parse(id)
	if err != nil {
		panic("ids.Short: not a UUID: " + id)
	}
	return Encode(u[:])
}

// Encode renders bytes in base36.
func Encode(b []byte) string {
	n := new(big.Int).SetBytes(b)
	var out []byte
	mod := new(big.Int)
	for n.Sign() > 0 {
		n.DivMod(n, base, mod)
		out = append(out, alphabet[mod.Int64()])
	}
	// A UUID is padded to a fixed width so renderings line up and sort.
	for len(out) < shortLen && len(b) == 16 {
		out = append(out, alphabet[0])
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

// Decode is the inverse of Short: the UUID a rendering stands for.
func Decode(s string) (string, error) {
	if len(s) != shortLen {
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
