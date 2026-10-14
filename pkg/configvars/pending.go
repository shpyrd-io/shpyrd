package configvars

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const AnnotationDeployRequested = "shpyrd.io/config-deploy-requested"
const AnnotationPendingBase = "shpyrd.io/config-pending-base"

func PendingName(app *shpyrdv1.App) string { return app.EnvSecretName() + "-pending" }

// DeployToken changes only for a deployment request, not a resize, sleep,
// health update or saved config edit. Include source/image for direct CR users.
func DeployToken(app *shpyrdv1.App) string {
	b, _ := json.Marshal([]any{app.Spec.Image, app.Spec.Source, app.Spec.Build, app.Annotations[AnnotationDeployRequested], app.Annotations[shpyrdv1.AnnotationRestartedAt], app.Annotations[shpyrdv1.AnnotationRebuildAt]})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Read returns the saved configuration, preferring pending edits. Both kinds
// are kept in a Secret so kind changes are atomic and legacy values stay private.
func Read(ctx context.Context, c client.Client, app *shpyrdv1.App) (*corev1.Secret, bool, error) {
	sec := &corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: PendingName(app)}, sec)
	if err == nil {
		return sec, true, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, false, err
	}
	err = c.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: app.EnvSecretName()}, sec)
	if apierrors.IsNotFound(err) {
		return &corev1.Secret{}, false, nil
	}
	return sec, false, err
}
