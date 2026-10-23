package gitauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// DeployKey is a key pair shpyrd generates for a project: the private half
// stays in a Secret, the public half is added to the repository.
type DeployKey struct {
	PrivatePEM  []byte
	Public      string // authorized_keys form, with the comment
	Fingerprint string // SHA256:...
}

// GenerateDeployKey makes an ed25519 key pair.
func GenerateDeployKey(comment string) (DeployKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return DeployKey{}, err
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return DeployKey{}, err
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		return DeployKey{}, err
	}
	return DeployKey{
		PrivatePEM:  pem.EncodeToMemory(block),
		Public:      strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))) + " " + comment,
		Fingerprint: ssh.FingerprintSHA256(sp),
	}, nil
}

var errScanned = errors.New("host key recorded")

// ScanHostKey connects to host's SSH port, records the key it presents and
// hangs up before logging in. The line is in known_hosts form.
func ScanHostKey(ctx context.Context, host, port string) (string, string, error) {
	if port == "" {
		port = "22"
	}
	addr := net.JoinHostPort(host, port)
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", "", fmt.Errorf("could not reach %s to record its SSH host key: %v", addr, err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	}
	var key ssh.PublicKey
	cfg := &ssh.ClientConfig{
		User: "git",
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			key = k
			return errScanned
		},
	}
	_, _, _, err = ssh.NewClientConn(conn, addr, cfg)
	if key == nil {
		return "", "", fmt.Errorf("could not reach %s to record its SSH host key: %v", addr, err)
	}
	return knownhosts.Line([]string{knownhosts.Normalize(addr)}, key), ssh.FingerprintSHA256(key), nil
}
