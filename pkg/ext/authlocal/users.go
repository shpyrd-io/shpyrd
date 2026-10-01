package authlocal

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"net/mail"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Local accounts are Dex "Password" objects (its Kubernetes storage): email,
// bcrypt hash, display name and a stable user id. Dex renders the login
// form and checks the password; shpyrd only manages the objects, so no Dex
// API client is needed and users survive Dex restarts and upgrades.

// PasswordGVR is Dex's password resource.
var PasswordGVR = schema.GroupVersionResource{Group: "dex.coreos.com", Version: "v1", Resource: "passwords"}

// MinPasswordLength is enforced on create and change.
const MinPasswordLength = 8

// ErrNotEnabled says Dex (and its CRDs) are not installed.
var ErrNotEnabled = errors.New("local accounts are not enabled: run `shpyrd extensions enable auth-local`")

// User is a local account as shown to admins.
type User struct {
	Email     string    `json:"email"`
	Name      string    `json:"name,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	// Status is "active", "pending" (invited, no password yet) or "locked".
	Status string `json:"status,omitempty"`
	// Verified reports whether the email address has been confirmed.
	Verified bool `json:"verified"`
}

// Account status values.
const (
	StatusActive  = "active"
	StatusPending = "pending" // invited; no usable password
	StatusLocked  = "locked"  // locked after repeated failures
)

// Annotation keys on Password objects.
const (
	AnnotationInvited  = "shpyrd.io/invited"  // "true" = pending invitation
	AnnotationVerified = "shpyrd.io/verified" // "true" = email verified
	AnnotationLocked   = "shpyrd.io/locked"   // RFC3339 lock expiry or ""
	AnnotationFailures = "shpyrd.io/failures" // JSON: count + window start
)

// nameEncoding is the alphabet Dex uses to turn ids into object names.
var nameEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567")

// PasswordName is the object name Dex expects for an email: its kubernetes
// storage appends the FNV-64 offset basis to the lower-cased id and base32
// encodes it (see dex/storage/kubernetes/client.go idToName).
func PasswordName(email string) string {
	sum := fnv.New64().Sum([]byte(strings.ToLower(email)))
	return strings.TrimRight(nameEncoding.EncodeToString(sum), "=")
}

// NormalizeEmail validates and lower-cases an address.
func NormalizeEmail(email string) (string, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", fmt.Errorf("invalid email address %q", email)
	}
	return email, nil
}

// CheckPassword applies the password policy.
func CheckPassword(pw string) error {
	if len(pw) < MinPasswordLength {
		return fmt.Errorf("password must have at least %d characters", MinPasswordLength)
	}
	return nil
}

// Store manages local accounts in a namespace.
type Store struct {
	Dynamic   dynamic.Interface
	Namespace string
}

func (s *Store) res() dynamic.ResourceInterface {
	return s.Dynamic.Resource(PasswordGVR).Namespace(s.Namespace)
}

// List returns the accounts sorted by email.
func (s *Store) List(ctx context.Context) ([]User, error) {
	list, err := s.res().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, wrap(err)
	}
	out := make([]User, 0, len(list.Items))
	for _, u := range list.Items {
		out = append(out, userOf(u))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

// userOf converts a Dex Password object to a User.
func userOf(u unstructured.Unstructured) User {
	email, _, _ := unstructured.NestedString(u.Object, "email")
	name, _, _ := unstructured.NestedString(u.Object, "username")
	ann := u.GetAnnotations()
	status := StatusActive
	if ann[AnnotationInvited] == "true" {
		status = StatusPending
	}
	if exp := ann[AnnotationLocked]; exp != "" {
		if t, err := time.Parse(time.RFC3339, exp); err == nil && time.Now().Before(t) {
			status = StatusLocked
		}
	}
	return User{
		Email:     email,
		Name:      name,
		CreatedAt: u.GetCreationTimestamp().Time,
		Status:    status,
		Verified:  ann[AnnotationVerified] == "true",
	}
}

// Create adds an account; the email must be new.
func (s *Store) Create(ctx context.Context, email, name, password string) (*User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return nil, err
	}
	if err := CheckPassword(password); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	idRaw := make([]byte, 16)
	if _, err := rand.Read(idRaw); err != nil {
		return nil, err
	}
	if name == "" {
		name = strings.SplitN(email, "@", 2)[0]
	}
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": PasswordGVR.Group + "/" + PasswordGVR.Version,
		"kind":       "Password",
		"metadata": map[string]interface{}{
			"name":      PasswordName(email),
			"namespace": s.Namespace,
			"labels":    map[string]interface{}{"app.kubernetes.io/managed-by": "shpyrd"},
		},
		"email":    email,
		"hash":     base64.StdEncoding.EncodeToString(hash), // Dex's []byte field: base64 on the wire
		"username": name,
		"userID":   hex.EncodeToString(idRaw),
	}}
	created, err := s.res().Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("user %s already exists", email)
		}
		return nil, wrap(err)
	}
	return &User{Email: email, Name: name, CreatedAt: created.GetCreationTimestamp().Time}, nil
}

// SetPassword changes an account's password.
func (s *Store) SetPassword(ctx context.Context, email, password string) error {
	email, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	if err := CheckPassword(password); err != nil {
		return err
	}
	obj, err := s.res().Get(ctx, PasswordName(email), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("user %s not found", email)
		}
		return wrap(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	obj.Object["hash"] = base64.StdEncoding.EncodeToString(hash)
	if _, err := s.res().Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
		return wrap(err)
	}
	return nil
}

// unusableHash is a bcrypt hash no plaintext will ever produce, used for
// pending accounts that have no password yet.
const unusableHash = "$2a$10$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// CreatePending creates an account in the "pending" state (invited): a
// Password object with an unusable hash. The person uses the invite link
// to set their own password. Implements ext.LocalAccountStore.
func (s *Store) CreatePending(ctx context.Context, email, name string) error {
	email, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	if name == "" {
		name = strings.SplitN(email, "@", 2)[0]
	}
	idRaw := make([]byte, 16)
	if _, err := rand.Read(idRaw); err != nil {
		return err
	}
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": PasswordGVR.Group + "/" + PasswordGVR.Version,
		"kind":       "Password",
		"metadata": map[string]interface{}{
			"name":      PasswordName(email),
			"namespace": s.Namespace,
			"labels":    map[string]interface{}{"app.kubernetes.io/managed-by": "shpyrd"},
			"annotations": map[string]interface{}{
				AnnotationInvited: "true",
			},
		},
		"email":    email,
		"hash":     base64.StdEncoding.EncodeToString([]byte(unusableHash)),
		"username": name,
		"userID":   hex.EncodeToString(idRaw),
	}}
	created, err := s.res().Create(ctx, obj, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		// Re-invite: mark existing account as pending again, keep its password.
		_, serr := s.setAnnotation(ctx, email, AnnotationInvited, "true")
		return serr
	}
	if err != nil {
		return wrap(err)
	}
	_ = created
	return nil
}

// MarkVerified records that the email address has been confirmed.
func (s *Store) MarkVerified(ctx context.Context, email string) error {
	_, err := s.setAnnotation(ctx, email, AnnotationVerified, "true")
	return err
}

// Lock locks an account until expiry; expiry="" clears the lock.
func (s *Store) Lock(ctx context.Context, email string, until time.Time) error {
	v := until.UTC().Format(time.RFC3339)
	if until.IsZero() {
		v = ""
	}
	_, err := s.setAnnotation(ctx, email, AnnotationLocked, v)
	return err
}

// IsLocked reports whether the account is currently locked.
func (s *Store) IsLocked(ctx context.Context, email string) (bool, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return false, err
	}
	obj, err := s.res().Get(ctx, PasswordName(email), metav1.GetOptions{})
	if err != nil {
		return false, wrap(err)
	}
	exp := obj.GetAnnotations()[AnnotationLocked]
	if exp == "" {
		return false, nil
	}
	t, err := time.Parse(time.RFC3339, exp)
	if err != nil {
		return false, nil
	}
	return time.Now().Before(t), nil
}

// Status is the account's state — active, pending or locked — and ""
// when the address has no local account.
func (s *Store) Status(ctx context.Context, email string) (string, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return "", err
	}
	obj, err := s.res().Get(ctx, PasswordName(email), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", wrap(err)
	}
	return userOf(*obj).Status, nil
}

// setAnnotation is a helper: read, patch one annotation, write back.
func (s *Store) setAnnotation(ctx context.Context, email, key, value string) (*User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return nil, err
	}
	obj, err := s.res().Get(ctx, PasswordName(email), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("user %s not found", email)
		}
		return nil, wrap(err)
	}
	ann := obj.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	if value == "" {
		delete(ann, key)
	} else {
		ann[key] = value
	}
	obj.SetAnnotations(ann)
	updated, err := s.res().Update(ctx, obj, metav1.UpdateOptions{})
	if err != nil {
		return nil, wrap(err)
	}
	u := userOf(*updated)
	return &u, nil
}

// ActivateFromInvite sets the password (clearing the pending state) and marks
// the email verified — called when the invite link is used.
func (s *Store) ActivateFromInvite(ctx context.Context, email, password string) error {
	email, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	if err := CheckPassword(password); err != nil {
		return err
	}
	obj, err := s.res().Get(ctx, PasswordName(email), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("user %s not found", email)
		}
		return wrap(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	obj.Object["hash"] = base64.StdEncoding.EncodeToString(hash)
	ann := obj.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	delete(ann, AnnotationInvited)
	ann[AnnotationVerified] = "true"
	obj.SetAnnotations(ann)
	if _, err := s.res().Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
		return wrap(err)
	}
	return nil
}

// SetPasswordAndVerify updates the password and marks the email verified
// (called on reset completion).
func (s *Store) SetPasswordAndVerify(ctx context.Context, email, password string) error {
	email, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	if err := CheckPassword(password); err != nil {
		return err
	}
	obj, err := s.res().Get(ctx, PasswordName(email), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("user %s not found", email)
		}
		return wrap(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	obj.Object["hash"] = base64.StdEncoding.EncodeToString(hash)
	ann := obj.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	ann[AnnotationVerified] = "true"
	delete(ann, AnnotationLocked) // unlock on successful reset
	obj.SetAnnotations(ann)
	if _, err := s.res().Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
		return wrap(err)
	}
	return nil
}

// Delete removes an account.
func (s *Store) Delete(ctx context.Context, email string) error {
	email, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	if err := s.res().Delete(ctx, PasswordName(email), metav1.DeleteOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("user %s not found", email)
		}
		return wrap(err)
	}
	return nil
}

// wrap turns a missing CRD into ErrNotEnabled.
func wrap(err error) error {
	if meta.IsNoMatchError(err) || apierrors.IsNotFound(err) && strings.Contains(err.Error(), "the server could not find the requested resource") {
		return ErrNotEnabled
	}
	return err
}
