package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// RFC-0059: a renewed registry certificate restarts the registry; the
// first pass only records the running one.
func TestRegistryRotationRestartsOnRenewal(t *testing.T) {
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "registry-tls", Namespace: "shpyrd-system"}, Data: map[string][]byte{corev1.TLSCertKey: []byte("cert-1")}}
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "registry", Namespace: "shpyrd-system"}}
	c := fake.NewClientBuilder().WithObjects(sec, dep).Build()
	r := &RegistryRotation{Client: c, Namespace: "shpyrd-system", Secret: "registry-tls", Deploy: "registry"}
	ctx := context.Background()

	restarted, err := r.Check(ctx)
	if err != nil || restarted {
		t.Fatalf("first pass stamps only: restarted=%v err=%v", restarted, err)
	}
	_ = c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: "registry"}, dep)
	first := dep.Annotations[AnnotationTLSChecksum]
	if first == "" {
		t.Fatal("no checksum recorded")
	}
	// A pod template annotation is a rollout: the first pass leaves the
	// template alone, so a fresh install does not restart its registry.
	if dep.Spec.Template.Annotations[AnnotationTLSChecksum] != "" {
		t.Fatal("the first pass changed the pod template")
	}
	if restarted, err := r.Check(ctx); err != nil || restarted {
		t.Fatalf("same certificate: restarted=%v err=%v", restarted, err)
	}

	sec.Data[corev1.TLSCertKey] = []byte("cert-2")
	if err := c.Update(ctx, sec); err != nil {
		t.Fatal(err)
	}
	restarted, err = r.Check(ctx)
	if err != nil || !restarted {
		t.Fatalf("renewed certificate must restart: restarted=%v err=%v", restarted, err)
	}
	_ = c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: "registry"}, dep)
	if got := dep.Spec.Template.Annotations[AnnotationTLSChecksum]; got == "" || got == first || got != dep.Annotations[AnnotationTLSChecksum] {
		t.Errorf("the renewal stamps the pod template with the new checksum: template=%q deployment=%q first=%q", got, dep.Annotations[AnnotationTLSChecksum], first)
	}
	if restarted, err := r.Check(ctx); err != nil || restarted {
		t.Fatalf("after the restart: restarted=%v err=%v", restarted, err)
	}
}

// A registry stamped before the record moved to the Deployment carries the
// checksum on its pod template only; that record counts, so nothing
// restarts until the certificate changes.
func TestRegistryRotationReadsAnOlderStamp(t *testing.T) {
	sum := sha256.Sum256([]byte("cert-1"))
	stamp := hex.EncodeToString(sum[:8])
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "registry-tls", Namespace: "shpyrd-system"}, Data: map[string][]byte{corev1.TLSCertKey: []byte("cert-1")}}
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "registry", Namespace: "shpyrd-system"}}
	dep.Spec.Template.Annotations = map[string]string{AnnotationTLSChecksum: stamp}
	c := fake.NewClientBuilder().WithObjects(sec, dep).Build()
	r := &RegistryRotation{Client: c, Namespace: "shpyrd-system", Secret: "registry-tls", Deploy: "registry"}
	ctx := context.Background()
	if restarted, err := r.Check(ctx); err != nil || restarted {
		t.Fatalf("same certificate: restarted=%v err=%v", restarted, err)
	}
	sec.Data[corev1.TLSCertKey] = []byte("cert-2")
	if err := c.Update(ctx, sec); err != nil {
		t.Fatal(err)
	}
	if restarted, err := r.Check(ctx); err != nil || !restarted {
		t.Fatalf("renewed certificate must restart: restarted=%v err=%v", restarted, err)
	}
	_ = c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: "registry"}, dep)
	if dep.Spec.Template.Annotations[AnnotationTLSChecksum] == stamp {
		t.Error("checksum not updated")
	}
}
