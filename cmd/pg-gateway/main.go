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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

	// The port table follows the Postgres resources: reload now and every
	// 30 s, opening a listener for every wake port that appears and closing
	// the ones that go. The process lives until the context ends, whether or
	// not any database has a policy yet.
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		if err := gw.reload(ctx); err != nil {
			log.Warn("reload", "err", err)
		}
		gw.syncListeners(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
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
	// listeners are the open wake-port listeners, port → cancel.
	listeners map[int]context.CancelFunc
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
	changed := len(ports) != len(g.ports)
	if !changed {
		for p, db := range ports {
			if g.ports[p] != db {
				changed = true
				break
			}
		}
	}
	g.ports = ports
	g.mu.Unlock()
	if changed {
		g.log.Info("port table reloaded", "databases", len(ports))
	}
	return nil
}

// syncListeners opens a listener for every wake port in the table that has
// none, and closes listeners whose port left the table.
func (g *gateway) syncListeners(ctx context.Context) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.listeners == nil {
		g.listeners = map[int]context.CancelFunc{}
	}
	for port, cancel := range g.listeners {
		if _, still := g.ports[port]; !still {
			cancel()
			delete(g.listeners, port)
			g.log.Info("listener closed", "port", port)
		}
	}
	for port, db := range g.ports {
		if _, open := g.listeners[port]; open {
			continue
		}
		lctx, cancel := context.WithCancel(ctx)
		g.listeners[port] = cancel
		go g.listenPort(lctx, port, db[0], db[1])
	}
}

func (g *gateway) listenPort(ctx context.Context, port int, ns, name string) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		g.log.Error("listen", "port", port, "err", err)
		g.mu.Lock()
		delete(g.listeners, port) // try again on the next sync
		g.mu.Unlock()
		return
	}
	g.log.Info("listening", "port", port, "database", ns+"/"+name)
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
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

	// Wait for the primary to be reachable. CNPG's "<name>-rw" Service holds
	// the primary once it passes readiness — never this gateway, which the
	// shpyrd-owned "<name>" Service points at while the database sleeps.
	deadline := time.Now().Add(maxWakeWait)
	var backend string
	for {
		if time.Now().After(deadline) {
			g.log.Warn("wake timeout", "database", key)
			return
		}
		ep, err := g.cs.CoreV1().Endpoints(ns).Get(ctx, name+"-rw", metav1.GetOptions{})
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

// wake starts a sleeping database: it removes CNPG's hibernation
// annotation itself (the time-critical part — CNPG starts the pods within
// seconds) and marks the status waking so the reconciler, which watches
// status, moves the Service back to the primary once it is ready. A
// suspended database is left alone: its Service never points here.
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
		return nil // awake, waking or suspended
	}
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "Cluster"})
	if err := cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, cluster); err == nil {
		if ann := cluster.GetAnnotations(); ann["cnpg.io/hibernation"] != "" {
			cp := cluster.DeepCopy()
			delete(ann, "cnpg.io/hibernation")
			cluster.SetAnnotations(ann)
			if err := cl.Patch(ctx, cluster, client.MergeFrom(cp)); err != nil {
				return fmt.Errorf("un-hibernate: %w", err)
			}
		}
	}
	patch := client.MergeFrom(pg.DeepCopy())
	pg.Status.Sleep.State = "waking"
	pg.Status.Sleep.Message = "waking: a client connected"
	return cl.Status().Patch(ctx, pg, patch)
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
