package gitauth

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"syscall"
	"time"
)

// Where a listing may connect (RFC-0018). The URL is a customer's, so the
// server must not be made to call itself or the cloud's metadata service:
// loopback, unspecified, link-local and the metadata addresses are
// refused after the name resolves, for HTTP and SSH alike. Private ranges
// stay allowed: a self-hosted git server often lives there.

// ErrRefusedAddress: the git host resolves to an address shpyrd does not
// connect to.
var ErrRefusedAddress = errors.New("not an address shpyrd connects to")

// addressRefused names the host in a refusal.
type addressRefused struct{ host string }

func (e *addressRefused) Error() string        { return e.host + " is not an address shpyrd connects to" }
func (e *addressRefused) Is(target error) bool { return target == ErrRefusedAddress }

// allowLoopbackForTests lets the package's tests reach their servers on
// 127.0.0.1. Only a _test.go file sets it.
var allowLoopbackForTests atomic.Bool

var metadataV6 = net.ParseIP("fd00:ec2::254")

// refusedIP says shpyrd does not connect to ip.
func refusedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	switch {
	case ip.IsLoopback():
		return !allowLoopbackForTests.Load()
	case ip.IsUnspecified(), ip.IsLinkLocalUnicast(), ip.Equal(metadataV6):
		return true
	}
	return false
}

// dialControl refuses a connection to a refused address, once resolved.
func dialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return ErrRefusedAddress
	}
	if refusedIP(net.ParseIP(host)) {
		return ErrRefusedAddress
	}
	return nil
}

func newDialer() *net.Dialer {
	return &net.Dialer{Timeout: lsRemoteTimeout, Control: dialControl}
}

// refusal turns a dial refused by dialControl into the host's refusal.
func refusal(host string, err error) error {
	if errors.Is(err, ErrRefusedAddress) {
		return &addressRefused{host: host}
	}
	return nil
}

// maxRedirects is how many redirects a listing follows.
const maxRedirects = 3

var errRedirect = errors.New("the git host sent shpyrd to another address")

// checkRedirect follows a redirect only on the same host, and never from
// https to http.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return fmt.Errorf("%w: more than %d redirects", errRedirect, maxRedirects)
	}
	first, prev := via[0], via[len(via)-1]
	if req.URL.Host != first.URL.Host {
		return errRedirect
	}
	if (first.URL.Scheme == "https" || prev.URL.Scheme == "https") && req.URL.Scheme != "https" {
		return errRedirect
	}
	return nil
}

// SafeHTTPClient is an HTTP client for hosts a customer or a setting
// names: it dials only addresses shpyrd connects to, follows a redirect
// only on the same host and never from https to http, and gives up after
// timeout. The GitHub App's calls go through it too (RFC-0054).
func SafeHTTPClient(timeout time.Duration) *http.Client {
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           newDialer().DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{Timeout: timeout, Transport: tr, CheckRedirect: checkRedirect}
}

func newHTTPClient() *http.Client { return SafeHTTPClient(lsRemoteTimeout) }
