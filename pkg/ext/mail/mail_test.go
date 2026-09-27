package mail

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// fakeSMTP is enough of an SMTP server to receive one message: EHLO, AUTH
// PLAIN or LOGIN, MAIL, RCPT, DATA, QUIT. No TLS: the tests use the
// "none" security on 127.0.0.1, where Go's PLAIN auth allows it.
type fakeSMTP struct {
	ln       net.Listener
	mu       sync.Mutex
	messages []received
	user     string
	password string
	// mechanisms advertised: "PLAIN LOGIN" by default.
	mechanisms string
	rejectRcpt string
}

type received struct {
	from string
	to   []string
	data string
	auth string
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{ln: ln, user: "postmaster", password: "s3cret", mechanisms: "PLAIN LOGIN"}
	go f.serve()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeSMTP) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeSMTP) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *fakeSMTP) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
	w("220 fake ESMTP")
	var cur received
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			w("250-fake")
			w("250-AUTH " + f.mechanisms)
			w("250 8BITMIME")
		case strings.HasPrefix(cmd, "AUTH PLAIN"):
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(line[len("AUTH PLAIN"):]))
			parts := strings.Split(string(raw), "\x00")
			if len(parts) == 3 && parts[1] == f.user && parts[2] == f.password {
				cur.auth = "plain:" + parts[1]
				w("235 ok")
			} else {
				w("535 authentication failed")
			}
		case strings.HasPrefix(cmd, "AUTH LOGIN"):
			w("334 " + base64.StdEncoding.EncodeToString([]byte("Username:")))
			u, _ := r.ReadString('\n')
			w("334 " + base64.StdEncoding.EncodeToString([]byte("Password:")))
			p, _ := r.ReadString('\n')
			ud, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(u))
			pd, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(p))
			if string(ud) == f.user && string(pd) == f.password {
				cur.auth = "login:" + string(ud)
				w("235 ok")
			} else {
				w("535 authentication failed")
			}
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			cur.from = strings.Trim(strings.Fields(line[len("MAIL FROM:"):])[0], "<> ")
			w("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			to := strings.Trim(line[len("RCPT TO:"):], "<> ")
			if f.rejectRcpt != "" && to == f.rejectRcpt {
				w("550 no such user")
				continue
			}
			cur.to = append(cur.to, to)
			w("250 ok")
		case cmd == "DATA":
			w("354 go ahead")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			cur.data = b.String()
			f.mu.Lock()
			f.messages = append(f.messages, cur)
			f.mu.Unlock()
			cur = received{auth: cur.auth}
			w("250 queued")
		case cmd == "QUIT":
			w("221 bye")
			return
		case strings.HasPrefix(cmd, "RSET"):
			w("250 ok")
		default:
			w("502 not implemented")
		}
	}
}

func (f *fakeSMTP) last(t *testing.T) received {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.messages) == 0 {
		t.Fatal("no message received")
	}
	return f.messages[len(f.messages)-1]
}

func storeWith(s *Settings) *Store {
	cs := kubefake.NewSimpleClientset()
	st := &Store{Kube: cs, Namespace: "shpyrd-system"}
	if s != nil {
		_ = st.Set(context.Background(), *s)
	}
	return st
}

func TestSettingsValidate(t *testing.T) {
	cases := []struct {
		in   Settings
		want string // error substring, "" for ok
		port int
		sec  string
	}{
		{Settings{Host: "smtp.example.com", From: "noreply@example.com"}, "", 587, SecurityStartTLS},
		{Settings{Host: "smtp.example.com", From: "shpyrd <noreply@example.com>", Security: "tls"}, "", 465, SecurityTLS},
		{Settings{Host: "smtp.example.com", From: "noreply@example.com", Port: 2525, Security: "none"}, "", 2525, SecurityNone},
		{Settings{From: "noreply@example.com"}, "host is required", 0, ""},
		{Settings{Host: "smtp.example.com:587", From: "noreply@example.com"}, "without a port", 0, ""},
		{Settings{Host: "smtp.example.com"}, "from is required", 0, ""},
		{Settings{Host: "smtp.example.com", From: "not an address"}, "from:", 0, ""},
		{Settings{Host: "smtp.example.com", From: "noreply@example.com", Security: "ssl"}, "security must be", 0, ""},
		{Settings{Host: "smtp.example.com", From: "noreply@example.com", User: "u"}, "go together", 0, ""},
		{Settings{Host: "smtp.example.com", From: "noreply@example.com", Port: 70000}, "port must be", 0, ""},
	}
	for _, c := range cases {
		err := c.in.Validate()
		if c.want == "" {
			if err != nil || c.in.Port != c.port || c.in.Security != c.sec {
				t.Errorf("%+v: err=%v port=%d security=%s", c.in, err, c.in.Port, c.in.Security)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: err=%v, want %q", c.in, err, c.want)
		}
	}
}

func TestStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := storeWith(nil)
	if s, err := st.Get(ctx); err != nil || s != nil {
		t.Fatalf("empty: %+v %v", s, err)
	}
	if err := st.Set(ctx, Settings{Host: "smtp.example.com", From: "noreply@example.com", User: "u", Password: "p"}); err != nil {
		t.Fatal(err)
	}
	s, err := st.Get(ctx)
	if err != nil || s.Host != "smtp.example.com" || s.Port != 587 || s.Password != "p" || s.Security != SecurityStartTLS {
		t.Fatalf("get: %+v %v", s, err)
	}
	if err := st.Set(ctx, Settings{Host: "relay.internal", From: "noreply@example.com", Security: SecurityNone}); err != nil {
		t.Fatal(err)
	}
	if s, _ := st.Get(ctx); s.Host != "relay.internal" || s.User != "" || s.Password != "" || s.Port != 587 {
		t.Errorf("update: %+v", s)
	}
	sec, _ := st.Kube.CoreV1().Secrets("shpyrd-system").Get(ctx, SecretName, metav1.GetOptions{})
	if sec.Labels["shpyrd.io/extension"] != Name || sec.Type != corev1.SecretTypeOpaque {
		t.Errorf("secret: %+v", sec.ObjectMeta)
	}
	if cur, _ := st.Get(ctx); !cur.status().Configured || cur.status().Auth || cur.status().Host != "relay.internal" {
		t.Errorf("status: %+v", cur.status())
	}
	if err := st.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete(ctx); err != nil {
		t.Errorf("delete twice: %v", err)
	}
	if s, err := st.Get(ctx); err != nil || s != nil {
		t.Errorf("after delete: %+v %v", s, err)
	}
}

func TestSendPlainAuth(t *testing.T) {
	srv := newFakeSMTP(t)
	sender := NewSender(storeWith(&Settings{Host: "127.0.0.1", Port: srv.port(), From: "shpyrd <noreply@example.com>", User: "postmaster", Password: "s3cret", Security: SecurityNone}))
	ctx := context.Background()
	if !sender.Configured(ctx) {
		t.Fatal("configured")
	}
	msg := ext.Message{To: []string{"Ada <ada@example.test>"}, Subject: "Convite: você foi convidado", Text: "Hello Ada,\nthe link: https://x.example/invite/abc\n", HTML: "<p>Hello <b>Ada</b></p>"}
	if err := sender.Send(ctx, msg); err != nil {
		t.Fatalf("send: %v", err)
	}
	got := srv.last(t)
	if got.from != "noreply@example.com" || len(got.to) != 1 || got.to[0] != "ada@example.test" || got.auth != "plain:postmaster" {
		t.Errorf("envelope: %+v", got)
	}
	for _, want := range []string{
		"From: \"shpyrd\" <noreply@example.com>\r\n", "To: ada@example.test\r\n", "Subject: =?utf-8?q?", "MIME-Version: 1.0\r\n",
		"Content-Type: multipart/alternative; boundary=", "Content-Type: text/plain; charset=utf-8", "Content-Type: text/html; charset=utf-8",
		"Content-Transfer-Encoding: quoted-printable", "the link: https://x.example/invite/abc", "<p>Hello <b>Ada</b></p>", "Message-ID: <", "Auto-Submitted: auto-generated",
	} {
		if !strings.Contains(got.data, want) {
			t.Errorf("message lacks %q:\n%s", want, got.data)
		}
	}
	// Text only: a plain body.
	if err := sender.Send(ctx, ext.Message{To: []string{"bob@example.test"}, Subject: "plain", Text: "just text"}); err != nil {
		t.Fatal(err)
	}
	if got := srv.last(t); !strings.Contains(got.data, "Content-Type: text/plain; charset=utf-8\r\n") || strings.Contains(got.data, "multipart") {
		t.Errorf("plain message:\n%s", got.data)
	}
}

func TestSendLoginAuthAndFailures(t *testing.T) {
	srv := newFakeSMTP(t)
	srv.mechanisms = "LOGIN"
	srv.rejectRcpt = "nobody@example.test"
	st := storeWith(&Settings{Host: "127.0.0.1", Port: srv.port(), From: "noreply@example.com", User: "postmaster", Password: "s3cret", Security: SecurityNone})
	sender := NewSender(st)
	ctx := context.Background()
	if err := sender.Send(ctx, ext.Message{To: []string{"ada@example.test"}, Subject: "s", Text: "t"}); err != nil {
		t.Fatalf("login auth: %v", err)
	}
	if got := srv.last(t); got.auth != "login:postmaster" {
		t.Errorf("auth = %q", got.auth)
	}
	// A refused recipient is an error naming it.
	if err := sender.Send(ctx, ext.Message{To: []string{"nobody@example.test"}, Subject: "s", Text: "t"}); err == nil || !strings.Contains(err.Error(), "nobody@example.test") {
		t.Errorf("rejected recipient: %v", err)
	}
	// Wrong password: the error names the user, not the password.
	_ = st.Set(ctx, Settings{Host: "127.0.0.1", Port: srv.port(), From: "noreply@example.com", User: "postmaster", Password: "wrong", Security: SecurityNone})
	sender.Forget()
	if err := sender.Send(ctx, ext.Message{To: []string{"ada@example.test"}, Subject: "s", Text: "t"}); err == nil || !strings.Contains(err.Error(), "authentication failed for postmaster") || strings.Contains(err.Error(), "wrong") {
		t.Errorf("wrong password: %v", err)
	}
	// STARTTLS demanded from a server without it: a clear message.
	_ = st.Set(ctx, Settings{Host: "127.0.0.1", Port: srv.port(), From: "noreply@example.com", Security: SecurityStartTLS})
	sender.Forget()
	if err := sender.Send(ctx, ext.Message{To: []string{"ada@example.test"}, Subject: "s", Text: "t"}); err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("no starttls: %v", err)
	}
	// Nothing configured, no recipient, bad recipient.
	empty := NewSender(storeWith(nil))
	if empty.Configured(ctx) {
		t.Error("empty store is configured")
	}
	if err := empty.Send(ctx, ext.Message{To: []string{"a@b.c"}}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("not configured: %v", err)
	}
	if err := sender.Send(ctx, ext.Message{}); err == nil {
		t.Error("no recipient accepted")
	}
	if err := sender.Send(ctx, ext.Message{To: []string{"not an address"}}); err == nil {
		t.Error("bad recipient accepted")
	}
	// Unreachable server: the error names the address, within the timeout.
	_ = st.Set(ctx, Settings{Host: "127.0.0.1", Port: 9, From: "noreply@example.com", Security: SecurityNone})
	sender.Forget()
	sender.Timeout = 2 * time.Second
	if err := sender.Send(ctx, ext.Message{To: []string{"ada@example.test"}, Subject: "s", Text: "t"}); err == nil || !strings.Contains(err.Error(), "127.0.0.1:9") {
		t.Errorf("unreachable: %v", err)
	}
}

func TestRateLimit(t *testing.T) {
	srv := newFakeSMTP(t)
	sender := NewSender(storeWith(&Settings{Host: "127.0.0.1", Port: srv.port(), From: "noreply@example.com", Security: SecurityNone}))
	sender.PerRecipient, sender.Window = 2, time.Hour
	now := time.Now()
	sender.now = func() time.Time { return now }
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := sender.Send(ctx, ext.Message{To: []string{"Ada@example.test"}, Subject: "s", Text: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := sender.Send(ctx, ext.Message{To: []string{"ada@example.test"}, Subject: "s", Text: "t"}); !errors.Is(err, ErrRateLimited) {
		t.Errorf("third message: %v", err)
	}
	if err := sender.Send(ctx, ext.Message{To: []string{"bob@example.test"}, Subject: "s", Text: "t"}); err != nil {
		t.Errorf("another recipient: %v", err)
	}
	now = now.Add(2 * time.Hour)
	if err := sender.Send(ctx, ext.Message{To: []string{"ada@example.test"}, Subject: "s", Text: "t"}); err != nil {
		t.Errorf("after the window: %v", err)
	}
}
