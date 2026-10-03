package objectstore

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// SourceCapability grants read access to one immutable archive. The key stays
// in the control plane; callers only receive the resulting object capability.
func SourceCapability(key []byte, name string) string {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte("shpyrd-source-v1/" + name))
	return hex.EncodeToString(h.Sum(nil))
}
