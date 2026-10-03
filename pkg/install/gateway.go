package install

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"net/url"
	"path"
	"regexp"

	"github.com/shpyrd-io/shpyrd/pkg/objectgateway"
	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const GatewayBackendSecret = "object-gateway-backend"

// Bootstrap uses the provider credential only to create the small consumer
// descriptors. All runtime object traffic uses the S3 gateway thereafter.
func gatewayCredentialsHook(ctx context.Context, e *Engine, c *Component) error {
	secrets := e.kube.Kube.CoreV1().Secrets(c.Namespace)
	creds := e.opts.GatewayCredentials
	if creds == nil {
		existing, err := secrets.Get(ctx, GatewayBackendSecret, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("gateway needs --object-storage-credentials-file: %w", err)
		}
		creds = map[string]string{"AWS_ACCESS_KEY_ID": string(existing.Data["AWS_ACCESS_KEY_ID"]), "AWS_SECRET_ACCESS_KEY": string(existing.Data["AWS_SECRET_ACCESS_KEY"])}
	}
	store, err := objectstore.NewS3(e.vars[VarGatewayEndpoint], e.vars[VarGatewayRegion], e.vars[VarGatewayBucket], "", creds["AWS_ACCESS_KEY_ID"], creds["AWS_SECRET_ACCESS_KEY"])
	if err != nil {
		return err
	}
	records := &objectgateway.Records{Store: store}
	if err := records.CheckConditionalWrites(ctx); err != nil {
		return err
	}
	endpoint := "http://object-storage." + c.Namespace + ".svc:3900"
	for _, consumer := range []struct{ bucket, secret string }{{"registry", RegistryS3SecretName}, {"sources", "gateway-sources"}, {"platform-backups", "gateway-platform-backups"}} {
		if err := records.Ensure(ctx, objectstore.BucketSpec{Name: consumer.bucket}); err != nil {
			return err
		}
		cred, err := records.Credential(ctx, consumer.bucket, "", "")
		if err != nil {
			return err
		}
		data := map[string]string{"AWS_ACCESS_KEY_ID": cred.AccessKey, "AWS_SECRET_ACCESS_KEY": cred.SecretKey, "bucket": consumer.bucket, "endpoint": endpoint, "region": objectgateway.Region}
		if err := e.applyOpaqueSecret(ctx, c.Namespace, consumer.secret, data); err != nil {
			return err
		}
	}
	return e.applyOpaqueSecret(ctx, c.Namespace, GatewayBackendSecret, map[string]string{"AWS_ACCESS_KEY_ID": creds["AWS_ACCESS_KEY_ID"], "AWS_SECRET_ACCESS_KEY": creds["AWS_SECRET_ACCESS_KEY"], VarGatewayBucket: e.vars[VarGatewayBucket], VarGatewayEndpoint: e.vars[VarGatewayEndpoint], VarGatewayRegion: e.vars[VarGatewayRegion]})
}

// On the first upgrade, give existing App sources their capabilities before
// the new server starts enforcing them. A durable marker makes this migration
// resumable but prevents future reconciles from signing arbitrary new URLs.
func sourcesSigningHook(ctx context.Context, e *Engine, c *Component) error {
	secrets := e.kube.Kube.CoreV1().Secrets(c.Namespace)
	sec, err := secrets.Get(ctx, "sources-signing", metav1.GetOptions{})
	key := ""
	if err == nil {
		key = string(sec.Data["key"])
		if key != "" && string(sec.Data["migrated"]) == "true" {
			return nil
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	if key == "" {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return err
		}
		key = hex.EncodeToString(raw)
		if err := e.applyOpaqueSecret(ctx, c.Namespace, "sources-signing", map[string]string{"key": key}); err != nil {
			return err
		}
	}
	if e.kube.Dynamic != nil {
		resource := e.kube.Dynamic.Resource(schema.GroupVersionResource{Group: "shpyrd.io", Version: "v1alpha1", Resource: "apps"})
		next := ""
		for {
			apps, err := resource.List(ctx, metav1.ListOptions{Limit: 200, Continue: next})
			if apierrors.IsNotFound(err) {
				break
			}
			if err != nil {
				return err
			}
			for _, app := range apps.Items {
				raw, _, _ := unstructured.NestedString(app.Object, "spec", "source", "blob", "url")
				u, err := url.Parse(raw)
				if err != nil || u == nil {
					continue
				}
				if u.Hostname() != "shpyrd-server."+c.Namespace+".svc" && u.Hostname() != "shpyrd-server."+c.Namespace+".svc.cluster.local" {
					continue
				}
				name := path.Base(u.Path)
				if u.Path != "/api/sources/"+name || !regexp.MustCompile(`^[a-f0-9]{64}\.tgz$`).MatchString(name) {
					continue
				}
				q := u.Query()
				q.Set("token", objectstore.SourceCapability([]byte(key), name))
				u.RawQuery = q.Encode()
				patch, _ := json.Marshal(map[string]any{"metadata": map[string]string{"resourceVersion": app.GetResourceVersion()}, "spec": map[string]any{"source": map[string]any{"blob": map[string]string{"url": u.String()}}}})
				if _, err := resource.Namespace(app.GetNamespace()).Patch(ctx, app.GetName(), types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
					return err
				}
			}
			next = apps.GetContinue()
			if next == "" {
				break
			}
		}
	}
	return e.applyOpaqueSecret(ctx, c.Namespace, "sources-signing", map[string]string{"key": key, "migrated": "true"})
}
