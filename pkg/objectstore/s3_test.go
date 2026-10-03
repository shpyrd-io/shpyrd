package objectstore

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestS3UsageIncludesEveryPageAndRejectsPartialMeasurements(t *testing.T) {
	for _, failSecondPage := range []bool{false, true} {
		t.Run(fmt.Sprint(failSecondPage), func(t *testing.T) {
			pages := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				pages++
				if r.URL.Query().Get("prefix") != "docker/" {
					t.Errorf("wrong prefix: %s", r.URL)
				}
				w.Header().Set("Content-Type", "application/xml")
				if pages == 1 {
					fmt.Fprint(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>next</NextContinuationToken><Contents><Key>docker/a</Key><Size>7</Size></Contents></ListBucketResult>`)
					return
				}
				if r.URL.Query().Get("continuation-token") != "next" {
					t.Error("listing did not continue from the previous page")
				}
				if failSecondPage {
					w.WriteHeader(403)
					fmt.Fprint(w, `<Error><Code>AccessDenied</Code><Message>denied</Message></Error>`)
					return
				}
				fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>docker/b</Key><Size>11</Size></Contents></ListBucketResult>`)
			}))
			defer srv.Close()
			s, err := NewS3(srv.URL, "test-region", "images", "docker", "key", "secret")
			if err != nil {
				t.Fatal(err)
			}
			u, err := s.Usage(context.Background())
			if failSecondPage {
				if err == nil || u.Bytes != 0 || u.Objects != 0 {
					t.Fatalf("partial listing looks successful: %+v, %v", u, err)
				}
			} else if err != nil || u.Bytes != 18 || u.Objects != 2 {
				t.Fatalf("usage = %+v, %v", u, err)
			}
			if pages != 2 {
				t.Errorf("read %d pages", pages)
			}
		})
	}
}

func TestS3ConfigurationRequiresBothCredentialsAndAnEndpointWithoutAPath(t *testing.T) {
	for _, endpoint := range []string{"invalid", "ftp://objects.test", "https://objects.test/path", "https://user:pass@objects.test", "https://objects.test?x=1"} {
		if _, err := NewS3(endpoint, "region", "bucket", "sources", "key", "secret"); err == nil {
			t.Errorf("accepted invalid endpoint %q", endpoint)
		}
	}
	if _, err := NewS3("", "us-east-1", "bucket", "sources", "key", ""); err == nil {
		t.Error("accepted an incomplete credential")
	}
	s, err := NewS3("", "us-east-1", "bucket", "sources", "key", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if s.Client.EndpointURL().String() != "https://s3.us-east-1.amazonaws.com" || s.Key("hash.tgz") != "sources/hash.tgz" {
		t.Fatal("wrong default endpoint or source prefix")
	}
}
