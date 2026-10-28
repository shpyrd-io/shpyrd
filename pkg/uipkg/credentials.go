package uipkg

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// A workspace's credential for a registry host, to pull private packages
// (RFC-0084): a Secret in the system namespace, labelled with the
// workspace, as Git tokens are (RFC-0017). Values are written and used,
// never returned.
const (
	// LabelCredential marks the Secret; its value is the kind.
	LabelCredential = "shpyrd.io/registry-credential"
	KindToken       = "token"
	// AnnotationHost is the registry host the credential is for.
	AnnotationHost = "shpyrd.io/registry-host"
	// AnnotationUpdatedBy is who set it last.
	AnnotationUpdatedBy = "shpyrd.io/updated-by"
	KeyUsername         = "username"
	KeyToken            = "token"
)

// Credential is a user name and a token for a registry; empty means
// anonymous.
type Credential struct {
	Username string
	Token    string
}

var notName = regexp.MustCompile(`[^a-z0-9]+`)

// SecretName is the Secret holding a workspace's credential for a host.
// A hash of the host keeps ghcr.io and ghcr-io apart.
func SecretName(workspace, host string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(host)))
	slug := strings.Trim(notName.ReplaceAllString(strings.ToLower(host), "-"), "-")
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	return "shpyrd-registry-token-" + workspace + "-" + slug + "-" + hex.EncodeToString(sum[:4])
}

// HostRe is what a registry host looks like: a name, with a port when it
// has one, as it appears in a reference.
var HostRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?(:[0-9]{1,5})?$`)
