package api

import (
	"bufio"
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// preface is what a listener saw at the start of a connection.
type preface struct {
	line string
	addr string // the address the preface claims, "" when it claims none
}

// parsePreface reads the PROXY line the way nginx does: UNKNOWN keeps the
// address of the connection; TCP4/TCP6 carry a client address.
func parsePreface(line string) (addr string, ok bool) {
	if !strings.HasSuffix(line, "\r\n") || !strings.HasPrefix(line, "PROXY ") {
		return "", false
	}
	fields := strings.Fields(strings.TrimSuffix(line, "\r\n"))
	switch fields[1] {
	case "UNKNOWN":
		return "", true
	case "TCP4", "TCP6":
		if len(fields) != 6 {
			return "", false
		}
		return fields[2], true
	}
	return "", false
}

// prefaceListener is an HTTP listener (optionally TLS) that requires the
// PROXY line before anything else when require is set, and drops the
// connection when it gets one it does not expect, as a plain front door does.
type prefaceListener struct {
	URL      string
	Addr     string
	mu       sync.Mutex
	seen     []preface
	rejected atomic.Int32
	conns    atomic.Int32
}

func newPrefaceListener(t *testing.T, require, useTLS bool, h http.Handler) *prefaceListener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pl := &prefaceListener{Addr: ln.Addr().String()}
	inner := &chanListener{conns: make(chan net.Conn), done: make(chan struct{}), addr: ln.Addr()}
	srv := &http.Server{Handler: h}
	if useTLS {
		ts := httptest.NewUnstartedServer(h)
		ts.StartTLS()
		cert := ts.TLS.Certificates
		ts.Close()
		srv.TLSConfig = &tls.Config{Certificates: cert}
		go srv.ServeTLS(inner, "", "")
		pl.URL = "https://" + pl.Addr
	} else {
		go srv.Serve(inner)
		pl.URL = "http://" + pl.Addr
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			pl.conns.Add(1)
			go func() {
				br := bufio.NewReader(c)
				if require {
					c.SetReadDeadline(time.Now().Add(2 * time.Second))
					line, err := br.ReadString('\n')
					addr, ok := parsePreface(line)
					if err != nil || !ok {
						pl.rejected.Add(1)
						c.Close()
						return
					}
					c.SetReadDeadline(time.Time{})
					pl.mu.Lock()
					pl.seen = append(pl.seen, preface{line: line, addr: addr})
					pl.mu.Unlock()
				} else if b, _ := br.Peek(6); string(b) == "PROXY " {
					pl.rejected.Add(1) // a plain door reads the line as a broken request
					c.Close()
					return
				}
				select {
				case inner.conns <- &bufConn{Conn: c, r: br}:
				case <-inner.done:
					c.Close()
				}
			}()
		}
	}()
	t.Cleanup(func() { ln.Close(); close(inner.done); srv.Close() })
	return pl
}

func (pl *prefaceListener) prefaces() []preface {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return append([]preface(nil), pl.seen...)
}

type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufConn) Read(p []byte) (int, error) { return b.r.Read(p) }

type chanListener struct {
	conns chan net.Conn
	done  chan struct{}
	addr  net.Addr
}

func (l *chanListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *chanListener) Close() error   { return nil }
func (l *chanListener) Addr() net.Addr { return l.addr }

func maintenanceHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Shpyrd-Maintenance", "true")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusServiceUnavailable)
	})
}

func shortMaintenanceTimeout(t *testing.T) {
	old := maintenanceConfirmTimeout
	maintenanceConfirmTimeout = 3 * time.Second
	t.Cleanup(func() { maintenanceConfirmTimeout = old })
}

func TestMaintenanceCheckAnnouncesItselfToTheExternalFrontDoor(t *testing.T) {
	shortMaintenanceTimeout(t)
	app := &shpyrdv1.App{Spec: shpyrdv1.AppSpec{Exposure: "external"}, Status: shpyrdv1.AppStatus{URL: "http://site.example"}}

	// On: a front door that requires the preface answers, and the preface
	// claims no client address.
	door := newPrefaceListener(t, true, false, maintenanceHandler())
	s, _ := newTestServer(t, nil, nil)
	s.opts.ProxyProtocol = true
	s.opts.IngressServiceExternal = door.Addr
	if err := s.confirmProjectMaintenance(context.Background(), app); err != nil {
		t.Fatalf("with the option on: %v", err)
	}
	seen := door.prefaces()
	if len(seen) == 0 {
		t.Fatal("no preface reached the front door")
	}
	for _, p := range seen {
		if p.addr != "" {
			t.Errorf("the preface claims an address: %q", p.line)
		}
	}

	// Off: the bytes are those of a plain request, a door that rejects the
	// preface is served, and none was sent.
	plain := newPrefaceListener(t, false, false, maintenanceHandler())
	s.opts.ProxyProtocol = false
	s.opts.IngressServiceExternal = plain.Addr
	if err := s.confirmProjectMaintenance(context.Background(), app); err != nil {
		t.Fatalf("with the option off: %v", err)
	}
	if plain.rejected.Load() != 0 || plain.conns.Load() == 0 {
		t.Errorf("plain door: %d connections, %d with a preface", plain.conns.Load(), plain.rejected.Load())
	}
}

func TestMaintenanceCheckToTheInternalFrontDoorSendsNoPreface(t *testing.T) {
	shortMaintenanceTimeout(t)
	app := &shpyrdv1.App{Spec: shpyrdv1.AppSpec{Exposure: "internal"}, Status: shpyrdv1.AppStatus{URL: "http://site.example"}}
	for _, on := range []bool{true, false} {
		door := newPrefaceListener(t, false, false, maintenanceHandler())
		s, _ := newTestServer(t, nil, nil)
		s.opts.ProxyProtocol = on
		s.opts.IngressServiceInternal = door.Addr
		if err := s.confirmProjectMaintenance(context.Background(), app); err != nil {
			t.Fatalf("option %v: %v", on, err)
		}
		if door.rejected.Load() != 0 || door.conns.Load() == 0 {
			t.Errorf("option %v: %d connections, %d with a preface", on, door.conns.Load(), door.rejected.Load())
		}
	}
}

func TestSignInDiscoveryAnnouncesItselfToTheExternalFrontDoor(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	get := func(s *Server, domain string) error {
		s.rp.domain = domain
		client := s.rp.httpClient()
		client.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = true
		resp, err := client.Get("https://idp." + domain + "/.well-known/openid-configuration")
		if err != nil {
			return err
		}
		resp.Body.Close()
		return nil
	}

	door := newPrefaceListener(t, true, true, h)
	s, _ := newTestServer(t, nil, nil)
	s.opts.ProxyProtocol = true
	s.rp.ingress = door.Addr
	s.rp.announce = s.announcesToFrontDoor
	if err := get(s, "cluster.example"); err != nil {
		t.Fatalf("with the option on: %v", err)
	}
	if seen := door.prefaces(); len(seen) == 0 || seen[0].addr != "" {
		t.Errorf("prefaces seen: %+v", seen)
	}

	// Off: nothing is sent.
	plain := newPrefaceListener(t, false, true, h)
	s.opts.ProxyProtocol = false
	s.rp.ingress = plain.Addr
	if err := get(s, "cluster.example"); err != nil {
		t.Fatalf("with the option off: %v", err)
	}
	if plain.rejected.Load() != 0 || plain.conns.Load() == 0 {
		t.Errorf("plain door: %d connections, %d with a preface", plain.conns.Load(), plain.rejected.Load())
	}

	// On, but the address is the internal front door: nothing is sent.
	s.opts.ProxyProtocol = true
	s.opts.IngressServiceInternal = plain.Addr
	if err := get(s, "cluster.example"); err != nil {
		t.Fatalf("internal door: %v", err)
	}
	if plain.rejected.Load() != 0 {
		t.Errorf("a preface reached the internal front door")
	}
}

// With the option on and no internal controller Service, a bare internal
// name is refused saying the internal front door is missing, and nothing is
// dialled.
func TestBareInternalNameNeverFallsBackToTheExternalFrontDoor(t *testing.T) {
	external := newPrefaceListener(t, true, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	s := internalNamesServer(t)
	s.opts.ProxyProtocol = true
	s.opts.IngressServiceExternal = external.Addr

	if door := s.internalDoor(context.Background()); door != "" {
		t.Fatalf("door = %q, want none", door)
	}
	req := httptest.NewRequest("GET", "/api/customers", nil)
	req.Host = "crm.internal"
	req.Header.Set("X-Real-IP", "10.1.0.5")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(notifyingRecorder{rec}, req)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "internal front door is missing") {
		t.Fatalf("= %d %s", rec.Code, rec.Body.String())
	}
	if external.conns.Load() != 0 {
		t.Errorf("%d connections opened to the external front door", external.conns.Load())
	}

	// Off: the fall back is what it was.
	s.opts.ProxyProtocol = false
	s.doorCache = doorCache{}
	if door := s.internalDoor(context.Background()); !strings.HasSuffix(door, ":80") {
		t.Errorf("door with the option off = %q", door)
	}
}
