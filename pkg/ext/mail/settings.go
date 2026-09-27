// Package mail is the mail extension (RFC-0013): one SMTP sender the
// platform uses for invitations and, later, notifications. Its settings
// live in the Secret shpyrd-mail of the system namespace, written by
// `shpyrd-ctl mail set`; the password never leaves it.
package mail

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Name is the extension's identifier in SHPYRD_EXTENSIONS.
const Name = "mail"

// SecretName holds the sender's settings in the system namespace.
const SecretName = "shpyrd-mail"

// Security is how the connection to the SMTP server is protected.
const (
	SecurityStartTLS = "starttls" // plain connection upgraded with STARTTLS (port 587)
	SecurityTLS      = "tls"      // TLS from the first byte (port 465)
	SecurityNone     = "none"     // no encryption: only for a relay on localhost or a private network
)

// Settings is the sender's configuration.
type Settings struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user,omitempty"`
	Password string `json:"-"`
	// From is the sender address, optionally with a display name:
	// "shpyrd <noreply@example.com>".
	From     string `json:"from"`
	Security string `json:"security"`
}

// Validate checks the settings and fills the defaults (port, security).
func (s *Settings) Validate() error {
	s.Host = strings.TrimSpace(s.Host)
	s.User = strings.TrimSpace(s.User)
	s.From = strings.TrimSpace(s.From)
	s.Security = strings.ToLower(strings.TrimSpace(s.Security))
	if s.Host == "" {
		return errors.New("host is required")
	}
	if strings.ContainsAny(s.Host, " /:") {
		return errors.New("host is a host name, without a port or a scheme")
	}
	switch s.Security {
	case "":
		s.Security = SecurityStartTLS
	case SecurityStartTLS, SecurityTLS, SecurityNone:
	default:
		return fmt.Errorf("security must be %s, %s or %s", SecurityStartTLS, SecurityTLS, SecurityNone)
	}
	if s.Port == 0 {
		s.Port = 587
		if s.Security == SecurityTLS {
			s.Port = 465
		}
	}
	if s.Port < 1 || s.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if s.From == "" {
		return errors.New("from is required: the sender address, for example \"shpyrd <noreply@example.com>\"")
	}
	if _, err := mail.ParseAddress(s.From); err != nil {
		return fmt.Errorf("from: %w", err)
	}
	if (s.User == "") != (s.Password == "") {
		return errors.New("user and password go together")
	}
	return nil
}

// Addr is host:port.
func (s Settings) Addr() string { return s.Host + ":" + strconv.Itoa(s.Port) }

// Status is what the API and the CLI show: the settings without the
// secret, and whether credentials are set.
type Status struct {
	Configured bool   `json:"configured"`
	Host       string `json:"host,omitempty"`
	Port       int    `json:"port,omitempty"`
	From       string `json:"from,omitempty"`
	Security   string `json:"security,omitempty"`
	Auth       bool   `json:"auth"`
}

func (s *Settings) status() Status {
	if s == nil {
		return Status{}
	}
	return Status{Configured: true, Host: s.Host, Port: s.Port, From: s.From, Security: s.Security, Auth: s.User != ""}
}

// Store reads and writes the settings Secret.
type Store struct {
	Kube      kubernetes.Interface
	Namespace string
}

// Get returns the settings, or nil when none are configured.
func (st *Store) Get(ctx context.Context) (*Settings, error) {
	sec, err := st.Kube.CoreV1().Secrets(st.Namespace).Get(ctx, SecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s := fromSecret(sec)
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("secret %s: %w", SecretName, err)
	}
	return s, nil
}

func fromSecret(sec *corev1.Secret) *Settings {
	get := func(k string) string { return strings.TrimSpace(string(sec.Data[k])) }
	port, _ := strconv.Atoi(get("port"))
	return &Settings{Host: get("host"), Port: port, User: get("user"), Password: string(sec.Data["password"]), From: get("from"), Security: get("security")}
}

// Set creates or replaces the settings.
func (st *Store) Set(ctx context.Context, s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: st.Namespace, Labels: map[string]string{"app.kubernetes.io/managed-by": "shpyrd", "shpyrd.io/extension": Name}},
		Type:       corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"host": []byte(s.Host), "port": []byte(strconv.Itoa(s.Port)), "user": []byte(s.User), "password": []byte(s.Password),
			"from": []byte(s.From), "security": []byte(s.Security),
		},
	}
	secrets := st.Kube.CoreV1().Secrets(st.Namespace)
	if _, err := secrets.Create(ctx, sec, metav1.CreateOptions{}); err == nil {
		return nil
	} else if !apierrors.IsAlreadyExists(err) {
		return err
	}
	cur, err := secrets.Get(ctx, SecretName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	cur.Labels, cur.Data, cur.StringData = sec.Labels, sec.Data, nil
	_, err = secrets.Update(ctx, cur, metav1.UpdateOptions{})
	return err
}

// Delete removes the settings; nothing happens when there are none.
func (st *Store) Delete(ctx context.Context) error {
	err := st.Kube.CoreV1().Secrets(st.Namespace).Delete(ctx, SecretName, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
