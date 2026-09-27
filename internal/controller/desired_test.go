package controller

import "testing"

// Build pods fetch archives from the server's sources port; archives
// uploaded when the API port served them are pointed at it too, and any
// other URL is left alone.
func TestSourceURL(t *testing.T) {
	c := Config{SystemNamespace: "shpyrd-system"}
	for in, want := range map[string]string{
		"http://shpyrd-server.shpyrd-system.svc/api/sources/abc.tgz":               "http://shpyrd-server.shpyrd-system.svc:8082/api/sources/abc.tgz",
		"http://shpyrd-server.shpyrd-system.svc:80/api/sources/abc.tgz":            "http://shpyrd-server.shpyrd-system.svc:8082/api/sources/abc.tgz",
		"http://shpyrd-server.shpyrd-system.svc.cluster.local/api/sources/abc.tgz": "http://shpyrd-server.shpyrd-system.svc.cluster.local:8082/api/sources/abc.tgz",
		"http://shpyrd-server.shpyrd-system.svc:8082/api/sources/abc.tgz":          "http://shpyrd-server.shpyrd-system.svc:8082/api/sources/abc.tgz",
		"https://bucket.example.test/sources/abc.tgz":                              "https://bucket.example.test/sources/abc.tgz",
		"http://shpyrd-server.other.svc/api/sources/abc.tgz":                       "http://shpyrd-server.other.svc/api/sources/abc.tgz",
	} {
		if got := c.sourceURL(in); got != want {
			t.Errorf("sourceURL(%q) = %q, want %q", in, got, want)
		}
	}
}
