package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"golang.org/x/term"

	"github.com/shpyrd-io/shpyrd/pkg/api"
	"github.com/shpyrd-io/shpyrd/pkg/kexec"
)

// The shell over the API (RFC-0026's bridge, RFC-0052's transport): signed
// in with `shpyrd login`, the CLI has no cluster to exec into, so it does
// what the dashboard's terminal does — mints a shell ticket for one
// instance, opens the WebSocket at the workspace, and bridges the local
// terminal: binary frames are bytes, text frames are control messages
// (open, resize, exit, error).

// shellFrame mirrors the server's control frame.
type shellFrame struct {
	Type     string `json:"type"`
	Instance string `json:"instance,omitempty"`
	Shell    string `json:"shell,omitempty"`
	Cols     uint16 `json:"cols,omitempty"`
	Rows     uint16 `json:"rows,omitempty"`
	Code     *int   `json:"code,omitempty"`
	Message  string `json:"message,omitempty"`
}

// pickInstanceAPI chooses the instance a shell attaches to through the API:
// the one named, else the first ready one of the process (web by default).
func (a *appClient) pickInstanceAPI(ctx context.Context, slug, process, instance string) (api.Instance, error) {
	raw, err := a.serverRequest(ctx, "GET", "api/projects/"+slug+"/instances", nil, "")
	if err != nil {
		return api.Instance{}, err
	}
	var list []api.Instance
	if err := json.Unmarshal(raw, &list); err != nil {
		return api.Instance{}, err
	}
	if len(list) == 0 {
		return api.Instance{}, errors.New("no running instances in this project")
	}
	if instance != "" {
		for _, i := range list {
			if i.Name == instance {
				return i, nil
			}
		}
		return api.Instance{}, fmt.Errorf("no instance %q; running: %s", instance, instanceNames(list))
	}
	want := firstNonEmpty(process, "web")
	var fallback *api.Instance
	for i := range list {
		if list[i].Process != want {
			continue
		}
		if list[i].Ready {
			return list[i], nil
		}
		if fallback == nil {
			fallback = &list[i]
		}
	}
	if fallback != nil {
		return *fallback, nil
	}
	if process == "" {
		// No web process: the first instance there is.
		return list[0], nil
	}
	return api.Instance{}, fmt.Errorf("no running instance of process %q; running: %s", process, instanceNames(list))
}

func instanceNames(list []api.Instance) string {
	names := make([]string, 0, len(list))
	for _, i := range list {
		names = append(names, i.Name)
	}
	return strings.Join(names, ", ")
}

// shellAPI runs an interactive shell — or a command — in an instance over
// the WebSocket bridge.
func (a *appClient) shellAPI(ctx context.Context, slug string, inst api.Instance, command []string, stderr io.Writer) error {
	// A ticket, bound to the project and the instance (30 s, single use),
	// carrying the command when there is one.
	q := url.Values{"instance": {inst.Name}}
	for _, arg := range command {
		q.Add("cmd", arg)
	}
	ticket, err := a.mintTicket(ctx, "api/projects/"+slug+"/shell/ticket?"+q.Encode())
	if err != nil {
		return err
	}
	return a.bridge(ctx, slug, ticket, inst.Name, len(command) > 0, false, stderr)
}

// execResourceAPI is a shell into a resource (`shpyrd pg psql`, `shpyrd
// redis cli`) over the bridge: the server names the pod and the command.
func (a *appClient) execResourceAPI(ctx context.Context, slug, kind, name string, args []string, stderr io.Writer) error {
	q := url.Values{}
	for _, arg := range args {
		q.Add("arg", arg)
	}
	raw, err := a.serverRequest(ctx, "POST", "api/projects/"+slug+"/resources/"+kind+"/"+name+"/shell/ticket?"+q.Encode(), nil, "")
	if err != nil {
		return err
	}
	var minted struct {
		Ticket   string `json:"ticket"`
		Instance string `json:"instance"`
	}
	if err := json.Unmarshal(raw, &minted); err != nil || minted.Ticket == "" {
		return errors.New("the server issued no shell ticket")
	}
	return a.bridge(ctx, slug, minted.Ticket, minted.Instance, true, false, stderr)
}

// runAPI is `shpyrd run` over the API: the server starts the one-off
// instance; attached, the bridge follows it until it exits and the exit
// code comes back as the CLI's own.
func (a *appClient) runAPI(ctx context.Context, slug string, command []string, size string, detach, tty bool, out, stderr io.Writer) error {
	body, _ := json.Marshal(api.RunRequest{Command: command, Size: size, Detach: detach, TTY: tty})
	raw, err := a.serverRequest(ctx, "POST", "api/projects/"+slug+"/run", body, "application/json")
	if err != nil {
		return err
	}
	var res api.RunResult
	if err := json.Unmarshal(raw, &res); err != nil || res.Name == "" {
		return fmt.Errorf("unexpected response: %s", truncate(string(raw), 200))
	}
	if detach {
		fmt.Fprintf(out, "Started %s (follow with `shpyrd logs --process run --project %s`)\n", res.Name, slug)
		return nil
	}
	fmt.Fprintf(stderr, "Running on %s (%s)...\n", res.Name, res.Size)
	if tty {
		fmt.Fprintln(stderr, "If you don't see a prompt, try pressing enter.")
	}
	return a.bridge(ctx, slug, res.Ticket, res.Name, true, true, stderr)
}

// mintTicket asks for a shell ticket at path.
func (a *appClient) mintTicket(ctx context.Context, path string) (string, error) {
	raw, err := a.serverRequest(ctx, "POST", path, nil, "")
	if err != nil {
		return "", err
	}
	var minted struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(raw, &minted); err != nil || minted.Ticket == "" {
		return "", errors.New("the server issued no shell ticket")
	}
	return minted.Ticket, nil
}

// bridge opens the WebSocket at the workspace with a minted ticket and
// bridges the local terminal to it until the remote side ends. quiet
// skips the "Connected to" line (a command, not a shell). pipe says the
// remote command owns its stdin (a one-off run): when the local stdin
// ends, the server is told, once the session is open, so the command
// sees EOF; a shell's stdin is never closed from here, since closing an
// exec session's stdin ends it. The remote exit code comes back as a
// *kexec.ExitError, so the CLI exits with it.
func (a *appClient) bridge(ctx context.Context, slug, ticket, instance string, quiet, pipe bool, stderr io.Writer) error {
	base := a.workspaceURL()
	if base == "" {
		return errors.New("no workspace URL for the shell (sign in with `shpyrd login`)")
	}
	wsURL := strings.Replace(strings.Replace(base, "https://", "wss://", 1), "http://", "ws://", 1)
	wsURL += "/api/projects/" + slug + "/shell?" + url.Values{"ticket": {ticket}, "instance": {instance}}.Encode()
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, wsURL, http.Header{})
	if err != nil {
		if resp != nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			return fmt.Errorf("shell: %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		return fmt.Errorf("shell: %w", err)
	}
	defer conn.Close()

	// The local terminal, raw while the shell runs.
	fd := int(os.Stdin.Fd())
	isTTY := term.IsTerminal(fd)
	var restore func()
	if isTTY {
		state, err := term.MakeRaw(fd)
		if err == nil {
			restore = func() { _ = term.Restore(fd, state) }
			defer restore()
		}
	}
	var writeMu sync.Mutex
	control := func(f shellFrame) error {
		b, _ := json.Marshal(f)
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(websocket.TextMessage, b)
	}
	sendSize := func() {
		if !isTTY {
			return
		}
		if cols, rows, err := term.GetSize(fd); err == nil {
			_ = control(shellFrame{Type: "resize", Cols: uint16(cols), Rows: uint16(rows)})
		}
	}
	sendSize()
	stopResize := watchResize(sendSize) // per OS: resize_unix.go, resize_windows.go
	defer stopResize()

	// stdin → binary frames. Piped input that reaches EOF is reported with
	// an eof frame, once the session is open, so the remote command sees
	// its stdin close (a one-off run only).
	opened := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				writeMu.Lock()
				werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n])
				writeMu.Unlock()
				if werr != nil {
					return
				}
			}
			if err != nil {
				if pipe && !isTTY {
					select {
					case <-opened:
					case <-ctx.Done():
						return
					}
					_ = control(shellFrame{Type: "eof"})
				}
				return
			}
		}
	}()

	// frames → stdout, control frames handled.
	for {
		typ, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) || ctx.Err() != nil {
				return nil
			}
			if restore != nil {
				restore()
			}
			return fmt.Errorf("shell: connection lost: %w", err)
		}
		switch typ {
		case websocket.BinaryMessage:
			_, _ = os.Stdout.Write(data)
		case websocket.TextMessage:
			var f shellFrame
			if json.Unmarshal(data, &f) != nil {
				_, _ = os.Stdout.Write(data)
				continue
			}
			switch f.Type {
			case "open":
				close(opened)
				if !quiet {
					fmt.Fprintf(stderr, "Connected to %s (%s). Type exit to leave.\r\n", f.Instance, firstNonEmpty(f.Shell, "shell"))
				}
			case "exit":
				if restore != nil {
					restore()
					restore = nil
				}
				if f.Code != nil && *f.Code != 0 {
					return &kexec.ExitError{Code: *f.Code}
				}
				return nil
			case "error":
				if restore != nil {
					restore()
					restore = nil
				}
				return fmt.Errorf("shell: %s", f.Message)
			}
		}
	}
}

// workspaceURL is the URL of the workspace this client talks to (session
// mode): SHPYRD_URL or the current login.
func (a *appClient) workspaceURL() string {
	if u := os.Getenv("SHPYRD_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return loadSessions().activeURL()
}
