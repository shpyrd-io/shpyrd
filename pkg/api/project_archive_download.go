package api

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shpyrd-io/shpyrd/pkg/projectarchive"
)

// Files live only until their one-time download finishes, or for five minutes
// if the browser never starts it. This is not a backup catalog. Like shell
// tickets, it uses the server's existing single-replica deployment model.
type archiveDownload struct {
	bundle                *projectarchive.Bundle
	file, name, namespace string
	timer                 *time.Timer
}
type archiveDownloads struct {
	mu    sync.Mutex
	files map[string]*archiveDownload
}

func newArchiveDownloads() *archiveDownloads {
	return &archiveDownloads{files: map[string]*archiveDownload{}}
}

func (d *archiveDownloads) keep(b *projectarchive.Bundle, file, name, namespace string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	key := hashCode(token)
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.files) >= 4 {
		return "", errors.New("too many pending project downloads; finish a download and try again")
	}
	entry := &archiveDownload{bundle: b, file: file, name: name, namespace: namespace}
	d.files[key] = entry
	entry.timer = time.AfterFunc(5*time.Minute, func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.files[key] == entry {
			delete(d.files, key)
			_ = b.Close()
		}
	})
	return token, nil
}

func (d *archiveDownloads) take(token, namespace string) (*archiveDownload, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := hashCode(token)
	entry, ok := d.files[key]
	if !ok || entry.namespace != namespace {
		return nil, errors.New("download is invalid or expired; export the project again")
	}
	delete(d.files, key)
	entry.timer.Stop()
	return entry, nil
}

func (s *Server) downloadProjectArchive(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	entry, err := s.archiveDownloads.take(c.Query("ticket"), app.Namespace)
	if err != nil {
		abort(c, http.StatusGone, err)
		return
	}
	defer entry.bundle.Close()
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "application/gzip")
	c.Header("Referrer-Policy", "no-referrer")
	c.FileAttachment(entry.file, entry.name)
}
