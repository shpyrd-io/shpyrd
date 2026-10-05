//go:build !foss

package costs

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/shpyrd-io/shpyrd/ee/licensing"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Cost drains send the cost lines elsewhere, the way log drains send logs:
// one POST of JSON at a time, with the drain's headers, of the lines that
// changed since the drain's cursor, oldest first. A receiver that answers
// 2xx moves the cursor; any other answer is counted and the same lines go
// again on the next pass: delivery is at least once, and a line's id says
// which line it is.

// drainSecret holds a drain's header values.
func drainSecret(id string) string { return "shpyrd-cost-drain-" + id }

// batchSize is how many lines one POST carries.
const batchSize = 500

// Payload is the body of a POST to a drain.
type Payload struct {
	Cluster string    `json:"cluster"`
	SentAt  time.Time `json:"sentAt"`
	Lines   []Line    `json:"lines"`
}

// Line is a cost line as a drain gets it: its project by the project's
// UUID, as its workspace is, where the cluster keeps the short id the
// namespace labels carry; and the project's name as it is when sent, so a
// receiver that keeps projects by id renames them.
type Line struct {
	store.CostLine
	ProjectName string `json:"projectName,omitempty"`
}

// sent turns the lines kept into the lines sent.
func sent(lines []store.CostLine, projects map[string]store.Project) []Line {
	out := make([]Line, len(lines))
	for i, l := range lines {
		out[i].CostLine = l
		if p, ok := projects[l.Project]; ok {
			out[i].Project = p.ID
			out[i].ProjectName = cmp.Or(p.Name, p.Slug)
		} else if id, err := ids.Decode(l.Project); err == nil {
			out[i].Project = id
		}
	}
	return out
}

// Sender is the drains' loop.
type Sender struct {
	Store     store.Store
	Kube      kubernetes.Interface
	Namespace string
	Cluster   string
	HTTP      *http.Client
	Logger    *slog.Logger
	Every     time.Duration // five minutes by default
	MaxBatch  int           // batches per drain and pass; 20 by default
}

func (s *Sender) NeedLeaderElection() bool { return true }

// Start runs until ctx ends.
func (s *Sender) Start(ctx context.Context) error {
	every := s.Every
	if every <= 0 {
		every = 5 * time.Minute
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		s.Send(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// Send is one pass over every drain.
func (s *Sender) Send(ctx context.Context) {
	if !licensing.Active() {
		return
	}
	log := s.Logger
	if log == nil {
		log = slog.Default()
	}
	drains, err := s.Store.ListCostDrains(ctx)
	if err != nil {
		log.Warn("cost drains", "error", err)
		return
	}
	if len(drains) == 0 {
		return
	}
	projects := projectsByShortID(ctx, s.Store)
	for _, d := range drains {
		if err := s.drain(ctx, d, projects); err != nil {
			log.Warn("cost drain", "drain", d.Name, "error", err)
		}
	}
}

func (s *Sender) drain(ctx context.Context, d store.CostDrain, projects map[string]store.Project) error {
	headers, err := s.headers(ctx, d.ID)
	if err != nil {
		return err
	}
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	max := s.MaxBatch
	if max <= 0 {
		max = 20
	}
	var at time.Time
	if d.CursorAt != nil {
		at = *d.CursorAt
	}
	id := d.CursorID
	for i := 0; i < max; i++ {
		lines, err := s.Store.CostLinesChangedSince(ctx, at, id, batchSize)
		if err != nil || len(lines) == 0 {
			return err
		}
		if err := post(ctx, client, d.URL, headers, Payload{Cluster: s.Cluster, SentAt: time.Now().UTC(), Lines: sent(lines, projects)}); err != nil {
			return s.Store.RecordCostDelivery(ctx, d.ID, store.CostDelivery{At: time.Now(), Err: err.Error()})
		}
		last := lines[len(lines)-1]
		at, id = last.ChangedAt, last.ID
		cursor := at
		if err := s.Store.RecordCostDelivery(ctx, d.ID, store.CostDelivery{At: time.Now(), Sent: len(lines), CursorAt: &cursor, CursorID: id}); err != nil {
			return err
		}
		if len(lines) < batchSize {
			return nil
		}
	}
	return nil
}

func (s *Sender) headers(ctx context.Context, id string) (map[string]string, error) {
	out := map[string]string{}
	if s.Kube == nil {
		return out, nil
	}
	sec, err := s.Kube.CoreV1().Secrets(s.Namespace).Get(ctx, drainSecret(id), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for k, v := range sec.Data {
		out[k] = string(v)
	}
	return out, nil
}

func post(ctx context.Context, client *http.Client, url string, headers map[string]string, p Payload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "shpyrd-cost-drain")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 256))
		return fmt.Errorf("%s: %s", res.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}
