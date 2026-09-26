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
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/gorilla/websocket"
	"golang.org/x/term"

	"github.com/shpyrd-io/shpyrd/pkg/api"
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

// shellAPI runs an interactive shell in an instance over the WebSocket
// bridge. Commands cannot be passed: the bridge opens the image's shell.
func (a *appClient) shellAPI(ctx context.Context, slug string, inst api.Instance, stderr io.Writer) error {
	// 1. A ticket, bound to the project and the instance (30 s, single use).
	raw, err := a.serverRequest(ctx, "POST", "api/projects/"+slug+"/shell/ticket?instance="+url.QueryEscape(inst.Name), nil, "")
	if err != nil {
		return err
	}
	var minted struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(raw, &minted); err != nil || minted.Ticket == "" {
		return errors.New("the server issued no shell ticket")
	}
	// 2. The WebSocket at the workspace host.
	base := a.workspaceURL()
	if base == "" {
		return errors.New("no workspace URL for the shell (sign in with `shpyrd login`)")
	}
	wsURL := strings.Replace(strings.Replace(base, "https://", "wss://", 1), "http://", "ws://", 1)
	wsURL += "/api/projects/" + slug + "/shell?" + url.Values{"ticket": {minted.Ticket}, "instance": {inst.Name}}.Encode()
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, wsURL, http.Header{})
	if err != nil {
		if resp != nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			return fmt.Errorf("shell: %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		return fmt.Errorf("shell: %w", err)
	}
	defer conn.Close()

	// 3. The local terminal, raw while the shell runs.
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
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for range winch {
			sendSize()
		}
	}()

	// stdin → binary frames.
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
				return
			}
		}
	}()

	// frames → stdout, control frames handled.
	exitCode := 0
	for {
		typ, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) || ctx.Err() != nil {
				break
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
				fmt.Fprintf(stderr, "Connected to %s (%s). Type exit to leave.\r\n", f.Instance, firstNonEmpty(f.Shell, "shell"))
			case "exit":
				if f.Code != nil {
					exitCode = *f.Code
				}
				if restore != nil {
					restore()
					restore = nil
				}
				if exitCode != 0 {
					return fmt.Errorf("shell exited with code %d", exitCode)
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
	return nil
}

// workspaceURL is the URL of the workspace this client talks to (session
// mode): SHPYRD_URL or the current login.
func (a *appClient) workspaceURL() string {
	if u := os.Getenv("SHPYRD_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return loadSessions().activeURL()
}
