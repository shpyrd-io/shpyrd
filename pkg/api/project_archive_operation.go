package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/projectarchive"
)

const projectOperationSecret = "shpyrd-project-operation"

type projectArchiveOperation struct {
	ResumeStarted    bool                            `json:"resumeStarted,omitempty"`
	Move             *projectMove                    `json:"move,omitempty"`
	ID               string                          `json:"id"`
	Kind             string                          `json:"kind"`
	StartedAt        time.Time                       `json:"startedAt"`
	State            projectarchive.TransactionState `json:"state"`
	Original         projectArchiveMetadata          `json:"original"`
	Limits           map[string]int                  `json:"limits"`
	CreatedVolumes   []string                        `json:"createdVolumes,omitempty"`
	CreatedDatabases []string                        `json:"createdDatabases,omitempty"`
	// Warnings is the operation's report: what a helper said about the data
	// without failing, such as a link copied as it is (#129). Each sentence
	// names the file it is about.
	Warnings []string `json:"warnings,omitempty"`
}

// warn adds a sentence to the operation's report, once. The report is
// bounded so the operation's record stays small; past the bound it says so.
func (op *projectArchiveOperation) warn(sentence string) {
	const bound = 100
	if sentence == "" || slices.Contains(op.Warnings, sentence) || len(op.Warnings) > bound {
		return
	}
	if len(op.Warnings) == bound {
		sentence = "Further warnings were left out of this report."
	}
	op.Warnings = append(op.Warnings, sentence)
}

// Starting workloads (including workers and direct database clients) can
// accept writes before HTTP maintenance is lifted. Persist that boundary.
func (op *projectArchiveOperation) canRollback() bool {
	return !op.ResumeStarted && op.State.Phase != "starting" && op.State.Phase != "releasing" && op.State.Phase != "complete"
}

func (s *Server) beginProjectArchive(ctx context.Context, app *shpyrdv1.App, kind string, original *projectArchiveMetadata) (*projectArchiveOperation, error) {
	if app.Annotations[shpyrdv1.AnnotationMaintenance] != "" {
		return nil, errors.New("project already has a maintenance operation; recover it before starting another")
	}
	op := &projectArchiveOperation{ID: strings.ReplaceAll(uuid.NewString(), "-", ""), Kind: kind, StartedAt: time.Now().UTC(), State: projectarchive.TransactionState{Phase: "preparing"}, Original: *original, Limits: map[string]int{}}
	data, err := json.Marshal(op)
	if err != nil {
		return nil, err
	}
	if len(data) > 800<<10 {
		return nil, errors.New("project recovery metadata exceeds 800 KiB")
	}
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: projectOperationSecret, Namespace: app.Namespace, Labels: map[string]string{shpyrdv1.LabelApp: app.Name, projectOperationLabel: op.ID}}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"operation.json": data}}
	if err := s.apps.Create(ctx, sec); err != nil {
		return nil, err
	}
	before := app.DeepCopy()
	if app.Annotations == nil {
		app.Annotations = map[string]string{}
	}
	app.Annotations[shpyrdv1.AnnotationMaintenance] = "preparing"
	if err := s.apps.Patch(ctx, app, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
		_ = s.apps.Delete(context.WithoutCancel(ctx), sec)
		return nil, err
	}
	return op, nil
}

func (s *Server) saveProjectArchive(ctx context.Context, namespace string, op *projectArchiveOperation) error {
	sec := &corev1.Secret{}
	if err := s.apps.Get(ctx, types.NamespacedName{Namespace: namespace, Name: projectOperationSecret}, sec); err != nil {
		return err
	}
	if sec.Labels[projectOperationLabel] != op.ID {
		return errors.New("project operation ownership changed")
	}
	data, err := json.Marshal(op)
	if err != nil {
		return err
	}
	if len(data) > 800<<10 {
		return errors.New("project recovery metadata exceeds 800 KiB")
	}
	sec.Data = map[string][]byte{"operation.json": data}
	return s.apps.Update(ctx, sec)
}

func (s *Server) readProjectArchive(ctx context.Context, namespace string) (*projectArchiveOperation, error) {
	sec := &corev1.Secret{}
	if err := s.apps.Get(ctx, types.NamespacedName{Namespace: namespace, Name: projectOperationSecret}, sec); err != nil {
		return nil, err
	}
	var op projectArchiveOperation
	if err := json.Unmarshal(sec.Data["operation.json"], &op); err != nil {
		return nil, err
	}
	if op.ID == "" || sec.Labels[projectOperationLabel] != op.ID {
		return nil, errors.New("invalid project recovery record")
	}
	return &op, nil
}

func (s *Server) projectArchivePhase(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation, phase string) error {
	op.State = projectarchive.TransactionState{Phase: phase}
	if err := s.saveProjectArchive(ctx, app.Namespace, op); err != nil {
		return err
	}
	return s.setProjectMaintenance(ctx, app, phase)
}

func (s *Server) setProjectMaintenance(ctx context.Context, app *shpyrdv1.App, phase string) error {
	current := &shpyrdv1.App{}
	if err := s.apps.Get(ctx, client.ObjectKeyFromObject(app), current); err != nil {
		return err
	}
	before := current.DeepCopy()
	if current.Annotations == nil {
		current.Annotations = map[string]string{}
	}
	if phase == "" {
		delete(current.Annotations, shpyrdv1.AnnotationMaintenance)
	} else {
		current.Annotations[shpyrdv1.AnnotationMaintenance] = phase
	}
	return s.apps.Patch(ctx, current, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}

func (s *Server) waitArchiveCondition(ctx context.Context, check func(context.Context) (bool, error)) error {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		ok, err := check(ctx)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func (s *Server) confirmProjectMaintenance(ctx context.Context, app *shpyrdv1.App) error {
	if app.Status.URL == "" {
		return nil
	}
	u, err := url.Parse(app.Status.URL)
	if err != nil {
		return err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return errors.New("invalid project HTTP address")
	}
	u.Path = "/_shpyrd/maintenance-check"
	u.RawQuery = ""
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if s.rp != nil {
		if configured, ok := s.rp.httpClient().Transport.(*http.Transport); ok {
			transport = configured.Clone()
		}
	}
	defer transport.CloseIdleConnections()
	address := s.projectIngressService(app)
	if address != "" {
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		transport.DialContext = func(c context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(c, network, address)
		}
	}
	httpc := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	// Bounded: a front door that never shows the page (the wrong controller,
	// a host it does not know) must fail the operation before anything else
	// changes, not hold the project in maintenance for the request's hour.
	bounded, stop := context.WithTimeout(ctx, maintenanceConfirmTimeout)
	defer stop()
	confirmed := 0
	last := "no answer"
	err = s.waitArchiveCondition(bounded, func(c context.Context) (bool, error) {
		req, err := http.NewRequestWithContext(c, http.MethodHead, u.String(), nil)
		if err != nil {
			return false, err
		}
		resp, err := httpc.Do(req)
		if err != nil {
			confirmed = 0
			if c.Err() == nil { // a real failure, not the bound cutting the last request
				last = err.Error()
			}
			return false, nil
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		last = resp.Status
		if resp.StatusCode == http.StatusServiceUnavailable && resp.Header.Get("X-Shpyrd-Maintenance") == "true" && resp.Header.Get("Retry-After") != "" {
			confirmed++
		} else {
			confirmed = 0
		}
		return confirmed >= 3, nil
	})
	if err != nil && ctx.Err() == nil && errors.Is(bounded.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("the project's address did not show the maintenance page within %s (last answer: %s, asked through %s): nothing else was changed", maintenanceConfirmTimeout, last, address)
	}
	return err
}

// maintenanceConfirmTimeout bounds the wait for a project's front door to
// show the maintenance page.
var maintenanceConfirmTimeout = 2 * time.Minute

// projectIngressService is the controller Service that serves a project's
// hostnames: the internal front door for an internal project, the external
// one otherwise (RFC-0036), and the platform's when neither is configured.
func (s *Server) projectIngressService(app *shpyrdv1.App) string {
	address := s.opts.IngressServiceExternal
	if app.Spec.Exposure == "internal" {
		address = s.opts.IngressServiceInternal
	}
	if address == "" {
		address = s.opts.IngressService
	}
	return address
}

// drainProjectArchive pauses the project: the front door confirmed to show
// the maintenance page, the phase set, every pod of the project gone. A
// rollback is not strict about the confirmation: it is on its way to
// resuming the project, and a door it cannot reach must not keep it paused;
// the pods still have to be gone before claims are swapped back.
func (s *Server) drainProjectArchive(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation, strict bool) error {
	if err := s.confirmProjectMaintenance(ctx, app); err != nil {
		if strict || ctx.Err() != nil {
			return err
		}
		op.Warnings = append(op.Warnings, "the maintenance page was not confirmed before the rollback: "+err.Error())
	}
	if err := s.projectArchivePhase(ctx, app, op, "paused"); err != nil {
		return err
	}
	return s.waitArchiveCondition(ctx, func(c context.Context) (bool, error) {
		var pods corev1.PodList
		if err := s.apps.List(c, &pods, client.InNamespace(app.Namespace), client.MatchingLabels{shpyrdv1.LabelApp: app.Name}); err != nil {
			return false, err
		}
		for _, p := range pods.Items {
			if p.Labels[projectOperationLabel] == op.ID {
				continue
			}
			if p.Status.Phase != corev1.PodSucceeded && p.Status.Phase != corev1.PodFailed {
				return false, nil
			}
		}
		return true, nil
	})
}

func (s *Server) fenceArchiveDatabases(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation, databases []archivedDatabase) error {
	for _, pg := range databases {
		if err := s.setDatabaseMaintenance(ctx, app.Namespace, pg.Name, op.ID); err != nil {
			return err
		}
		var db projectarchive.PostgreSQL
		if err := s.waitArchiveCondition(ctx, func(c context.Context) (bool, error) {
			var err error
			db, err = s.archiveDatabase(c, app.Namespace, pg.Name)
			return err == nil, nil
		}); err != nil {
			return err
		}
		if err := db.Check(ctx); err != nil {
			return fmt.Errorf("database %s: %w", pg.Name, err)
		}
		limit, err := db.ConnectionLimit(ctx)
		if err != nil {
			return err
		}
		op.Limits[pg.Name] = limit
		if err := s.saveProjectArchive(ctx, app.Namespace, op); err != nil {
			return err
		}
		if err := db.Fence(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) resumeProjectArchive(ctx context.Context, app *shpyrdv1.App, op *projectArchiveOperation) error {
	if err := s.cleanupArchiveHelpers(ctx, app.Namespace, op.ID); err != nil {
		return err
	}
	if !op.ResumeStarted {
		op.ResumeStarted = true
		if err := s.saveProjectArchive(ctx, app.Namespace, op); err != nil {
			return err
		}
	}
	for name, limit := range op.Limits {
		db, err := s.archiveDatabase(ctx, app.Namespace, name)
		if err != nil {
			return err
		}
		if err := db.Unfence(ctx, limit); err != nil {
			return err
		}
	}
	if err := s.projectArchivePhase(ctx, app, op, "starting"); err != nil {
		return err
	}
	if err := s.waitArchiveCondition(ctx, func(c context.Context) (bool, error) {
		current := &shpyrdv1.App{}
		if err := s.apps.Get(c, client.ObjectKeyFromObject(app), current); err != nil {
			return false, err
		}
		if current.Status.ObservedGeneration != current.Generation {
			return false, nil
		}
		var deps appsv1.DeploymentList
		if err := s.apps.List(c, &deps, client.InNamespace(app.Namespace), client.MatchingLabels{shpyrdv1.LabelApp: app.Name}); err != nil {
			return false, err
		}
		for _, d := range deps.Items {
			want := int32(1)
			if d.Spec.Replicas != nil {
				want = *d.Spec.Replicas
			}
			if d.Status.ObservedGeneration != d.Generation || d.Status.ReadyReplicas < want || d.Status.UpdatedReplicas < want {
				return false, nil
			}
		}
		return current.Status.Phase == shpyrdv1.PhaseRunning || current.Status.Phase == shpyrdv1.PhasePending && current.Spec.Image == "" && !current.HasSource(), nil
	}); err != nil {
		return err
	}
	// HTTP stays in maintenance until health checks pass. The earlier
	// ResumeStarted record already protects writes made by starting workloads.
	op.State = projectarchive.TransactionState{Phase: "releasing"}
	if err := s.saveProjectArchive(ctx, app.Namespace, op); err != nil {
		return err
	}
	if err := s.setProjectMaintenance(ctx, app, ""); err != nil {
		return err
	}
	if err := s.clearArchiveDatabaseMaintenance(ctx, app.Namespace, op); err != nil {
		return err
	}
	op.State = projectarchive.TransactionState{Phase: "complete"}
	return s.saveProjectArchive(ctx, app.Namespace, op)
}

func (s *Server) clearProjectArchive(ctx context.Context, namespace string, op *projectArchiveOperation) error {
	sec := &corev1.Secret{}
	if err := s.apps.Get(ctx, types.NamespacedName{Namespace: namespace, Name: projectOperationSecret}, sec); apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}
	if sec.Labels[projectOperationLabel] != op.ID {
		return errors.New("project operation ownership changed")
	}
	return s.apps.Delete(ctx, sec)
}
