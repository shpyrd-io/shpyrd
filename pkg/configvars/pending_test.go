package configvars

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

func TestDeployTokenIgnoresAutoDeployAndFollowsThePin(t *testing.T) {
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}},
		Spec:       shpyrdv1.AppSpec{Source: &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://github.com/acme/shop", Revision: "main"}}},
	}
	before := DeployToken(app)
	off := false
	app.Spec.Source.Git.AutoDeploy = &off
	if DeployToken(app) != before {
		t.Error("switching auto-deploy is a setting, not a deploy")
	}
	app.Annotations[shpyrdv1.AnnotationGitCommit] = "0123456789abcdef0123456789abcdef01234567"
	app.Annotations[shpyrdv1.AnnotationGitCommitRef] = "main"
	if DeployToken(app) == before {
		t.Error("a new pinned commit is a deploy")
	}
}

func TestDeployTokenOfAnAppWithoutAPinIsUnchanged(t *testing.T) {
	for _, app := range []*shpyrdv1.App{
		{Spec: shpyrdv1.AppSpec{Image: "ghcr.io/acme/shop:1"}},
		{Spec: shpyrdv1.AppSpec{Source: &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://github.com/acme/shop", Revision: "a1b2c3d"}}}},
	} {
		// The formula before RFC-0018: a token that moved would apply
		// every saved config edit on the upgrade.
		b, _ := json.Marshal([]any{app.Spec.Image, app.Spec.Source, app.Spec.Build, "", "", ""})
		h := sha256.Sum256(b)
		if got := DeployToken(app); got != hex.EncodeToString(h[:]) {
			t.Errorf("%+v: the token of an app with no pin changed", app.Spec)
		}
	}
}
