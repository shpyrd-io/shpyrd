package gitauth

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The test servers listen on 127.0.0.1, which production refuses.
func init() { allowLoopbackForTests = true }

func refuseLoopback(t *testing.T) {
	t.Helper()
	allowLoopbackForTests = false
	t.Cleanup(func() { allowLoopbackForTests = true })
}

func TestRefusedAddresses(t *testing.T) {
	refuseLoopback(t)
	for _, s := range []string{
		"127.0.0.1", "127.8.9.10", "::1", "0.0.0.0", "::", "169.254.169.254", "169.254.1.1",
		"fe80::1", "fd00:ec2::254", "::ffff:169.254.169.254", "::ffff:127.0.0.1", "::ffff:0.0.0.0",
	} {
		if !refusedIP(net.ParseIP(s)) {
			t.Errorf("%s must be refused", s)
		}
	}
	for _, s := range []string{"10.0.0.1", "172.16.4.2", "192.168.1.10", "fd00::1", "fc00::5", "140.82.112.3", "2606:50c0:8000::153"} {
		if refusedIP(net.ParseIP(s)) {
			t.Errorf("%s must be allowed", s)
		}
	}
}

func TestLsRemoteRefusesLoopback(t *testing.T) {
	var hit atomic.Bool // the handler runs on the server's goroutine
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit.Store(true) }))
	defer srv.Close()
	refuseLoopback(t)
	_, err := LsRemote(context.Background(), srv.URL+"/acme/shop", Build{})
	if !errors.Is(err, ErrRefusedAddress) || err.Error() != "127.0.0.1 is not an address shpyrd connects to" {
		t.Errorf("err = %v", err)
	}
	if hit.Load() {
		t.Error("the loopback server was reached")
	}
}

func TestLsRemoteSSHRefusesLoopback(t *testing.T) {
	key, _ := GenerateDeployKey("shpyrd shop")
	refuseLoopback(t)
	_, err := LsRemote(context.Background(), "ssh://git@127.0.0.1:2222/acme/shop.git", deployKeyBuild(t, "127.0.0.1 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl", key))
	if !errors.Is(err, ErrRefusedAddress) {
		t.Errorf("err = %v", err)
	}
}

func TestRedirects(t *testing.T) {
	req := func(raw string) *http.Request {
		u, _ := url.Parse(raw)
		return &http.Request{URL: u}
	}
	for name, tc := range map[string]struct {
		via  []string
		next string
		ok   bool
	}{
		"same host":        {[]string{"https://git.example.com/a"}, "https://git.example.com/b", true},
		"https to http":    {[]string{"https://git.example.com/a"}, "http://git.example.com/a", false},
		"another host":     {[]string{"https://git.example.com/a"}, "https://login.example.com/", false},
		"http to https":    {[]string{"http://git.example.com/a"}, "https://git.example.com/a", true},
		"three redirects":  {[]string{"https://h/1", "https://h/2", "https://h/3"}, "https://h/4", true},
		"four redirects":   {[]string{"https://h/1", "https://h/2", "https://h/3", "https://h/4"}, "https://h/5", false},
		"back to http mid": {[]string{"https://h/1", "https://h/2"}, "http://h/3", false},
	} {
		var via []*http.Request
		for _, v := range tc.via {
			via = append(via, req(v))
		}
		if err := checkRedirect(req(tc.next), via); (err == nil) != tc.ok {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestLsRemoteDoesNotFollowARedirectToAnotherHost(t *testing.T) {
	other := smartHTTP(t, func(*http.Request) { t.Error("the other host was reached") }, pkt("# service=git-upload-pack\n")+"0000"+refLines(sampleRefs...))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.RequestURI(), http.StatusFound)
	}))
	defer srv.Close()
	if _, err := LsRemote(context.Background(), srv.URL+"/acme/shop", Build{}); err == nil {
		t.Error("a redirect to another host must be refused")
	}
}

func TestLsRemoteHTTPStatusesInFixedWords(t *testing.T) {
	for code, want := range map[int]string{
		http.StatusNotFound:            "the repository was not found",
		http.StatusInternalServerError: "the git host answered with an error",
		http.StatusTeapot:              "the git host answered with an error",
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }))
		_, err := LsRemote(context.Background(), srv.URL+"/acme/shop", Build{})
		srv.Close()
		if err == nil || err.Error() != want || strings.Contains(err.Error(), "500") {
			t.Errorf("%d: err = %v, want %q", code, err, want)
		}
	}
}

func TestSafeHTTPClientRefusesLoopback(t *testing.T) {
	var hit atomic.Bool // the handler runs on the server's goroutine
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit.Store(true) }))
	defer srv.Close()
	refuseLoopback(t)
	_, err := SafeHTTPClient(time.Second).Get(srv.URL)
	if !errors.Is(err, ErrRefusedAddress) || hit.Load() {
		t.Errorf("err = %v, reached %v", err, hit.Load())
	}
}
