package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
)

// One-off commands over the API (RFC-0052): `shpyrd run` signed in with
// `shpyrd login` has no cluster to create a pod in, so the server creates
// the one-off instance and hands back a ticket for the web terminal's
// socket (RFC-0026), which attaches to the instance's own process and
// removes it when the session ends.

// RunRequest is POST /api/projects/:slug/run.
type RunRequest struct {
	Command []string `json:"command"`
	// Size is an instance size from the catalog; the catalog's default
	// when empty.
	Size string `json:"size,omitempty"`
	// Detach starts the instance and returns: nothing attaches, and the
	// instance is not removed when the command ends (its deadline is).
	Detach bool `json:"detach,omitempty"`
	// TTY says the attached terminal is one: the process gets a pty.
	TTY bool `json:"tty,omitempty"`
	// Process mounts that process's volumes at their paths, on the machine
	// that holds a single-instance disk right now (#126): a data migration
	// on a disk, a look at a crash-looping process's files.
	Process string `json:"process,omitempty"`
}

// RunResult is what a started one-off instance is called, and the ticket
// to attach to it (empty when detached).
type RunResult struct {
	Name   string `json:"name"`
	Size   string `json:"size"`
	Ticket string `json:"ticket,omitempty"`
	// Note says what the instance got beyond the command: the volumes of
	// a process, and the machine it runs on for them.
	Note string `json:"note,omitempty"`
}

// runWaitTimeout bounds how long the socket waits for a one-off instance
// to start before attaching: pulling an image onto a new node takes a
// while; a pod that cannot start at all says so sooner (waitRunPod).
const runWaitTimeout = 3 * time.Minute

// runApp creates the one-off instance.
func (s *Server) runApp(c *gin.Context) {
	var req RunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	if len(req.Command) == 0 {
		abort(c, http.StatusBadRequest, errors.New("give the command to run"))
		return
	}
	if len(strings.Join(req.Command, " ")) > 4096 {
		abort(c, http.StatusBadRequest, errors.New("the command is too long"))
		return
	}
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	image := firstNonEmpty(app.Spec.Image, app.Status.Image)
	if image == "" {
		abort(c, http.StatusConflict, errors.New("the project has no release yet; deploy first"))
		return
	}
	catalog, err := s.catalog(c.Request.Context())
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	res, sizeName, err := catalog.Resolve(req.Size, corev1.ResourceRequirements{})
	if err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	// Node pools (RFC-0077): one-off runs belong with the apps.
	pool := ""
	if info, err := install.ReadInstallInfo(c.Request.Context(), s.kube, s.deps().SystemNamespace); err == nil {
		pool = info.Vars[install.VarAppsPool]
	}
	attach := !req.Detach
	pod := controller.OneOffPod(app, image, req.Command, res, attach && req.TTY, attach, pool)
	note := ""
	if req.Process != "" {
		mounts, node, err := s.processMounts(c.Request.Context(), app, req.Process)
		if err != nil {
			abort(c, http.StatusBadRequest, err)
			return
		}
		controller.MountVolumesOnPod(&pod.Spec, mounts, node)
		note = controller.MountsNote(req.Process, mounts, node)
	}
	created, err := s.kube.Kube.CoreV1().Pods(app.Namespace).Create(c.Request.Context(), pod, metav1.CreateOptions{})
	if err != nil {
		abort(c, http.StatusBadGateway, fmt.Errorf("start one-off instance: %w", err))
		return
	}
	slug := project.SlugOf(app)
	detail := strings.Join(req.Command, " ")
	if req.Detach {
		s.audit(c, slug, "run", created.Name, detail+" (detached)")
		c.JSON(http.StatusCreated, RunResult{Name: created.Name, Size: sizeLabel(catalog, sizeName), Note: note})
		return
	}
	s.audit(c, slug, "run", created.Name, detail)
	id, _ := ext.IdentityFrom(c)
	code, err := s.execTickets.mint(execTicket{
		Identity: id, Workspace: s.workspace(c), Project: slug, Instance: created.Name,
		Pod: created.Name, Container: appContainer, Attach: true, TTY: req.TTY, Command: req.Command,
	})
	if err != nil {
		// The instance would otherwise wait out its deadline for a client
		// that cannot come.
		_ = s.kube.Kube.CoreV1().Pods(app.Namespace).Delete(context.Background(), created.Name, metav1.DeleteOptions{})
		if errors.Is(err, errTicketsFull) {
			c.Header("Retry-After", "30")
			abort(c, http.StatusServiceUnavailable, err)
			return
		}
		abort(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusCreated, RunResult{Name: created.Name, Size: sizeLabel(catalog, sizeName), Ticket: code, Note: note})
}

// processMounts resolves a process's volumes for a one-off instance (#126)
// and the machine that holds a single-instance claim now.
func (s *Server) processMounts(ctx context.Context, app *shpyrdv1.App, process string) ([]controller.ProcessMount, string, error) {
	var list shpyrdv1.VolumeList
	if err := s.apps.List(ctx, &list, client.InNamespace(app.Namespace)); err != nil {
		return nil, "", err
	}
	volumes := map[string]*shpyrdv1.Volume{}
	for i := range list.Items {
		volumes[list.Items[i].Name] = &list.Items[i]
	}
	mounts, err := controller.ProcessMounts(app, process, volumes)
	if err != nil {
		return nil, "", err
	}
	node := ""
	if pods, err := s.kube.Kube.CoreV1().Pods(app.Namespace).List(ctx, metav1.ListOptions{}); err == nil {
		node = controller.ClaimHolderNode(pods.Items, mounts)
	}
	return mounts, node, nil
}

// sizeLabel names a size the way the CLI prints it: "shared-m: 0.5 CPU,
// 1Gi".
func sizeLabel(cat *sizes.Catalog, name string) string {
	if s, ok := cat.Get(name); ok {
		return fmt.Sprintf("%s: %s CPU, %s", s.Name, s.CPU, s.Memory)
	}
	return name
}

// waitRunPod waits for a one-off instance to be running, or done already,
// before the socket attaches. A pod that cannot start (the image cannot be
// pulled, the container cannot be created) is reported at once.
func (s *Server) waitRunPod(ctx context.Context, namespace, name string) error {
	ctx, cancel := context.WithTimeout(ctx, runWaitTimeout)
	defer cancel()
	for {
		p, err := s.kube.Kube.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if why := controller.StartupFailure(p, time.Now()); why != "" {
			return fmt.Errorf("instance cannot start: %s", why)
		}
		switch p.Status.Phase {
		case corev1.PodRunning, corev1.PodSucceeded, corev1.PodFailed:
			return nil
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return errors.New("timed out waiting for the one-off instance to start")
			}
			return ctx.Err()
		case <-time.After(s.runPoll):
		}
	}
}

// runExitCode reads how the one-off command ended, once it has: the
// container's exit code. Not found says the pod is gone or still running
// after a short wait.
func (s *Server) runExitCode(ctx context.Context, namespace, name string) (int, bool) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		p, err := s.kube.Kube.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return 0, false
			}
			return 0, false
		}
		for _, cs := range p.Status.ContainerStatuses {
			if cs.State.Terminated != nil {
				return int(cs.State.Terminated.ExitCode), true
			}
		}
		if time.Now().After(deadline) {
			return 0, false
		}
		select {
		case <-ctx.Done():
			return 0, false
		case <-time.After(s.runPoll):
		}
	}
}
