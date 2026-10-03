package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/shpyrd-io/shpyrd/pkg/install"
)

func TestRegistryReportsBucketUsageInsteadOfTheEmptyVolume(t *testing.T) {
	requests := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/images/" || r.URL.Query().Get("prefix") != "docker/" {
			t.Errorf("wrong bucket or prefix: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>images</Name><IsTruncated>false</IsTruncated><Contents><Key>docker/blob1</Key><Size>1024</Size></Contents><Contents><Key>docker/blob2</Key><Size>2048</Size></Contents></ListBucketResult>`)
	}))
	defer remote.Close()
	s, _ := newTestServer(t, nil, nil, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: install.RegistryS3SecretName, Namespace: "shpyrd-system"}, Data: map[string][]byte{"AWS_ACCESS_KEY_ID": []byte("key"), "AWS_SECRET_ACCESS_KEY": []byte("secret")}})
	s.opts.Vars = func(k string) string {
		return map[string]string{install.VarRegistryBucket: "images", install.VarRegistryEndpoint: remote.URL, install.VarRegistryRegion: "test-region", install.VarRegistrySize: "50Gi"}[k]
	}
	for i := 0; i < 2; i++ {
		st := s.registryStorage(context.Background())
		if st.Backend != "s3" || st.Bucket != "images" || st.UsedBytes != 3072 || st.CapacityBytes != 0 || st.Size != "" || st.Error != "" || st.MeasuredAt == nil {
			t.Fatalf("wrong bucket storage: %+v", st)
		}
	}
	if requests != 1 {
		t.Errorf("uncached bucket listing: %d requests", requests)
	}
}

func TestMissingBucketCredentialsAreNotReportedAsZeroUsage(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)
	s.opts.Vars = func(k string) string {
		if k == install.VarRegistryBucket {
			return "images"
		}
		return ""
	}
	st := s.registryStorage(context.Background())
	if st.Error == "" || st.MeasuredAt != nil {
		t.Fatalf("missing credentials look like an empty bucket: %+v", st)
	}
}
