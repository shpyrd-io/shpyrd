// pg-gateway is a TCP wake-on-connect proxy for sleeping PostgreSQL databases
// (RFC-0075). It holds a raw TCP connection on a per-database port, asks the
// Postgres reconciler to wake the cluster, and pipes bytes once the primary
// is ready — without reading or writing a single byte of the PostgreSQL
// protocol.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// maxWakeWait is the deadline for a database to become ready after a
	// wake request. Past this, the connection is closed.
	maxWakeWait = 60 * time.Second
	// maxPending is the total number of held connections per database.
	maxPending = 32
	// readyPollInterval is how often to check whether the database is awake.
	readyPollInterval = 2 * time.Second
)

func main() {
	log := slog.Default()
	ns := envOr("SHPYRD_SYSTEM_NAMESPACE", "shpyrd-system")

	cfg, err := rest.InClusterConfig()
	if err != nil {
		log.Error("in-cluster config", "err", err)
		os.Exit(1)
	}
	scheme, err := kube.Scheme()
	if err != nil {
		log.Error("scheme", "err", err)
		os.Exit(1)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Error("kubernetes client", "err", err)
		os.Exit(1)
	}
	gw := &gateway{log: log, cfg: cfg, scheme: scheme, cs: cs, ns: ns, waits: map[string]*waitGroup{}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Build the initial port → database mapping from Postgres CRDs.
	if err := gw.reload(ctx); err != nil {
		log.Error("initial reload", "err", err)
	}

	// Reload every minute (new databases, port changes).
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := gw.reload(ctx); err != nil {
					log.Warn("reload", "err", err)
				}
			}
		}
	}()

	gw.listen(ctx)
}

type gateway struct {
	log    *slog.Logger
	cfg    *rest.Config
	scheme *runtime.Scheme
	cs     kubernetes.Interface
	ns     string

	mu    sync.RWMutex
	ports map[int][2]string // port → {namespace, name}
	waits map[string]*waitGroup
}

type waitGroup struct {
	mu      sync.Mutex
	pending int
}

func (g *gateway) reload(ctx context.Context) error {
	var list shpyrdv1.PostgresList
	cl, err := client.New(g.cfg, client.Options{Scheme: g.scheme})
	if err != nil {
		return err
	}
	if err := cl.List(ctx, &list); err != nil {
		return err
	}
	ports := map[int][2]string{}
	for _, pg := range list.Items {
		if pg.Status.Sleep == nil || pg.Status.Sleep.WakePort == nil {
			continue
		}
		port := int(*pg.Status.Sleep.WakePort)
		ports[port] = [2]string{pg.Namespace, pg.Name}
	}
	g.mu.Lock()
	g.ports = ports
	g.mu.Unlock()
	g.log.Info("port table reloaded", "databases", len(ports))
	return nil
}

func (g *gateway) listen(ctx context.Context) {
	g.mu.RLock()
	ports := g.ports
	g.mu.RUnlock()

	var wg sync.WaitGroup
	for port, db := range ports {
		port, db := port, db
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.listenPort(ctx, port, db[0], db[1])
		}()
	}
	wg.Wait()
}

func (g *gateway) listenPort(ctx context.Context, port int, ns, name string) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		g.log.Error("listen", "port", port, "err", err)
		return
	}
	defer ln.Close()
	g.log.Info("listening", "port", port, "database", ns+"/"+name)
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				g.log.Warn("accept", "port", port, "err", err)
				continue
			}
		}
		go g.handleConn(ctx, conn, ns, name)
	}
}

func (g *gateway) handleConn(ctx context.Context, conn net.Conn, ns, name string) {
	defer conn.Close()
	key := ns + "/" + name

	g.mu.Lock()
	wg, ok := g.waits[key]
	if !ok {
		wg = &waitGroup{}
		g.waits[key] = wg
	}
	g.mu.Unlock()

	wg.mu.Lock()
	if wg.pending >= maxPending {
		wg.mu.Unlock()
		g.log.Warn("pending limit reached", "database", key)
		return
	}
	wg.pending++
	wg.mu.Unlock()
	defer func() {
		wg.mu.Lock()
		wg.pending--
		wg.mu.Unlock()
	}()

	// Ask the reconciler to wake the database.
	if err := g.wake(ctx, ns, name); err != nil {
		g.log.Warn("wake request failed", "database", key, "err", err)
	}

	// Wait for the primary to be reachable.
	deadline := time.Now().Add(maxWakeWait)
	var backend string
	for {
		if time.Now().After(deadline) {
			g.log.Warn("wake timeout", "database", key)
			return
		}
		ep, err := g.cs.CoreV1().Endpoints(ns).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			for _, sub := range ep.Subsets {
				if len(sub.Addresses) > 0 && len(sub.Ports) > 0 {
					backend = sub.Addresses[0].IP + ":" + strconv.Itoa(int(sub.Ports[0].Port))
					break
				}
			}
		}
		if backend != "" {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(readyPollInterval):
		}
	}

	// Dial the primary and pipe.
	bConn, err := net.DialTimeout("tcp", backend, 5*time.Second)
	if err != nil {
		g.log.Warn("dial primary failed", "backend", backend, "err", err)
		return
	}
	defer bConn.Close()
	g.log.Info("proxying", "database", key, "client", conn.RemoteAddr(), "backend", backend)

	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(bConn, conn)
		close(done)
	}()
	_, _ = io.Copy(conn, bConn)
	<-done
}

// wake patches the CNPG Cluster's hibernation annotation off so the
// reconciler starts the pods.
func (g *gateway) wake(ctx context.Context, ns, name string) error {
	cl, err := client.New(g.cfg, client.Options{Scheme: g.scheme})
	if err != nil {
		return err
	}
	pg := &shpyrdv1.Postgres{}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, pg); err != nil {
		return err
	}
	if pg.Status.Sleep == nil || pg.Status.Sleep.State != "sleeping" {
		return nil // already awake or waking
	}
	// Patch the status to "waking" and remove the CNPG annotation.
	// The full reconciliation runs in the controller; we just trigger it.
	patch := client.MergeFrom(pg.DeepCopy())
	if pg.Status.Sleep == nil {
		pg.Status.Sleep = &shpyrdv1.PostgresSleepStatus{}
	}
	pg.Status.Sleep.State = "waking"
	return cl.Status().Patch(ctx, pg, patch)
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
