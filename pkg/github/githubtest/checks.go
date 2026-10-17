package githubtest

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Checks, statuses and Deployments (RFC-0054): what the source watcher
// reads and the reporter writes. Each repository call needs a token of an
// installation that grants the repository (404 otherwise) with the
// permission GitHub asks for (403 otherwise).

// File is a file a compare lists; Previous is its old name when renamed.
type File struct{ Name, Previous string }

// Compare is GitHub's answer to a compare.
type Compare struct {
	Status string
	Files  []File
}

// CheckRun is a check run on a commit: other tools' are set with
// SetCheckRuns, shpyrd's are made through the API (AppID is then AppID).
type CheckRun struct {
	ID                       int64
	Name, Status, Conclusion string
	AppID                    int64
	HeadSHA, Title, Summary  string
	DetailsURL, ExternalID   string
	viaAPI                   bool
}

// Status is a commit status.
type Status struct{ Context, State string }

// Deployment is one made through the API (AppID is then AppID), or added
// with AddDeployment, with its statuses in order. App and Release are
// what its payload says it is for.
type Deployment struct {
	ID                            int64
	Ref, Environment, Description string
	AutoMerge, Transient          bool
	RequiredContexts              []string
	AppID                         int64
	App                           string
	Release                       int
	Statuses                      []DeploymentStatus
}

// DeploymentStatus is one status of a Deployment.
type DeploymentStatus struct{ State, EnvironmentURL, LogURL string }

// Write is a call that would change something, refused or not, with the
// permissions of the token that made it (nil without one).
type Write struct {
	Method, Path string
	Permissions  map[string]string
}

func compareKey(fullName, base, head string) string {
	return strings.ToLower(fullName) + " " + strings.ToLower(base) + "..." + strings.ToLower(head)
}

// SetCompare is GitHub's answer to base...head on the repository.
func (f *Server) SetCompare(fullName, base, head string, c Compare) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.compares[compareKey(fullName, base, head)] = c
}

// SetCheckRuns sets the check runs other tools made on sha; the ones made
// through the API stay.
func (f *Server) SetCheckRuns(sha string, runs ...CheckRun) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sha = strings.ToLower(sha)
	kept := []CheckRun{}
	for _, r := range f.runs[sha] {
		if r.viaAPI {
			kept = append(kept, r)
		}
	}
	for _, r := range runs {
		f.nextID++
		r.ID, r.HeadSHA, r.viaAPI = f.nextID, sha, false
		kept = append(kept, r)
	}
	f.runs[sha] = kept
}

// SetStatuses sets the statuses on sha.
func (f *Server) SetStatuses(sha string, statuses ...Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses[strings.ToLower(sha)] = statuses
}

// CheckRunsOn are the check runs on sha, other tools' and shpyrd's.
func (f *Server) CheckRunsOn(sha string) []CheckRun {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]CheckRun(nil), f.runs[strings.ToLower(sha)]...)
}

// DeleteCheckRun forgets a check run, as when it is deleted on GitHub.
func (f *Server) DeleteCheckRun(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for sha, runs := range f.runs {
		for i := range runs {
			if runs[i].ID == id {
				f.runs[sha] = append(runs[:i:i], runs[i+1:]...)
				return
			}
		}
	}
}

// Deployments are the Deployments made, oldest first.
func (f *Server) Deployments() []Deployment {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Deployment, len(f.deployments))
	for i, d := range f.deployments {
		d.Statuses = append([]DeploymentStatus(nil), d.Statuses...)
		out[i] = d
	}
	return out
}

// AddDeployment adds a Deployment made elsewhere (another App's, or one
// made before) and gives its id.
func (f *Server) AddDeployment(d Deployment) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	d.ID = f.nextID
	f.deployments = append(f.deployments, d)
	return d.ID
}

// DeleteDeployment forgets a Deployment, as when it is deleted on GitHub.
func (f *Server) DeleteDeployment(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.deployments {
		if f.deployments[i].ID == id {
			f.deployments = append(f.deployments[:i:i], f.deployments[i+1:]...)
			return
		}
	}
}

// Writes are the POST and PATCH calls on repositories, in order.
func (f *Server) Writes() []Write {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Write(nil), f.writes...)
}

// SetCheckRunsTotal makes the check-runs listing of sha report a total_count
// above what it serves, as GitHub does for a commit with more runs.
func (f *Server) SetCheckRunsTotal(sha string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.totals["runs "+strings.ToLower(sha)] = n
}

// SetStatusesTotal is SetCheckRunsTotal for the commit status listing.
func (f *Server) SetStatusesTotal(sha string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.totals["statuses "+strings.ToLower(sha)] = n
}

// RefusePermission makes token requests that ask for the permission answer
// 422, as GitHub does while the installation has not approved it.
func (f *Server) RefusePermission(perm string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refused[perm] = true
}

// LastCompareQuery is the query string of the last compare.
func (f *Server) LastCompareQuery() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.compareQuery
}

func (f *Server) total(kind, sha string, n int) int {
	if t, ok := f.totals[kind+" "+strings.ToLower(sha)]; ok {
		return t
	}
	return n
}

// may says the token has the permission at the level (write covers read).
func may(g tokenGrant, perm, level string) bool {
	v := g.perms[perm]
	return v == "write" || (level == "read" && v == "read")
}

func names(repos []string, name string) bool {
	for _, r := range repos {
		if strings.EqualFold(r, name) {
			return true
		}
	}
	return false
}

type checkRunBody struct {
	Name       string `json:"name"`
	HeadSHA    string `json:"head_sha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	DetailsURL string `json:"details_url"`
	ExternalID string `json:"external_id"`
	Output     struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
	} `json:"output"`
}

func (b checkRunBody) apply(r *CheckRun) {
	for _, set := range []struct {
		to   *string
		from string
	}{{&r.Status, b.Status}, {&r.Conclusion, b.Conclusion}, {&r.DetailsURL, b.DetailsURL}, {&r.ExternalID, b.ExternalID},
		{&r.Title, b.Output.Title}, {&r.Summary, b.Output.Summary}} {
		if set.from != "" {
			*set.to = set.from
		}
	}
}

// repoCall answers /repos/{owner}/{repo}/...: a compare, a commit's check
// runs and status, and the check runs and Deployments written.
func (f *Server) repoCall(w http.ResponseWriter, r *http.Request, p string) {
	parts := strings.SplitN(strings.TrimPrefix(p, "/repos/"), "/", 3)
	if len(parts) < 3 {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	fullName, rest := parts[0]+"/"+parts[1], parts[2]
	grant, hasToken := f.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	if r.Method != "GET" {
		var perms map[string]string
		if hasToken {
			perms = grant.perms
		}
		f.writes = append(f.writes, Write{Method: r.Method, Path: p, Permissions: perms})
	}
	if !hasToken || !grants(f.repos[grant.installation], parts[1]) || (len(grant.repos) > 0 && !names(grant.repos, parts[1])) {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	need := func(perm, level string) bool {
		if may(grant, perm, level) {
			return true
		}
		reply(w, http.StatusForbidden, map[string]string{"message": "Resource not accessible by integration"})
		return false
	}
	seg := strings.Split(rest, "/")
	raw, _ := io.ReadAll(r.Body)
	switch {
	case r.Method == "GET" && len(seg) == 2 && seg[0] == "compare":
		if !need("contents", "read") {
			return
		}
		f.compareQuery = r.URL.RawQuery
		base, head, _ := strings.Cut(seg[1], "...")
		c, ok := f.compares[compareKey(fullName, base, head)]
		if !ok {
			reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
			return
		}
		files := []map[string]string{}
		for _, fl := range c.Files {
			m := map[string]string{"filename": fl.Name, "status": "modified"}
			if fl.Previous != "" {
				m["previous_filename"], m["status"] = fl.Previous, "renamed"
			}
			files = append(files, m)
		}
		reply(w, http.StatusOK, map[string]any{"status": c.Status, "files": files})
	case r.Method == "GET" && len(seg) == 3 && seg[0] == "commits" && seg[2] == "check-runs":
		if !need("checks", "read") {
			return
		}
		runs := f.runs[strings.ToLower(seg[1])]
		if name := r.URL.Query().Get("check_name"); name != "" {
			var named []CheckRun
			for _, run := range runs {
				if run.Name == name {
					named = append(named, run)
				}
			}
			runs = named
		}
		out := []map[string]any{}
		for _, run := range pageOf(runs, r) {
			var conclusion any
			if run.Conclusion != "" {
				conclusion = run.Conclusion
			}
			out = append(out, map[string]any{"id": run.ID, "name": run.Name, "status": run.Status, "conclusion": conclusion, "external_id": run.ExternalID, "app": map[string]any{"id": run.AppID}})
		}
		reply(w, http.StatusOK, map[string]any{"total_count": f.total("runs", seg[1], len(runs)), "check_runs": out})
	case r.Method == "GET" && len(seg) == 3 && seg[0] == "commits" && seg[2] == "status":
		if !need("statuses", "read") {
			return
		}
		all := f.statuses[strings.ToLower(seg[1])]
		state := "success"
		if len(all) == 0 {
			state = "pending"
		}
		out := []map[string]string{}
		for _, s := range all {
			switch {
			case s.State == "failure" || s.State == "error":
				state = "failure"
			case s.State == "pending" && state != "failure":
				state = "pending"
			}
		}
		for _, s := range pageOf(all, r) {
			out = append(out, map[string]string{"context": s.Context, "state": s.State})
		}
		reply(w, http.StatusOK, map[string]any{"state": state, "total_count": f.total("statuses", seg[1], len(all)), "statuses": out})
	case r.Method == "POST" && rest == "check-runs":
		if !need("checks", "write") {
			return
		}
		var in checkRunBody
		_ = json.Unmarshal(raw, &in)
		f.nextID++
		run := CheckRun{ID: f.nextID, Name: in.Name, HeadSHA: strings.ToLower(in.HeadSHA), AppID: AppID, viaAPI: true}
		in.apply(&run)
		f.runs[run.HeadSHA] = append(f.runs[run.HeadSHA], run)
		reply(w, http.StatusCreated, map[string]any{"id": run.ID})
	case r.Method == "PATCH" && len(seg) == 2 && seg[0] == "check-runs":
		if !need("checks", "write") {
			return
		}
		id, _ := strconv.ParseInt(seg[1], 10, 64)
		var in checkRunBody
		_ = json.Unmarshal(raw, &in)
		for sha := range f.runs {
			for i := range f.runs[sha] {
				if f.runs[sha][i].ID == id {
					in.apply(&f.runs[sha][i])
					reply(w, http.StatusOK, map[string]any{"id": id})
					return
				}
			}
		}
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	case r.Method == "POST" && rest == "deployments":
		if !need("deployments", "write") {
			return
		}
		var in struct {
			Ref              string   `json:"ref"`
			Environment      string   `json:"environment"`
			Description      string   `json:"description"`
			AutoMerge        bool     `json:"auto_merge"`
			RequiredContexts []string `json:"required_contexts"`
			Transient        bool     `json:"transient_environment"`
			Payload          struct {
				App     string `json:"app"`
				Release int    `json:"release"`
			} `json:"payload"`
		}
		_ = json.Unmarshal(raw, &in)
		f.nextID++
		f.deployments = append(f.deployments, Deployment{ID: f.nextID, Ref: in.Ref, Environment: in.Environment, Description: in.Description,
			AutoMerge: in.AutoMerge, Transient: in.Transient, RequiredContexts: in.RequiredContexts, AppID: AppID, App: in.Payload.App, Release: in.Payload.Release})
		reply(w, http.StatusCreated, map[string]any{"id": f.nextID})
	case r.Method == "GET" && rest == "deployments":
		if !need("deployments", "read") {
			return
		}
		// Newest first, as GitHub lists them.
		env := r.URL.Query().Get("environment")
		out := []map[string]any{}
		for i := len(f.deployments) - 1; i >= 0; i-- {
			d := f.deployments[i]
			if env != "" && d.Environment != env {
				continue
			}
			m := map[string]any{"id": d.ID, "ref": d.Ref, "environment": d.Environment, "payload": map[string]any{"app": d.App, "release": d.Release}}
			if d.AppID != 0 {
				m["performed_via_github_app"] = map[string]any{"id": d.AppID}
			}
			out = append(out, m)
		}
		reply(w, http.StatusOK, out)
	case r.Method == "GET" && len(seg) == 3 && seg[0] == "deployments" && seg[2] == "statuses":
		if !need("deployments", "read") {
			return
		}
		id, _ := strconv.ParseInt(seg[1], 10, 64)
		for _, d := range f.deployments {
			if d.ID == id {
				out := []map[string]string{}
				for i := len(d.Statuses) - 1; i >= 0; i-- {
					out = append(out, map[string]string{"state": d.Statuses[i].State})
				}
				reply(w, http.StatusOK, out)
				return
			}
		}
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	case r.Method == "POST" && len(seg) == 3 && seg[0] == "deployments" && seg[2] == "statuses":
		if !need("deployments", "write") {
			return
		}
		id, _ := strconv.ParseInt(seg[1], 10, 64)
		var in struct {
			State          string `json:"state"`
			EnvironmentURL string `json:"environment_url"`
			LogURL         string `json:"log_url"`
		}
		_ = json.Unmarshal(raw, &in)
		for i := range f.deployments {
			if f.deployments[i].ID == id {
				f.deployments[i].Statuses = append(f.deployments[i].Statuses, DeploymentStatus{State: in.State, EnvironmentURL: in.EnvironmentURL, LogURL: in.LogURL})
				f.nextID++
				reply(w, http.StatusCreated, map[string]any{"id": f.nextID})
				return
			}
		}
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	default:
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	}
}
