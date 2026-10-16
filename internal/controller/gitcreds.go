package controller

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

// Private git repositories (RFC-0017): before a build, the project's
// credential is written to <app>-git in its namespace, where kpack (through
// the build ServiceAccount) and the BuildKit fetch container read it. The
// project's deploy key wins over the workspace's token for the host.

// reconcileGitCredentials writes or removes <app>-git and says how the
// build clones.
func (r *AppReconciler) reconcileGitCredentials(ctx context.Context, app *shpyrdv1.App) (gitauth.Build, error) {
	name := gitauth.BuildSecretName(app.Name)
	if app.Spec.Source == nil || app.Spec.Source.Git == nil {
		return gitauth.Build{}, r.deleteSecret(ctx, app.Namespace, name)
	}
	b, err := lookupGitBuild(ctx, r.Client, r.Config.SystemNamespace, app)
	if errors.Is(err, errGitURL) {
		return r.noGitCredential(ctx, app)
	}
	if err != nil {
		return gitauth.Build{}, err
	}
	if b.Secret == nil {
		return b, r.deleteSecret(ctx, app.Namespace, name)
	}
	want := b.Secret
	// Owned by the App: deleted with it.
	if err := controllerutil.SetControllerReference(app, want, r.Scheme); err != nil {
		return b, fmt.Errorf("git credentials: %w", err)
	}
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: want.Name, Namespace: want.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, sec, func() error {
		sec.Labels = mergeMaps(sec.Labels, want.Labels)
		sec.Annotations = mergeMaps(sec.Annotations, want.Annotations)
		sec.Type = want.Type
		sec.Data = want.Data
		return controllerutil.SetControllerReference(app, sec, r.Scheme)
	}); err != nil {
		if apierrors.IsInvalid(err) {
			// The Secret's type is immutable: a deploy key replacing a
			// token needs a new Secret.
			if derr := r.deleteSecret(ctx, app.Namespace, name); derr != nil {
				return b, derr
			}
			return b, r.Create(ctx, want)
		}
		return b, fmt.Errorf("git credentials: %w", err)
	}
	return b, nil
}

// noGitCredential is the fallback for a git URL shpyrd cannot parse: no
// credential applies and the build uses the raw URL, as it did before
// credentials existed. It is not a reconcile error.
// The URL is not logged: it may carry a user name or password.
func (r *AppReconciler) noGitCredential(ctx context.Context, app *shpyrdv1.App) (gitauth.Build, error) {
	log.FromContext(ctx).Info("its git URL is not one shpyrd can parse; building without credentials", "app", app.Namespace+"/"+app.Name)
	return gitauth.Build{}, r.deleteSecret(ctx, app.Namespace, gitauth.BuildSecretName(app.Name))
}

// ensureBuildServiceAccount keeps shpyrd-builder listing the registry
// Secret and <app>-git, whichever the project needs. With neither, an
// existing account is emptied rather than left pointing at a Secret that
// is gone.
func (r *AppReconciler) ensureBuildServiceAccount(ctx context.Context, app *shpyrdv1.App, git gitauth.Build) error {
	var secrets []corev1.ObjectReference
	var pulls []corev1.LocalObjectReference
	if r.Config.RegistrySecret != "" {
		secrets = append(secrets, corev1.ObjectReference{Name: r.Config.RegistrySecret})
		pulls = append(pulls, corev1.LocalObjectReference{Name: r.Config.RegistrySecret})
	}
	if git.Secret != nil {
		secrets = append(secrets, corev1.ObjectReference{Name: git.Secret.Name})
	}
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: BuildServiceAccount, Namespace: app.Namespace}}
	if len(secrets) == 0 {
		if err := r.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: BuildServiceAccount}, sa); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		if len(sa.Secrets) == 0 && len(sa.ImagePullSecrets) == 0 {
			return nil
		}
		sa.Secrets, sa.ImagePullSecrets = nil, nil
		return r.Update(ctx, sa)
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error {
		sa.Labels = mergeMaps(sa.Labels, map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", shpyrdv1.LabelProject: app.Name})
		sa.Secrets = secrets
		sa.ImagePullSecrets = pulls
		return nil
	}); err != nil {
		return fmt.Errorf("build service account: %w", err)
	}
	return nil
}

// errGitURL: the App's git URL is not one shpyrd can parse; it builds
// without credentials, as before credentials existed.
var errGitURL = errors.New("the git URL is not one shpyrd can parse")

// lookupGitBuild finds the credential a build of the App's repository
// clones with, and the source watcher lists with: the project's deploy key
// for the host, else the workspace's token for the host, else none.
func lookupGitBuild(ctx context.Context, c client.Reader, systemNS string, app *shpyrdv1.App) (gitauth.Build, error) {
	url := app.Spec.Source.Git.URL
	repo, err := gitauth.Parse(url)
	if err != nil {
		return gitauth.Build{}, errGitURL
	}
	deployKey, err := optionalSecret(ctx, c, app.Namespace, gitauth.DeployKeySecretName)
	if err != nil {
		return gitauth.Build{}, err
	}
	token, err := optionalSecret(ctx, c, systemNS, gitauth.TokenSecretName(workspaceOf(app), repo.Host))
	if err != nil {
		return gitauth.Build{}, err
	}
	b, err := gitauth.Resolve(url, app.Name, app.Namespace, deployKey, token)
	if err != nil {
		return gitauth.Build{}, errGitURL
	}
	return b, nil
}

func optionalSecret(ctx context.Context, c client.Reader, ns, name string) (*corev1.Secret, error) {
	sec := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, sec); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return sec, nil
}

// deleteSecret removes a Secret when the cache holds it: every reconcile
// of an app without a credential passes here, and must not send a DELETE
// each time.
func (r *AppReconciler) deleteSecret(ctx context.Context, ns, name string) error {
	sec := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, sec); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return r.deleteIfExists(ctx, sec)
}
