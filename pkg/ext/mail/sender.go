package mail

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// Sender delivers messages over SMTP with the settings in the Secret,
// read again at most every settingsTTL so `shpyrd-ctl mail set` takes
// effect without a restart. It implements ext.Mailer.
type Sender struct {
	Store *Store
	// Timeout bounds one delivery, connection included.
	Timeout time.Duration
	// PerRecipient and Window rate limit deliveries to one address: a
	// re-invite loop or a bug must not flood someone's inbox.
	PerRecipient int
	Window       time.Duration

	mu      sync.Mutex
	cached  *Settings
	fetched time.Time
	recent  map[string][]time.Time
	now     func() time.Time
}

const settingsTTL = 30 * time.Second

// ErrNotConfigured says no settings exist yet.
var ErrNotConfigured = errors.New("mail is not configured: run `shpyrd-ctl mail set`")

// ErrRateLimited says the recipient got enough messages for now.
var ErrRateLimited = errors.New("too many messages to this address recently; try again later")

// NewSender returns a Sender over the settings Secret.
func NewSender(store *Store) *Sender {
	return &Sender{Store: store, Timeout: 30 * time.Second, PerRecipient: 5, Window: 10 * time.Minute}
}

func (s *Sender) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// settings returns the current settings, cached briefly; nil when none.
func (s *Sender) settings(ctx context.Context) (*Settings, error) {
	s.mu.Lock()
	if s.cached != nil && s.clock().Sub(s.fetched) < settingsTTL {
		defer s.mu.Unlock()
		return s.cached, nil
	}
	s.mu.Unlock()
	if s.Store == nil || s.Store.Kube == nil {
		return nil, nil
	}
	cfg, err := s.Store.Get(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cached, s.fetched = cfg, s.clock()
	s.mu.Unlock()
	return cfg, nil
}

// Forget drops the cached settings (after `mail set`, in the same process).
func (s *Sender) Forget() {
	s.mu.Lock()
	s.cached = nil
	s.mu.Unlock()
}

// Configured reports whether settings exist.
func (s *Sender) Configured(ctx context.Context) bool {
	cfg, err := s.settings(ctx)
	return err == nil && cfg != nil
}

// Status is the settings without the secret.
func (s *Sender) Status(ctx context.Context) (Status, error) {
	cfg, err := s.settings(ctx)
	if err != nil {
		return Status{}, err
	}
	return cfg.status(), nil
}

// allow applies the per-recipient rate limit.
func (s *Sender) allow(to string) bool {
	if s.PerRecipient <= 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	if s.recent == nil {
		s.recent = map[string][]time.Time{}
	}
	kept := s.recent[to][:0]
	for _, t := range s.recent[to] {
		if now.Sub(t) < s.Window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= s.PerRecipient {
		s.recent[to] = kept
		return false
	}
	s.recent[to] = append(kept, now)
	return true
}

// Send delivers one message.
func (s *Sender) Send(ctx context.Context, m ext.Message) error {
	cfg, err := s.settings(ctx)
	if err != nil {
		return err
	}
	if cfg == nil {
		return ErrNotConfigured
	}
	if len(m.To) == 0 {
		return errors.New("no recipient")
	}
	var to []string
	for _, addr := range m.To {
		a, err := mail.ParseAddress(strings.TrimSpace(addr))
		if err != nil {
			return fmt.Errorf("recipient %q: %w", addr, err)
		}
		if !s.allow(strings.ToLower(a.Address)) {
			return ErrRateLimited
		}
		to = append(to, a.Address)
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return fmt.Errorf("from: %w", err)
	}
	body, err := s.build(cfg, from, to, m)
	if err != nil {
		return err
	}
	return s.deliver(ctx, cfg, from.Address, to, body)
}

// deliver runs the SMTP conversation.
func (s *Sender) deliver(ctx context.Context, cfg *Settings, from string, to []string, body []byte) error {
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", cfg.Addr())
	if err != nil {
		return fmt.Errorf("connect to %s: %w", cfg.Addr(), err)
	}
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	tlsCfg := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}
	if cfg.Security == SecurityTLS {
		tc := tls.Client(conn, tlsCfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			conn.Close()
			return fmt.Errorf("tls to %s: %w", cfg.Addr(), err)
		}
		conn = tc
	}
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp %s: %w", cfg.Addr(), err)
	}
	defer c.Close()
	if err := c.Hello(heloName()); err != nil {
		return fmt.Errorf("smtp hello: %w", err)
	}
	if cfg.Security == SecurityStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("%s does not offer STARTTLS; use --tls on the TLS port or --plain for a trusted relay", cfg.Addr())
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if cfg.User != "" {
		ok, mechanisms := c.Extension("AUTH")
		if !ok {
			return fmt.Errorf("%s does not offer authentication, yet a user is configured", cfg.Addr())
		}
		var auth smtp.Auth
		switch {
		case strings.Contains(mechanisms, "PLAIN"):
			auth = smtp.PlainAuth("", cfg.User, cfg.Password, cfg.Host)
		case strings.Contains(mechanisms, "LOGIN"):
			auth = &loginAuth{user: cfg.User, password: cfg.Password, host: cfg.Host}
		default:
			return fmt.Errorf("%s offers no supported authentication (%s); PLAIN or LOGIN are needed", cfg.Addr(), mechanisms)
		}
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("authentication failed for %s: %w", cfg.User, err)
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("sender %s refused: %w", from, err)
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("recipient %s refused: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("message refused: %w", err)
	}
	return c.Quit()
}

func heloName() string {
	h, err := hostnameFn()
	if err != nil || h == "" {
		return "shpyrd"
	}
	return h
}

// build renders the message: text always; text and HTML as alternatives
// when HTML is given. Bodies are quoted-printable so long lines and
// non-ASCII survive every relay.
func (s *Sender) build(cfg *Settings, from *mail.Address, to []string, m ext.Message) ([]byte, error) {
	var b strings.Builder
	id := make([]byte, 12)
	_, _ = rand.Read(id)
	domain := from.Address[strings.LastIndex(from.Address, "@")+1:]
	fmt.Fprintf(&b, "From: %s\r\n", from.String())
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", strings.ReplaceAll(strings.ReplaceAll(m.Subject, "\r", " "), "\n", " ")))
	fmt.Fprintf(&b, "Date: %s\r\n", s.clock().UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@%s>\r\n", hex.EncodeToString(id), domain)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("X-Mailer: shpyrd\r\n")
	b.WriteString("Auto-Submitted: auto-generated\r\n")
	text := m.Text
	if text == "" {
		text = "(this message has no text version)"
	}
	if m.HTML == "" {
		b.WriteString("Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
		if err := writeQP(&b, text); err != nil {
			return nil, err
		}
		return []byte(b.String()), nil
	}
	boundary := "=_shpyrd_" + hex.EncodeToString(id)
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", boundary)
	if err := writeQP(&b, text); err != nil {
		return nil, err
	}
	fmt.Fprintf(&b, "\r\n--%s\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", boundary)
	if err := writeQP(&b, m.HTML); err != nil {
		return nil, err
	}
	fmt.Fprintf(&b, "\r\n--%s--\r\n", boundary)
	return []byte(b.String()), nil
}

func writeQP(w io.Writer, s string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(strings.ReplaceAll(s, "\r\n", "\n"))); err != nil {
		return err
	}
	return qp.Close()
}

// loginAuth is the LOGIN mechanism (Office 365 and some relays offer
// nothing else). Like PLAIN it refuses to send the password unencrypted.
type loginAuth struct{ user, password, host string }

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS && !isLocalhost(server.Name) {
		return "", nil, errors.New("unencrypted connection")
	}
	if server.Name != a.host {
		return "", nil, errors.New("wrong host name")
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(fromServer))) {
	case "username:":
		return []byte(a.user), nil
	case "password:":
		return []byte(a.password), nil
	}
	return nil, fmt.Errorf("unexpected server challenge %q", fromServer)
}

func isLocalhost(name string) bool {
	return name == "localhost" || name == "127.0.0.1" || name == "::1"
}
