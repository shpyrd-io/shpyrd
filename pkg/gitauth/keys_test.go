package gitauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestGenerateDeployKey(t *testing.T) {
	k, err := GenerateDeployKey("shpyrd shop")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(k.Public, "ssh-ed25519 ") || !strings.HasSuffix(k.Public, " shpyrd shop") {
		t.Errorf("public = %q", k.Public)
	}
	if !strings.HasPrefix(k.Fingerprint, "SHA256:") {
		t.Errorf("fingerprint = %q", k.Fingerprint)
	}
	signer, err := ssh.ParsePrivateKey(k.PrivatePEM)
	if err != nil {
		t.Fatalf("private key does not parse: %v", err)
	}
	if ssh.FingerprintSHA256(signer.PublicKey()) != k.Fingerprint {
		t.Error("private and public halves differ")
	}
}

// An SSH server on localhost: ScanHostKey records its key without logging in.
func TestScanHostKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{NoClientAuth: false, PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil }}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _, _, _, _ = ssh.NewServerConn(c, cfg); c.Close() }()
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	line, fp, err := ScanHostKey(ctx, host, port)
	if err != nil {
		t.Fatal(err)
	}
	if fp != ssh.FingerprintSHA256(signer.PublicKey()) {
		t.Errorf("fingerprint %s, want %s", fp, ssh.FingerprintSHA256(signer.PublicKey()))
	}
	if !strings.HasPrefix(line, "[127.0.0.1]:"+port+" ssh-ed25519 ") {
		t.Errorf("known_hosts line = %q", line)
	}
}

func TestScanHostKeyUnreachable(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()
	if _, _, err := ScanHostKey(context.Background(), "127.0.0.1", port); err == nil || !strings.Contains(err.Error(), "could not reach") {
		t.Errorf("err = %v", err)
	}
}
