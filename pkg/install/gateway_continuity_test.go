package install

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGatewaySettingsFromCluster(t *testing.T) {
	ctx := context.Background()
	// A cluster on the gateway: the Secret names the bucket, endpoint, region.
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: GatewayBackendSecret, Namespace: "shpyrd-system"}, Data: map[string][]byte{
		"AWS_ACCESS_KEY_ID": []byte("k"), "AWS_SECRET_ACCESS_KEY": []byte("s"),
		VarGatewayBucket: []byte("shpyrd-prod-objects"), VarGatewayEndpoint: []byte("https://x.compat.objectstorage.us-ashburn-1.oraclecloud.com"), VarGatewayRegion: []byte("us-ashburn-1"),
	}}
	settings, err := GatewaySettingsFromCluster(ctx, fake.NewSimpleClientset(secret), "shpyrd-system")
	if err != nil || settings[VarGatewayBucket] != "shpyrd-prod-objects" || settings[VarGatewayRegion] != "us-ashburn-1" || !strings.HasPrefix(settings[VarGatewayEndpoint], "https://") {
		t.Errorf("settings = %v, %v", settings, err)
	}
	// A cluster without the gateway (Garage, or nothing yet): nothing to say.
	settings, err = GatewaySettingsFromCluster(ctx, fake.NewSimpleClientset(), "shpyrd-system")
	if err != nil || settings != nil {
		t.Errorf("no gateway = %v, %v", settings, err)
	}
	// The gateway runs but its Secret is gone: refuse, do not render Garage.
	gateway := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "object-storage", Namespace: "shpyrd-system"}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "gateway", Args: []string{"object-gateway"}}}}}}}
	if _, err := GatewaySettingsFromCluster(ctx, fake.NewSimpleClientset(gateway), "shpyrd-system"); err == nil || !strings.Contains(err.Error(), "object-storage-credentials-file") {
		t.Errorf("a gateway without its secret must be refused: %v", err)
	}
	// Garage's deployment under the same name: not the gateway.
	garage := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "object-storage", Namespace: "shpyrd-system"}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "garage", Args: []string{"server"}}}}}}}
	if settings, err := GatewaySettingsFromCluster(ctx, fake.NewSimpleClientset(garage), "shpyrd-system"); err != nil || settings != nil {
		t.Errorf("garage = %v, %v", settings, err)
	}
}
