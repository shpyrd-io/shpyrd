package install

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// GatewaySettingsFromCluster reads the object gateway's bucket, endpoint and
// region from the backend Secret a previous `cluster init` left, so a run
// without --object-storage-credentials-file keeps the gateway the cluster
// has (the credentials stay in that Secret; the hook reuses them). It
// returns nothing for a cluster without the gateway, and an error for a
// cluster whose object-storage Deployment runs the gateway while the Secret
// is gone: rendering Garage over the gateway is never what a re-run means.
func GatewaySettingsFromCluster(ctx context.Context, k kubernetes.Interface, namespace string) (map[string]string, error) {
	secret, err := k.CoreV1().Secrets(namespace).Get(ctx, GatewayBackendSecret, metav1.GetOptions{})
	switch {
	case err == nil:
		if bucket := string(secret.Data[VarGatewayBucket]); bucket != "" {
			settings := map[string]string{VarGatewayBucket: bucket}
			for _, key := range []string{VarGatewayEndpoint, VarGatewayRegion} {
				if v := string(secret.Data[key]); v != "" {
					settings[key] = v
				}
			}
			return settings, nil
		}
	case !apierrors.IsNotFound(err):
		return nil, fmt.Errorf("the object gateway's backend secret: %w", err)
	}
	deploy, err := k.AppsV1().Deployments(namespace).Get(ctx, "object-storage", metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("the object-storage deployment: %w", err)
	}
	for _, c := range deploy.Spec.Template.Spec.Containers {
		for _, arg := range c.Args {
			if arg == "object-gateway" {
				return nil, fmt.Errorf("this cluster's object storage is the gateway, but its backend secret %s/%s is gone: pass --object-storage-credentials-file (without the gateway's settings this run would put Garage back over the gateway)", namespace, GatewayBackendSecret)
			}
		}
	}
	return nil, nil
}
