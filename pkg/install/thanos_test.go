package install

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// Storage plan step 7: with the object gateway, Prometheus's Thanos sidecar
// uploads its blocks to the gateway bucket platform-metrics (kept 90 days);
// the monitoring hook turns the gateway's credential into the sidecar's
// store config, and the values add the sidecar. Without the gateway,
// nothing of it.
func TestThanosSidecarFollowsTheGateway(t *testing.T) {
	ctx := context.Background()
	cred := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: MetricsSecret, Namespace: "shpyrd-system"}, Data: map[string][]byte{
		"AWS_ACCESS_KEY_ID": []byte("GK1"), "AWS_SECRET_ACCESS_KEY": []byte("s3cr3t"), "bucket": []byte(MetricsBucket),
		"endpoint": []byte("http://object-storage.shpyrd-system.svc:3900"), "region": []byte("garage"),
	}}
	cs := fake.NewClientset(cred)
	e := fakeEngine(ctx, cs, map[string]string{VarSystemNS: "shpyrd-system", VarGatewayBucket: "shpyrd-prod-objects"})
	c := &Component{Name: MonitoringComponent, Namespace: "monitoring"}
	if err := thanosObjstoreHook(ctx, e, c); err != nil {
		t.Fatal(err)
	}
	sec, err := cs.CoreV1().Secrets("monitoring").Get(ctx, ThanosObjstoreSecret, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	conf := string(sec.Data["objstore.yml"])
	for _, want := range []string{"type: S3", `bucket: "platform-metrics"`, `endpoint: "object-storage.shpyrd-system.svc:3900"`, `region: "garage"`, `access_key: "GK1"`, `secret_key: "s3cr3t"`, "insecure: true", "bucket_lookup_type: path"} {
		if !strings.Contains(conf, want) {
			t.Errorf("objstore.yml lacks %q:\n%s", want, conf)
		}
	}

	// The values: the sidecar reads that Secret, blocks carry the cluster.
	vals := map[string]interface{}{}
	thanosSidecar(vals, true, "shpyrd.io")
	spec := table(table(vals, "prometheus"), "prometheusSpec")
	existing := table(table(table(spec, "thanos"), "objectStorageConfig"), "existingSecret")
	if existing["name"] != ThanosObjstoreSecret || existing["key"] != "objstore.yml" || table(spec, "externalLabels")["cluster"] != "shpyrd.io" {
		t.Errorf("values = %+v", vals)
	}

	// Without the gateway: no Secret, no sidecar.
	cs2 := fake.NewClientset()
	e2 := fakeEngine(ctx, cs2, map[string]string{VarSystemNS: "shpyrd-system"})
	if err := thanosObjstoreHook(ctx, e2, c); err != nil {
		t.Fatal(err)
	}
	if _, err := cs2.CoreV1().Secrets("monitoring").Get(ctx, ThanosObjstoreSecret, metav1.GetOptions{}); err == nil {
		t.Error("no gateway, yet the store Secret was written")
	}
	plain := map[string]interface{}{}
	thanosSidecar(plain, false, "shpyrd.io")
	if len(plain) != 0 {
		t.Errorf("no gateway, yet values were added: %+v", plain)
	}

	// The gateway's consumers include the bucket, kept 90 days.
	found := false
	for _, consumer := range gatewayConsumers(map[string]string{}) {
		if consumer.bucket == MetricsBucket {
			found = consumer.secret == MetricsSecret && consumer.retentionDays == 90
		}
	}
	if !found {
		t.Error("the gateway consumers lack the metrics bucket with its retention")
	}
}
