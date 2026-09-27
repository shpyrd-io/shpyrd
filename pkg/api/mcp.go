package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/version"
)

// The workspace's MCP server (RFC-0032, first slice): a remote Model
// Context Protocol endpoint at /mcp, Streamable HTTP transport, stateless
// JSON-RPC. An assistant such as Claude adds it as a connector, signs the
// person in through the workspace's OAuth 2.1 server (oauth.go), and asks
// about their projects: what runs, its status, recent logs, metrics. The
// tools call the platform's own API in-process with the same token, so
// they see exactly what the person may see and nothing more.

// mcpProtocolVersion is the newest protocol revision the server speaks;
// older clients name theirs at initialize and get it echoed when known.
const mcpProtocolVersion = "2025-06-18"

var mcpKnownVersions = map[string]bool{"2025-06-18": true, "2025-03-26": true, "2024-11-05": true}

// mcpServerName is what the assistant shows for this server: the
// workspace's choice, or "<workspace> on shpyrd".
func (s *Server) mcpServerName(c *gin.Context) string {
	ws, err := s.tenant(c)
	if err != nil {
		return "shpyrd"
	}
	if n := strings.TrimSpace(ws.Settings.MCPName); n != "" {
		return n
	}
	return firstNonEmpty(ws.Name, ws.Slug) + " on shpyrd"
}

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// mcpTool describes one tool to the assistant.
type mcpTool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

var mcpTools = []mcpTool{
	{
		Name: "list_projects", Title: "List projects",
		Description: "List the projects (apps) of this workspace the user can see, with their status and URL.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
		Annotations: map[string]any{"readOnlyHint": true},
	},
	{
		Name: "get_project", Title: "Project status",
		Description: "The status of one project: phase, URL, processes and their instances and sizes, the current release, access mode, custom domains.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"project": map[string]any{"type": "string", "description": "The project's slug (as in the URL) or display name."},
		}, "required": []string{"project"}, "additionalProperties": false},
		Annotations: map[string]any{"readOnlyHint": true},
	},
	{
		Name: "get_logs", Title: "Recent logs",
		Description: "The most recent log lines of a project's running instances (all processes unless one is named).",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"project": map[string]any{"type": "string", "description": "The project's slug or display name."},
			"lines":   map[string]any{"type": "integer", "minimum": 1, "maximum": 1000, "default": 100, "description": "How many lines, at most 1000."},
			"process": map[string]any{"type": "string", "description": "Only this process type (web, worker, ...)."},
		}, "required": []string{"project"}, "additionalProperties": false},
		Annotations: map[string]any{"readOnlyHint": true},
	},
	{
		Name: "get_metrics", Title: "Metrics",
		Description: "A project's metrics over a time range: requests, latency, errors, CPU and memory per process, summarised (latest, average, peak).",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"project": map[string]any{"type": "string", "description": "The project's slug or display name."},
			"range":   map[string]any{"type": "string", "enum": []string{"15m", "1h", "6h", "24h", "7d"}, "default": "1h"},
		}, "required": []string{"project"}, "additionalProperties": false},
		Annotations: map[string]any{"readOnlyHint": true},
	},
}

// mcpUnauthorized tells the client where to get a token (MCP authorization
// spec: the protected resource metadata in WWW-Authenticate).
func (s *Server) mcpUnauthorized(c *gin.Context, desc string) {
	meta := s.oauthIssuer(c) + "/.well-known/oauth-protected-resource"
	c.Header("WWW-Authenticate", fmt.Sprintf(`Bearer realm="shpyrd", resource_metadata="%s", error="invalid_token", error_description="%s"`, meta, desc))
	c.JSON(http.StatusUnauthorized, gin.H{"error": desc})
}

// mcpAuth authenticates MCP requests: one of our OAuth access tokens, or a
// personal API token (shp_...) for clients that can set a header. Anything
// else is told where the authorization server is.
func (s *Server) mcpAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.identifyWithToken(c) {
			c.Next()
			return
		}
		if s.identifyWithOAuth(c) {
			if !c.IsAborted() {
				c.Next()
			}
			return
		}
		s.mcpUnauthorized(c, "authorize this client through the workspace first")
		c.Abort()
	}
}

// mcpHandle is POST /mcp: the person's identity is on the context.
func (s *Server) mcpHandle(c *gin.Context) {
	if _, ok := ext.IdentityFrom(c); !ok {
		s.mcpUnauthorized(c, "sign in through the workspace to use this server")
		return
	}
	var raw json.RawMessage
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, jsonrpcResponse{JSONRPC: "2.0", Error: &jsonrpcError{Code: -32700, Message: "parse error"}})
		return
	}
	// A single message or a batch.
	var reqs []jsonrpcRequest
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		if err := json.Unmarshal(raw, &reqs); err != nil {
			c.JSON(http.StatusBadRequest, jsonrpcResponse{JSONRPC: "2.0", Error: &jsonrpcError{Code: -32700, Message: "parse error"}})
			return
		}
	} else {
		var one jsonrpcRequest
		if err := json.Unmarshal(raw, &one); err != nil {
			c.JSON(http.StatusBadRequest, jsonrpcResponse{JSONRPC: "2.0", Error: &jsonrpcError{Code: -32700, Message: "parse error"}})
			return
		}
		reqs = []jsonrpcRequest{one}
	}
	var responses []jsonrpcResponse
	for _, req := range reqs {
		if resp := s.mcpDispatch(c, req); resp != nil {
			responses = append(responses, *resp)
		}
	}
	c.Header("Cache-Control", "no-store")
	switch {
	case len(responses) == 0:
		c.Status(http.StatusAccepted) // notifications only
	case len(reqs) == 1:
		c.JSON(http.StatusOK, responses[0])
	default:
		c.JSON(http.StatusOK, responses)
	}
}

// mcpDispatch answers one message; notifications (no id) get nil.
func (s *Server) mcpDispatch(c *gin.Context, req jsonrpcRequest) *jsonrpcResponse {
	reply := func(result any) *jsonrpcResponse {
		if len(req.ID) == 0 || string(req.ID) == "null" {
			return nil
		}
		return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	}
	fail := func(code int, msg string) *jsonrpcResponse {
		if len(req.ID) == 0 || string(req.ID) == "null" {
			return nil
		}
		return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &jsonrpcError{Code: code, Message: msg}}
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		v := mcpProtocolVersion
		if mcpKnownVersions[p.ProtocolVersion] {
			v = p.ProtocolVersion
		}
		return reply(gin.H{
			"protocolVersion": v,
			"capabilities":    gin.H{"tools": gin.H{"listChanged": false}},
			"serverInfo":      gin.H{"name": s.mcpServerName(c), "version": version.Version},
			"instructions":    "This server answers about the projects (apps) of the workspace " + s.workspaceNameOf(c) + " on the shpyrd platform: list them, check their status, read recent logs and metrics. Name a project by its slug or display name.",
		})
	case "notifications/initialized", "notifications/cancelled", "notifications/roots/list_changed":
		return nil
	case "ping":
		return reply(gin.H{})
	case "tools/list":
		return reply(gin.H{"tools": mcpTools})
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return fail(-32602, "invalid params")
		}
		text, isErr := s.mcpCall(c, p.Name, p.Arguments)
		return reply(gin.H{"content": []gin.H{{"type": "text", "text": text}}, "isError": isErr})
	case "resources/list", "prompts/list":
		return reply(gin.H{strings.TrimSuffix(req.Method, "/list"): []any{}})
	}
	return fail(-32601, "method not found: "+req.Method)
}

func (s *Server) workspaceNameOf(c *gin.Context) string {
	if ws, err := s.tenant(c); err == nil {
		return firstNonEmpty(ws.Name, ws.Slug)
	}
	return "this workspace"
}

// apiCall performs one request against the platform's API in-process, as
// the same caller (the Authorization header travels along), so tools see
// what the person sees.
func (s *Server) apiCall(c *gin.Context, path string) (int, []byte) {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = c.Request.Host
	req.Header.Set("Authorization", c.GetHeader("Authorization"))
	req.Header.Set("X-Shpyrd-Token", c.GetHeader("X-Shpyrd-Token"))
	req.Header.Set("Accept", "application/json")
	req.RemoteAddr = c.Request.RemoteAddr
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// resolveProject maps a slug or a display name to a slug the person may see.
func (s *Server) resolveProject(c *gin.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("say which project")
	}
	code, body := s.apiCall(c, "/api/projects")
	if code != http.StatusOK {
		return "", fmt.Errorf("could not list projects: %s", apiErrorText(body))
	}
	var list []AppSummary
	if err := json.Unmarshal(body, &list); err != nil {
		return "", err
	}
	var names []string
	for _, a := range list {
		if strings.EqualFold(a.Slug, name) || strings.EqualFold(a.DisplayName, name) {
			return a.Slug, nil
		}
		names = append(names, a.Slug)
	}
	sort.Strings(names)
	return "", fmt.Errorf("no project %q among the ones you can see (%s)", name, firstNonEmpty(strings.Join(names, ", "), "none"))
}

func apiErrorText(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return e.Error
	}
	return strings.TrimSpace(string(body))
}

func argString(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		return fmt.Sprint(v)
	}
	return ""
}

// mcpCall runs one tool and renders its answer as text for the assistant.
func (s *Server) mcpCall(c *gin.Context, name string, args map[string]any) (string, bool) {
	switch name {
	case "list_projects":
		code, body := s.apiCall(c, "/api/projects")
		if code != http.StatusOK {
			return apiErrorText(body), true
		}
		var list []AppSummary
		if err := json.Unmarshal(body, &list); err != nil {
			return err.Error(), true
		}
		if len(list) == 0 {
			return "No projects you can see in " + s.workspaceNameOf(c) + ".", false
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d project(s) in %s:\n", len(list), s.workspaceNameOf(c))
		for _, a := range list {
			fmt.Fprintf(&b, "- %s (slug %s): %s", a.DisplayName, a.Slug, firstNonEmpty(a.Phase, "Pending"))
			if a.URL != "" {
				fmt.Fprintf(&b, ", %s", a.URL)
			}
			if a.Description != "" {
				fmt.Fprintf(&b, " — %s", a.Description)
			}
			b.WriteString("\n")
		}
		return b.String(), false
	case "get_project":
		slug, err := s.resolveProject(c, argString(args, "project"))
		if err != nil {
			return err.Error(), true
		}
		code, body := s.apiCall(c, "/api/projects/"+slug)
		if code != http.StatusOK {
			return apiErrorText(body), true
		}
		var d map[string]any
		if err := json.Unmarshal(body, &d); err != nil {
			return err.Error(), true
		}
		// A trimmed view: the detail carries build logs and internals the
		// assistant does not need.
		out := map[string]any{"slug": d["slug"], "name": d["displayName"], "description": d["description"], "createdAt": d["createdAt"]}
		if spec, ok := d["spec"].(map[string]any); ok {
			out["access"] = spec["access"]
			out["exposure"] = spec["exposure"]
			out["processes"] = spec["processes"]
			out["domains"] = spec["domains"]
		}
		if st, ok := d["status"].(map[string]any); ok {
			for _, k := range []string{"phase", "url", "message", "release", "instances", "hosts", "domains"} {
				if v, ok := st[k]; ok {
					out[k] = v
				}
			}
		}
		text, _ := json.MarshalIndent(out, "", "  ")
		return string(text), false
	case "get_logs":
		slug, err := s.resolveProject(c, argString(args, "project"))
		if err != nil {
			return err.Error(), true
		}
		lines := 100
		if v, ok := args["lines"]; ok {
			if n, err := strconv.Atoi(fmt.Sprint(v)); err == nil && n > 0 {
				lines = n
			}
		}
		if lines > 1000 {
			lines = 1000
		}
		q := "?tail=" + strconv.Itoa(lines)
		if p := argString(args, "process"); p != "" {
			q += "&process=" + p
		}
		code, body := s.apiCall(c, "/api/projects/"+slug+"/logs"+q)
		if code != http.StatusOK {
			return apiErrorText(body), true
		}
		text := strings.TrimSpace(string(body))
		if text == "" {
			return "No log lines yet for " + slug + ".", false
		}
		return fmt.Sprintf("Last %d line(s) of %s:\n%s", lines, slug, text), false
	case "get_metrics":
		slug, err := s.resolveProject(c, argString(args, "project"))
		if err != nil {
			return err.Error(), true
		}
		rng := firstNonEmpty(argString(args, "range"), "1h")
		code, body := s.apiCall(c, "/api/projects/"+slug+"/metrics?range="+rng)
		if code != http.StatusOK {
			return apiErrorText(body), true
		}
		var m MetricsResponse
		if err := json.Unmarshal(body, &m); err != nil {
			return err.Error(), true
		}
		return summarizeMetrics(slug, m), false
	}
	return "unknown tool " + name, true
}

// summarizeMetrics renders charts as a few numbers per series: latest,
// average and peak over the range, in the chart's unit.
func summarizeMetrics(slug string, m MetricsResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Metrics of %s over the last %s:\n", slug, m.Range)
	empty := true
	for _, ch := range m.Charts {
		if ch.Error != "" {
			fmt.Fprintf(&b, "- %s: unavailable (%s)\n", ch.Title, ch.Error)
			continue
		}
		for _, se := range ch.Series {
			var sum, peak, latest float64
			n := 0
			for _, p := range se.Points {
				v := p[1]   // [timestamp, value]
				if v != v { // NaN: no sample
					continue
				}
				sum += v
				if v > peak {
					peak = v
				}
				latest = v
				n++
			}
			if n == 0 {
				continue
			}
			empty = false
			name := ch.Title
			if se.Name != "" && se.Name != ch.Title {
				name += " / " + se.Name
			}
			fmt.Fprintf(&b, "- %s: latest %s, average %s, peak %s", name, formatUnit(latest, ch.Unit), formatUnit(sum/float64(n), ch.Unit), formatUnit(peak, ch.Unit))
			if se.Reference > 0 {
				fmt.Fprintf(&b, " (limit %s)", formatUnit(se.Reference, ch.Unit))
			}
			b.WriteString("\n")
		}
	}
	if empty {
		b.WriteString("No data points in this range (the project may have no running instances, or metrics are not enabled).\n")
	}
	return b.String()
}

func formatUnit(v float64, unit string) string {
	switch unit {
	case "bytes":
		return humanBytes(v)
	case "bytes/s":
		return humanBytes(v) + "/s"
	case "ms":
		return fmt.Sprintf("%.0f ms", v)
	case "cores":
		return fmt.Sprintf("%.3f cores", v)
	case "rps":
		return fmt.Sprintf("%.2f req/s", v)
	}
	return strconv.FormatFloat(v, 'f', -1, 64) + " " + unit
}

func humanBytes(v float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

// mcpOther answers GET (no server-initiated stream: 405) and DELETE
// (nothing to end: 200) on /mcp.
func (s *Server) mcpOther(c *gin.Context) {
	if c.Request.Method == http.MethodDelete {
		c.Status(http.StatusOK)
		return
	}
	c.Header("Allow", "POST, DELETE")
	c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "this MCP server speaks Streamable HTTP without server-initiated streams: POST JSON-RPC messages"})
}

// updateMCPName is part of PATCH /api/workspace: the server's name as the
// assistant shows it.
func validMCPName(name string) error {
	name = strings.TrimSpace(name)
	if len(name) > 60 {
		return fmt.Errorf("the MCP name is at most 60 characters")
	}
	if strings.ContainsAny(name, "\n\r\t") {
		return fmt.Errorf("the MCP name is one line")
	}
	return nil
}

var _ = store.DefaultWorkspace
