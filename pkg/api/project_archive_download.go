package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shpyrd-io/shpyrd/pkg/projectarchive"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Files live only until their one-time download finishes, or for five minutes
// if the browser never starts it. This is not a backup catalog. The file stays
// on the replica that built it; with a store and more than one replica, the
// store says which replica that is (keyed by the ticket's hash), and a
// download that lands on another replica is fetched from it over the
// internal sources port, the ticket itself the only credential.
type archiveDownload struct {
	bundle                *projectarchive.Bundle
	file, name, namespace string
	timer                 *time.Timer
}
type archiveDownloads struct {
	mu    sync.Mutex
	files map[string]*archiveDownload
	// backing and peer, set together: the store the route is kept in and
	// this replica's internal address (http://<pod IP>:<sources port>).
	backing store.Store
	peer    string
}

// archiveDownloadTTL is how long an export waits for its download.
const archiveDownloadTTL = 5 * time.Minute

// archivePeerPath is where a replica hands its exports to another replica,
// on the internal sources port.
const archivePeerPath = "/internal/project-archives"

// archiveRoute is what the store keeps for a download: the replica holding
// the file and the project it belongs to.
type archiveRoute struct {
	Peer      string `json:"peer"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

func newArchiveDownloads() *archiveDownloads {
	return &archiveDownloads{files: map[string]*archiveDownload{}}
}

// sharedArchiveDownloads routes downloads through st when this replica can
// be reached at peer; either empty keeps them local, which a single replica
// is enough for.
func sharedArchiveDownloads(st store.Store, peer string) *archiveDownloads {
	d := newArchiveDownloads()
	if st != nil && peer != "" {
		d.backing, d.peer = st, peer
	}
	return d
}

func (d *archiveDownloads) keep(b *projectarchive.Bundle, file, name, namespace string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	key := hashCode(token)
	d.mu.Lock()
	if len(d.files) >= 4 {
		d.mu.Unlock()
		return "", errors.New("too many pending project downloads; finish a download and try again")
	}
	entry := &archiveDownload{bundle: b, file: file, name: name, namespace: namespace}
	d.files[key] = entry
	entry.timer = time.AfterFunc(archiveDownloadTTL, func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.files[key] == entry {
			delete(d.files, key)
			_ = b.Close()
		}
	})
	d.mu.Unlock()
	if d.backing != nil {
		payload, err := json.Marshal(archiveRoute{Peer: d.peer, Namespace: namespace, Name: name})
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), ticketStoreTimeout)
			defer cancel()
			err = d.backing.PutArchiveDownload(ctx, key, payload, time.Now().Add(archiveDownloadTTL))
		}
		if err != nil {
			// Without its route the download works only here: refuse it
			// rather than hand out a ticket another replica cannot serve.
			if e, _ := d.takeLocal(token, namespace); e != nil {
				e.bundle.Close()
			}
			return "", err
		}
	}
	return token, nil
}

var errArchiveGone = errors.New("download is invalid or expired; export the project again")

// takeLocal hands out the file of a download held by this replica.
func (d *archiveDownloads) takeLocal(token, namespace string) (*archiveDownload, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := hashCode(token)
	entry, ok := d.files[key]
	if !ok || entry.namespace != namespace {
		return nil, errArchiveGone
	}
	delete(d.files, key)
	entry.timer.Stop()
	return entry, nil
}

// take finds a download: on this replica (its route, if any, goes too), or
// the route to the replica that holds it. One shot either way.
func (d *archiveDownloads) take(ctx context.Context, token, namespace string) (*archiveDownload, *archiveRoute, error) {
	if token == "" {
		return nil, nil, errArchiveGone
	}
	if entry, err := d.takeLocal(token, namespace); err == nil {
		if d.backing != nil {
			_, _, _ = d.backing.TakeArchiveDownload(ctx, hashCode(token))
		}
		return entry, nil, nil
	}
	if d.backing == nil {
		return nil, nil, errArchiveGone
	}
	payload, ok, err := d.backing.TakeArchiveDownload(ctx, hashCode(token))
	if err != nil {
		return nil, nil, err
	}
	var route archiveRoute
	if !ok || json.Unmarshal(payload, &route) != nil || route.Namespace != namespace || route.Peer == "" || route.Peer == d.peer {
		return nil, nil, errArchiveGone
	}
	return nil, &route, nil
}

func (s *Server) downloadProjectArchive(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	entry, route, err := s.archiveDownloads.take(c.Request.Context(), c.Query("ticket"), app.Namespace)
	if err != nil {
		abort(c, http.StatusGone, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "application/gzip")
	c.Header("Referrer-Policy", "no-referrer")
	if route != nil {
		s.relayProjectArchive(c, route, c.Query("ticket"))
		return
	}
	defer entry.bundle.Close()
	c.FileAttachment(entry.file, entry.name)
}

// relayProjectArchive streams a download held by another replica.
func (s *Server) relayProjectArchive(c *gin.Context, route *archiveRoute, token string) {
	q := url.Values{"ticket": {token}, "namespace": {route.Namespace}}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, route.Peer+archivePeerPath+"?"+q.Encode(), nil)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		abort(c, http.StatusBadGateway, errors.New("the replica holding this download did not answer; export the project again"))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		abort(c, http.StatusGone, errArchiveGone)
		return
	}
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(route.Name))
	if resp.ContentLength >= 0 {
		c.Header("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, resp.Body)
}

// servePeerArchive is the internal side of a relayed download, on the
// sources port: the file of a download this replica holds, for the ticket
// that names it.
func (s *Server) servePeerArchive(c *gin.Context) {
	entry, err := s.archiveDownloads.takeLocal(c.Query("ticket"), c.Query("namespace"))
	if err != nil {
		c.Status(http.StatusGone)
		return
	}
	defer entry.bundle.Close()
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "application/gzip")
	c.File(entry.file)
}
