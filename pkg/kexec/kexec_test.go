package kexec

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	kexec "k8s.io/client-go/util/exec"

	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

func TestRemoteExit(t *testing.T) {
	err := RemoteExit(kexec.CodeExitError{Err: errors.New("command terminated with exit code 3"), Code: 3})
	if ExitCode(err) != 3 {
		t.Errorf("exit code = %d, want 3", ExitCode(err))
	}
	plain := errors.New("boom")
	if RemoteExit(plain) != plain || ExitCode(plain) != 1 {
		t.Error("other errors pass through and exit 1")
	}
	if RemoteExit(nil) != nil {
		t.Error("nil stays nil")
	}
}

func TestIsNotFound(t *testing.T) {
	for _, m := range []string{
		`OCI runtime exec failed: exec failed: unable to start container process: exec: "bash": executable file not found in $PATH: unknown`,
		"command terminated with exit code 126",
		"command terminated with exit code 127",
	} {
		if !IsNotFound(errors.New(m)) {
			t.Errorf("should be treated as missing executable: %s", m)
		}
	}
	if IsNotFound(errors.New("command terminated with exit code 2")) || IsNotFound(nil) {
		t.Error("ordinary failures are not missing executables")
	}
}

func TestChanSizeQueue(t *testing.T) {
	ch := make(chan remotecommand.TerminalSize, 1)
	q := &chanSizeQueue{ch: ch}
	ch <- remotecommand.TerminalSize{Width: 120, Height: 40}
	got := q.Next()
	if got == nil || got.Width != 120 || got.Height != 40 {
		t.Fatalf("Next() = %+v, want 120x40", got)
	}
	// A closed channel ends the queue: remotecommand stops its size
	// goroutine on nil, which is how the server avoids leaking one per
	// finished session.
	close(ch)
	if q.Next() != nil {
		t.Error("Next() after close must be nil so the size goroutine exits")
	}
}

func TestExecURL(t *testing.T) {
	// A real clientset, not the fake one: the fake's RESTClient() returns a
	// nil *rest.RESTClient and .Post() panics on it. A real client built from
	// a dummy Host never opens a connection — it only has to build a URL.
	cfg := &rest.Config{Host: "https://api.example.test"}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	k := &kube.Client{Kube: cs, Config: cfg}
	u := ExecURL(k, "app-blog", "blog-web-1", "app", []string{"bash"}, true)
	for _, want := range []string{"/namespaces/app-blog/pods/blog-web-1/exec", "container=app", "command=bash", "tty=true", "stdin=true"} {
		if !strings.Contains(u, want) {
			t.Errorf("ExecURL() = %q, missing %q", u, want)
		}
	}
	// With a TTY the remote side must not get a separate stderr stream:
	// there is only one terminal to write to.
	if strings.Contains(u, "stderr=true") {
		t.Errorf("ExecURL() with tty must not request stderr: %q", u)
	}
}

// The two streams are copied concurrently; every line of stdout must come
// back whatever stderr does at the same time, and stderr explains a failure.
func TestCollectKeepsStdoutWholeNextToStderr(t *testing.T) {
	const lines = 5000
	out, err := collect(func(o remotecommand.StreamOptions) error {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < lines; i++ {
				fmt.Fprintf(o.Stdout, "%d\n", i)
			}
		}()
		go func() {
			defer wg.Done()
			io.WriteString(o.Stderr, "")
			io.WriteString(o.Stderr, "NOTICE: nothing\n")
		}()
		wg.Wait()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(out, "\n"); got != lines {
		t.Errorf("stdout came back with %d of %d lines", got, lines)
	}
	if strings.Contains(out, "NOTICE") {
		t.Error("stderr must not be mixed into stdout")
	}
	_, err = collect(func(o remotecommand.StreamOptions) error {
		io.WriteString(o.Stderr, "psql: error: connection refused\n")
		return kexec.CodeExitError{Err: errors.New("command terminated with exit code 2"), Code: 2}
	})
	if err == nil || !strings.Contains(err.Error(), "connection refused") || ExitCode(RemoteExit(err)) != 2 {
		t.Errorf("a failure carries stderr and keeps its exit code: %v", err)
	}
}
