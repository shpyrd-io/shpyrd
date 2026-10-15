package controller

import (
	"context"
	"net/url"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// Rotation of a dedicated database's credentials: the Postgres carries the
// stamp (shpyrd.io/rotate, the API's), and when the CloudNativePG app
// Secret does not carry it yet, a new password goes into the Secret's
// password and every key derived from it; the operator applies the Secret's
// password to the role (within seconds, seen on dev) and the Binder, which
// reads the Secret, re-renders the apps. The Secret records the stamp.
func (r *PostgresReconciler) rotateDedicated(ctx context.Context, pg *shpyrdv1.Postgres) error {
	stamp := pg.Annotations[AnnotationRotate]
	if stamp == "" {
		return nil
	}
	sec := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: pg.Namespace, Name: PostgresSecretName(pg.Name)}, sec); err != nil {
		return client.IgnoreNotFound(err)
	}
	if sec.Annotations[annotationRotated] == stamp {
		return nil
	}
	before := sec.DeepCopy()
	password := randomPassword()
	rotateSecretData(sec.Data, string(sec.Data["password"]), password)
	sec.Annotations = mergeMaps(sec.Annotations, map[string]string{annotationRotated: stamp})
	if err := r.Patch(ctx, sec, client.MergeFrom(before)); err != nil {
		return err
	}
	r.Recorder.Event(pg, corev1.EventTypeNormal, "Rotated", "new credentials written; the apps bound to the database restart with them")
	return nil
}

// rotateSecretData puts the new password in every key that carries the
// old one: the password itself, the URIs (the user info), the JDBC URIs
// (their password parameter) and the pgpass line (its last field).
func rotateSecretData(data map[string][]byte, old, password string) {
	for key, value := range data {
		s := string(value)
		switch {
		case key == "password":
			s = password
		case strings.HasPrefix(s, "postgresql://") || strings.HasPrefix(s, "postgres://"):
			if u, err := url.Parse(s); err == nil && u.User != nil {
				u.User = url.UserPassword(u.User.Username(), password)
				s = u.String()
			}
		case strings.HasPrefix(s, "jdbc:postgresql://"):
			if i := strings.Index(s, "?"); i >= 0 {
				q, err := url.ParseQuery(s[i+1:])
				if err == nil && q.Has("password") {
					q.Set("password", password)
					s = s[:i+1] + q.Encode()
				}
			}
		case key == "pgpass":
			if j := strings.LastIndex(strings.TrimRight(s, "\n"), ":"); j >= 0 && old != "" && strings.HasSuffix(strings.TrimRight(s, "\n"), ":"+old) {
				s = strings.TrimRight(s, "\n")[:j+1] + password + "\n"
			}
		}
		data[key] = []byte(s)
	}
}
