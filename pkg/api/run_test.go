package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

func TestRunCreatesTheInstanceAndATicket(t *testing.T) {
	blog := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "blog", Namespace: "app-blog"}, Spec: shpyrdv1.AppSpec{Image: "ghcr.io/x/blog:1"}}
	s, _ := newTestServer(t, nil, []client.Object{blog})

	rec := do(t, s, "POST", "/api/projects/blog/run", `{"command":["rails","db:migrate"],"tty":true}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("run: %d %s", rec.Code, rec.Body.String())
	}
	var res RunResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Name, "blog-run-") || res.Ticket == "" {
		t.Fatalf("result = %+v", res)
	}
	pod, err := s.kube.Kube.CoreV1().Pods("app-blog").Get(context.Background(), res.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c := pod.Spec.Containers[0]
	if pod.Labels[shpyrdv1.LabelProcess] != controller.RunProcess || c.Image != "ghcr.io/x/blog:1" || !c.TTY || !c.Stdin {
		t.Errorf("pod = labels %v image %s tty %v stdin %v", pod.Labels, c.Image, c.TTY, c.Stdin)
	}
	// A prebuilt image runs the command as it is.
	if strings.Join(c.Command, " ") != "rails db:migrate" {
		t.Errorf("command = %v", c.Command)
	}
	// The ticket attaches to that pod.
	tk, err := s.execTickets.redeem(res.Ticket)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Pod != res.Name || !tk.Attach || !tk.TTY || tk.Project != "blog" || tk.Instance != res.Name {
		t.Errorf("ticket = %+v", tk)
	}

	// Detached: no ticket, no stdin.
	rec = do(t, s, "POST", "/api/projects/blog/run", `{"command":["./import.sh"],"detach":true}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("detached run: %d %s", rec.Code, rec.Body.String())
	}
	res = RunResult{}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Ticket != "" {
		t.Errorf("a detached run has no ticket: %+v", res)
	}
	pod, _ = s.kube.Kube.CoreV1().Pods("app-blog").Get(context.Background(), res.Name, metav1.GetOptions{})
	if pod.Spec.Containers[0].Stdin || pod.Spec.Containers[0].TTY {
		t.Error("a detached run attaches nothing")
	}

	// No release yet: nothing to run.
	fresh := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "fresh", Namespace: "app-fresh"}}
	s2, _ := newTestServer(t, nil, []client.Object{fresh})
	if rec := do(t, s2, "POST", "/api/projects/fresh/run", `{"command":["true"]}`, true); rec.Code != http.StatusConflict {
		t.Errorf("no release: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "POST", "/api/projects/blog/run", `{"command":[]}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("no command: %d", rec.Code)
	}
}

// runFixture is a server whose attach is faked and a one-off pod that is
// already running, with the exit code its container will report.
func newRunFixture(t *testing.T, out string, exitCode int32, attachErr error) (*shellFixture, string) {
	t.Helper()
	f := newShellFixture(t, "")
	f.s.runPoll = 10 * time.Millisecond
	f.s.attachStream = func(ctx context.Context, namespace, pod, container string, tty bool, stdin io.Reader, stdout io.Writer, sizes <-chan remotecommand.TerminalSize) error {
		f.mu.Lock()
		f.command = []string{"attach", namespace, pod, container}
		f.mu.Unlock()
		if out != "" {
			_, _ = stdout.Write([]byte(out))
		}
		b, _ := io.ReadAll(stdin)
		f.mu.Lock()
		f.stdin = append(f.stdin, b...)
		f.mu.Unlock()
		// The command ends: the pod reports how.
		cur, _ := f.s.kube.Kube.CoreV1().Pods(namespace).Get(context.Background(), pod, metav1.GetOptions{})
		cur.Status.Phase = corev1.PodSucceeded
		cur.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: container, State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: exitCode}}}}
		_, _ = f.s.kube.Kube.CoreV1().Pods(namespace).UpdateStatus(context.Background(), cur, metav1.UpdateOptions{})
		return attachErr
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "blog-run-abc", Namespace: "app-blog", Labels: map[string]string{shpyrdv1.LabelApp: "blog", shpyrdv1.LabelProcess: controller.RunProcess}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: appContainer, Image: "x"}}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	if _, err := f.s.kube.Kube.CoreV1().Pods("app-blog").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	return f, pod.Name
}

func (f *shellFixture) dialRun(t *testing.T, pod string, command []string) *websocket.Conn {
	t.Helper()
	code, err := f.s.execTickets.mint(execTicket{
		Identity: ext.Identity{Subject: "admin-token", Provider: "token", Admin: true},
		Project:  "blog", Instance: pod, Pod: pod, Container: appContainer, Attach: true, TTY: false, Command: command,
	})
	if err != nil {
		t.Fatal(err)
	}
	return f.dialWith(t, pod, code)
}

func TestRunAttachesAndReportsTheExitCode(t *testing.T) {
	f, pod := newRunFixture(t, "migrated 3 tables\n", 3, nil)
	c := f.dialRun(t, pod, []string{"rails", "db:migrate"})
	defer c.Close()

	open := readControl(t, c)
	if open["type"] != "open" || open["instance"] != pod || open["shell"] != "rails db:migrate" {
		t.Fatalf("open frame = %v", open)
	}
	// Piped input: bytes, then the end of it.
	if err := c.WriteMessage(websocket.BinaryMessage, []byte("y\n")); err != nil {
		t.Fatal(err)
	}
	if err := c.WriteMessage(websocket.TextMessage, []byte(`{"type":"eof"}`)); err != nil {
		t.Fatal(err)
	}
	var out []byte
	var exit map[string]any
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for exit == nil {
		typ, data, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v (output so far %q)", err, out)
		}
		if typ == websocket.BinaryMessage {
			out = append(out, data...)
			continue
		}
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		if m["type"] == "exit" {
			exit = m
		}
	}
	if string(out) != "migrated 3 tables\n" {
		t.Errorf("output = %q", out)
	}
	if exit["code"] != float64(3) {
		t.Errorf("exit = %v, want code 3", exit)
	}
	f.mu.Lock()
	cmd, stdin := f.command, string(f.stdin)
	f.mu.Unlock()
	if strings.Join(cmd, " ") != "attach app-blog "+pod+" app" {
		t.Errorf("attached to %v", cmd)
	}
	if stdin != "y\n" {
		t.Errorf("stdin = %q", stdin)
	}
	// The instance goes with the session.
	waitFor(t, "the one-off pod to be removed", func() bool {
		_, err := f.s.kube.Kube.CoreV1().Pods("app-blog").Get(context.Background(), pod, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	})
}

func TestRunPrintsTheLogWhenTheCommandBeatTheAttach(t *testing.T) {
	// The fake clientset serves no pod logs, so "nothing attached, read the
	// log" is observed as: no bytes, a clean exit 0, the pod removed.
	f, pod := newRunFixture(t, "", 0, nil)
	c := f.dialRun(t, pod, []string{"true"})
	defer c.Close()
	if open := readControl(t, c); open["type"] != "open" {
		t.Fatalf("open frame = %v", open)
	}
	// Nothing to type: stdin ends at once.
	if err := c.WriteMessage(websocket.TextMessage, []byte(`{"type":"eof"}`)); err != nil {
		t.Fatal(err)
	}
	exit := readControl(t, c)
	if exit["type"] != "exit" || exit["code"] != float64(0) {
		t.Errorf("exit = %v", exit)
	}
}

func TestRunRefusesAnotherTicketsPod(t *testing.T) {
	// A ticket bound to a pod is accepted only for the instance it names.
	f, pod := newRunFixture(t, "", 0, nil)
	code, _ := f.s.execTickets.mint(execTicket{
		Identity: ext.Identity{Subject: "admin-token", Provider: "token", Admin: true},
		Project:  "blog", Instance: pod, Pod: pod, Attach: true,
	})
	u := "ws" + strings.TrimPrefix(f.http.URL, "http") + "/api/projects/blog/shell?instance=web.1&ticket=" + code
	if _, resp, err := websocket.DefaultDialer.Dial(u, nil); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("another instance: err %v resp %v", err, resp)
	}
}

// ---- a shell into a resource -------------------------------------------------

// shellableExt is an extension whose one kind takes a shell at a fixed pod.
type shellableExt struct{ ext.Extension }

func (shellableExt) Name() string                   { return "fake-db" }
func (shellableExt) Description() string            { return "" }
func (shellableExt) Components() []ext.ComponentRef { return nil }
func (shellableExt) Routes(ext.Router, ext.Deps) error {
	return nil
}
func (shellableExt) Types() []ext.ResourceType {
	return []ext.ResourceType{{Kind: "FakeDB", Resource: "fakedb"}}
}
func (shellableExt) ResourceShell(ctx context.Context, deps ext.Deps, kind, namespace, name string, args []string) (ext.ShellTarget, error) {
	if name != "main" {
		return ext.ShellTarget{}, apierrors.NewNotFound(corev1.Resource("fakedb"), name)
	}
	return ext.ShellTarget{Pod: name + "-1", Container: "db", Command: append([]string{"dbcli", namespace}, args...)}, nil
}

func TestResourceShellTicketNamesThePodAndCommand(t *testing.T) {
	blog := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "blog", Namespace: "app-blog"}}
	s, _ := newTestServer(t, nil, []client.Object{blog})
	s.opts.Extensions = append(s.opts.Extensions, shellableExt{})

	rec := do(t, s, "POST", "/api/projects/blog/resources/fakedb/main/shell/ticket?arg=-c&arg=select+1", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("ticket: %d %s", rec.Code, rec.Body.String())
	}
	var minted struct {
		Ticket, Instance string
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &minted)
	if minted.Instance != "fakedb/main" {
		t.Errorf("instance = %q", minted.Instance)
	}
	tk, err := s.execTickets.redeem(minted.Ticket)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Pod != "main-1" || tk.Container != "db" || strings.Join(tk.Command, " ") != "dbcli app-blog -c select 1" || tk.Attach || tk.Launcher {
		t.Errorf("ticket = %+v", tk)
	}
	if rec := do(t, s, "POST", "/api/projects/blog/resources/fakedb/other/shell/ticket", "", true); rec.Code != http.StatusNotFound {
		t.Errorf("unknown resource: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "POST", "/api/projects/blog/resources/volume/main/shell/ticket", "", true); rec.Code != http.StatusNotFound {
		t.Errorf("a kind without a shell: %d %s", rec.Code, rec.Body.String())
	}
}

func TestResourceShellRunsTheTicketsCommandWithoutTheLauncher(t *testing.T) {
	f := newShellFixture(t, "psql (17)\n")
	code, err := f.s.execTickets.mint(execTicket{
		Identity: ext.Identity{Subject: "admin-token", Provider: "token", Admin: true},
		Project:  "blog", Instance: "postgres/db", Pod: "db-1", Container: "postgres", Command: []string{"psql", "-d", "app"},
	})
	if err != nil {
		t.Fatal(err)
	}
	c := f.dialWith(t, "postgres/db", code)
	defer c.Close()
	open := readControl(t, c)
	if open["type"] != "open" || open["instance"] != "postgres/db" || open["shell"] != "psql -d app" {
		t.Fatalf("open frame = %v", open)
	}
	c.Close()
	waitFor(t, "the exec to be recorded", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.command) > 0
	})
	f.mu.Lock()
	cmd := strings.Join(f.command, " ")
	f.mu.Unlock()
	if cmd != "psql -d app" {
		t.Errorf("command = %q (a resource's command never goes through the launcher)", cmd)
	}
}

func TestShellCommandMintedByTheAPIGoesThroughTheLauncher(t *testing.T) {
	f := newShellFixture(t, "")
	f.s.execRun = func(ctx context.Context, namespace, pod, container string, command []string) (string, error) {
		return "", nil // the image has the launcher
	}
	rec := do(t, f.s, "POST", "/api/projects/blog/shell/ticket?instance=web.1&cmd=rails&cmd=console", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("ticket: %d %s", rec.Code, rec.Body.String())
	}
	var minted struct{ Ticket string }
	_ = json.Unmarshal(rec.Body.Bytes(), &minted)
	c := f.dialWith(t, "web.1", minted.Ticket)
	defer c.Close()
	if open := readControl(t, c); open["shell"] != "rails console" {
		t.Fatalf("open frame = %v", open)
	}
	c.Close()
	waitFor(t, "the exec to be recorded", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.command) > 0
	})
	f.mu.Lock()
	cmd := strings.Join(f.command, " ")
	f.mu.Unlock()
	if cmd != cnbLauncher+" -- rails console" {
		t.Errorf("command = %q, want it through the launcher", cmd)
	}
}
