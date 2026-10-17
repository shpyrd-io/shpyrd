package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// A repository's checks, statuses and Deployments (RFC-0054): the source
// watcher reads with WatchRead, the reporter writes with ReportWrite, each
// with a token scoped to the one repository.

// maxCompareFiles is how many files GitHub's compare lists at most.
const maxCompareFiles = 300

// ErrTooManyChecks: a commit has more check runs or statuses than shpyrd
// reads. A partial list could hide a failure, so it is never used.
var ErrTooManyChecks = errors.New("more checks than shpyrd reads")

// IsNotFound says GitHub answered 404: a check run or Deployment deleted
// on GitHub, or a commit it does not know.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

// repoURL is the API's address of a repository's path.
func (c *Client) repoURL(owner, repo, path string) string {
	return fmt.Sprintf("%s/repos/%s/%s/%s", c.api, url.PathEscape(owner), url.PathEscape(repo), path)
}

// Comparison is GitHub's compare of two commits: ahead, behind, identical
// or diverged, and every file changed, a renamed one under both names.
type Comparison struct {
	Status string
	Files  []string
	// Capped: GitHub listed as many files as it lists; there may be more.
	Capped bool
}

// Compare compares base with head (base...head).
func (c *Client) Compare(ctx context.Context, installation int64, owner, repo, base, head string) (Comparison, error) {
	tok, err := c.InstallationToken(ctx, installation, repo, WatchRead())
	if err != nil {
		return Comparison{}, err
	}
	var res struct {
		Status string `json:"status"`
		Files  []struct {
			Filename string `json:"filename"`
			Previous string `json:"previous_filename"`
		} `json:"files"`
	}
	if err := c.call(ctx, "GET", c.repoURL(owner, repo, "compare/"+url.PathEscape(base)+"..."+url.PathEscape(head)+"?per_page=1"), tok.Value, nil, &res); err != nil {
		return Comparison{}, err
	}
	out := Comparison{Status: res.Status, Capped: len(res.Files) >= maxCompareFiles}
	for _, f := range res.Files {
		out.Files = append(out.Files, f.Filename)
		if f.Previous != "" {
			out.Files = append(out.Files, f.Previous)
		}
	}
	return out, nil
}

// CheckRuns are the check runs on a commit, every App's.
func (c *Client) CheckRuns(ctx context.Context, installation int64, owner, repo, sha string) ([]CheckRun, error) {
	return c.checkRuns(ctx, installation, owner, repo, sha, "", WatchRead())
}

// ReportCheckRuns are the check runs named name on a commit, read with the
// reporter's token (checks write reads them too): the reporter finds a run
// it made before it makes another.
func (c *Client) ReportCheckRuns(ctx context.Context, installation int64, owner, repo, sha, name string) ([]CheckRun, error) {
	return c.checkRuns(ctx, installation, owner, repo, sha, name, ReportWrite())
}

func (c *Client) checkRuns(ctx context.Context, installation int64, owner, repo, sha, name string, perms Permissions) ([]CheckRun, error) {
	tok, err := c.InstallationToken(ctx, installation, repo, perms)
	if err != nil {
		return nil, err
	}
	filter := ""
	if name != "" {
		filter = "&check_name=" + url.QueryEscape(name)
	}
	out := []CheckRun{}
	total := 0
	for page := 1; page <= 10; page++ {
		var res struct {
			Total int `json:"total_count"`
			Runs  []struct {
				ID         int64  `json:"id"`
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				ExternalID string `json:"external_id"`
				App        struct {
					ID int64 `json:"id"`
				} `json:"app"`
			} `json:"check_runs"`
		}
		endpoint := c.repoURL(owner, repo, fmt.Sprintf("commits/%s/check-runs?per_page=%d&page=%d%s", url.PathEscape(sha), perPage, page, filter))
		if err := c.call(ctx, "GET", endpoint, tok.Value, nil, &res); err != nil {
			return nil, err
		}
		total = res.Total
		for _, r := range res.Runs {
			out = append(out, CheckRun{ID: r.ID, Name: r.Name, Status: r.Status, Conclusion: r.Conclusion, AppID: r.App.ID, ExternalID: r.ExternalID})
		}
		if len(res.Runs) == 0 || len(out) >= res.Total {
			break
		}
	}
	if len(out) < total {
		return nil, ErrTooManyChecks
	}
	return out, nil
}

// CommitStatuses are the latest status of each context on a commit. They
// come from the individual statuses, never the combined state, which reads
// pending when there are none.
func (c *Client) CommitStatuses(ctx context.Context, installation int64, owner, repo, sha string) ([]CommitStatus, error) {
	tok, err := c.InstallationToken(ctx, installation, repo, WatchRead())
	if err != nil {
		return nil, err
	}
	out := []CommitStatus{}
	total := 0
	for page := 1; page <= 10; page++ {
		var res struct {
			Total    int `json:"total_count"`
			Statuses []struct {
				Context string `json:"context"`
				State   string `json:"state"`
			} `json:"statuses"`
		}
		endpoint := c.repoURL(owner, repo, fmt.Sprintf("commits/%s/status?per_page=%d&page=%d", url.PathEscape(sha), perPage, page))
		if err := c.call(ctx, "GET", endpoint, tok.Value, nil, &res); err != nil {
			return nil, err
		}
		total = res.Total
		for _, s := range res.Statuses {
			out = append(out, CommitStatus{Context: s.Context, State: s.State})
		}
		if len(res.Statuses) == 0 || len(out) >= res.Total {
			break
		}
	}
	if len(out) < total {
		return nil, ErrTooManyChecks
	}
	return out, nil
}

// CheckRunWrite is a check run as shpyrd writes it. Name and HeadSHA are
// read on create only; Conclusion and CompletedAt when Status is completed.
type CheckRunWrite struct {
	Name, HeadSHA          string
	Status, Conclusion     string
	DetailsURL, ExternalID string
	Title, Summary         string
	StartedAt, CompletedAt time.Time
}

func (w CheckRunWrite) body(create bool) map[string]any {
	b := map[string]any{"status": w.Status, "output": map[string]string{"title": w.Title, "summary": w.Summary}}
	if create {
		b["name"], b["head_sha"] = w.Name, w.HeadSHA
	}
	if w.DetailsURL != "" {
		b["details_url"] = w.DetailsURL
	}
	if w.ExternalID != "" {
		b["external_id"] = w.ExternalID
	}
	if !w.StartedAt.IsZero() {
		b["started_at"] = w.StartedAt.UTC().Format(time.RFC3339)
	}
	if w.Status == "completed" {
		b["conclusion"] = w.Conclusion
		if !w.CompletedAt.IsZero() {
			b["completed_at"] = w.CompletedAt.UTC().Format(time.RFC3339)
		}
	}
	return b
}

// CreateCheckRun makes a check run on w.HeadSHA and gives its id.
func (c *Client) CreateCheckRun(ctx context.Context, installation int64, owner, repo string, w CheckRunWrite) (int64, error) {
	tok, err := c.InstallationToken(ctx, installation, repo, ReportWrite())
	if err != nil {
		return 0, err
	}
	var res struct {
		ID int64 `json:"id"`
	}
	if err := c.call(ctx, "POST", c.repoURL(owner, repo, "check-runs"), tok.Value, w.body(true), &res); err != nil {
		return 0, err
	}
	if res.ID == 0 {
		return 0, errors.New("GitHub made the check run but gave no id")
	}
	return res.ID, nil
}

// UpdateCheckRun changes a check run; a deleted one is IsNotFound.
func (c *Client) UpdateCheckRun(ctx context.Context, installation int64, owner, repo string, id int64, w CheckRunWrite) error {
	tok, err := c.InstallationToken(ctx, installation, repo, ReportWrite())
	if err != nil {
		return err
	}
	return c.call(ctx, "PATCH", c.repoURL(owner, repo, fmt.Sprintf("check-runs/%d", id)), tok.Value, w.body(false), nil)
}

// DeploymentWrite is a Deployment as shpyrd makes it: shpyrd decided to
// deploy, so GitHub merges nothing and waits for no context. App and
// Release go in its payload, so the reporter finds the Deployment it made
// for a release before it makes another.
type DeploymentWrite struct {
	Ref, Environment, Description string
	App                           string
	Release                       int
}

// CreateDeployment makes a Deployment of d.Ref and gives its id.
func (c *Client) CreateDeployment(ctx context.Context, installation int64, owner, repo string, d DeploymentWrite) (int64, error) {
	tok, err := c.InstallationToken(ctx, installation, repo, ReportWrite())
	if err != nil {
		return 0, err
	}
	body := map[string]any{"ref": d.Ref, "environment": d.Environment, "description": d.Description,
		"auto_merge": false, "required_contexts": []string{}, "transient_environment": false}
	if d.App != "" {
		body["payload"] = map[string]any{"app": d.App, "release": d.Release}
	}
	var res struct {
		ID      int64  `json:"id"`
		Message string `json:"message"`
	}
	if err := c.call(ctx, "POST", c.repoURL(owner, repo, "deployments"), tok.Value, body, &res); err != nil {
		return 0, err
	}
	if res.ID == 0 {
		return 0, fmt.Errorf("GitHub made no Deployment: %s", firstNonEmpty(res.Message, "it gave no id"))
	}
	return res.ID, nil
}

// Deployment is a Deployment as the reporter finds it: the App that made
// it, and what its payload says it is for.
type Deployment struct {
	ID          int64
	Ref         string
	Environment string
	AppID       int64
	App         string
	Release     int
}

// ReportDeployments are an environment's newest Deployments (one page,
// newest first), read with the reporter's token.
func (c *Client) ReportDeployments(ctx context.Context, installation int64, owner, repo, environment string) ([]Deployment, error) {
	tok, err := c.InstallationToken(ctx, installation, repo, ReportWrite())
	if err != nil {
		return nil, err
	}
	var res []struct {
		ID          int64           `json:"id"`
		Ref         string          `json:"ref"`
		Environment string          `json:"environment"`
		Payload     json.RawMessage `json:"payload"`
		Via         *struct {
			ID int64 `json:"id"`
		} `json:"performed_via_github_app"`
	}
	endpoint := c.repoURL(owner, repo, fmt.Sprintf("deployments?environment=%s&per_page=%d", url.QueryEscape(environment), perPage))
	if err := c.call(ctx, "GET", endpoint, tok.Value, nil, &res); err != nil {
		return nil, err
	}
	out := make([]Deployment, 0, len(res))
	for _, d := range res {
		dep := Deployment{ID: d.ID, Ref: d.Ref, Environment: d.Environment}
		if d.Via != nil {
			dep.AppID = d.Via.ID
		}
		// A payload that is not shpyrd's (a string, another shape) says
		// nothing.
		var p struct {
			App     string `json:"app"`
			Release int    `json:"release"`
		}
		if json.Unmarshal(d.Payload, &p) == nil {
			dep.App, dep.Release = p.App, p.Release
		}
		out = append(out, dep)
	}
	return out, nil
}

// DeploymentState is a Deployment's newest state; "" when it has none. A
// deleted one is IsNotFound.
func (c *Client) DeploymentState(ctx context.Context, installation int64, owner, repo string, id int64) (string, error) {
	tok, err := c.InstallationToken(ctx, installation, repo, ReportWrite())
	if err != nil {
		return "", err
	}
	var res []struct {
		State string `json:"state"`
	}
	if err := c.call(ctx, "GET", c.repoURL(owner, repo, fmt.Sprintf("deployments/%d/statuses?per_page=1", id)), tok.Value, nil, &res); err != nil {
		return "", err
	}
	if len(res) == 0 {
		return "", nil
	}
	return res[0].State, nil
}

// DeploymentStatusWrite is a Deployment's new state: in_progress, success
// or failure. GitHub marks the previous Deployment inactive by itself.
type DeploymentStatusWrite struct {
	State, EnvironmentURL, LogURL, Description string
}

// CreateDeploymentStatus adds a status to a Deployment; a deleted one is
// IsNotFound.
func (c *Client) CreateDeploymentStatus(ctx context.Context, installation int64, owner, repo string, id int64, s DeploymentStatusWrite) error {
	tok, err := c.InstallationToken(ctx, installation, repo, ReportWrite())
	if err != nil {
		return err
	}
	body := map[string]any{"state": s.State}
	if s.EnvironmentURL != "" {
		body["environment_url"] = s.EnvironmentURL
	}
	if s.LogURL != "" {
		body["log_url"] = s.LogURL
	}
	if s.Description != "" {
		body["description"] = s.Description
	}
	return c.call(ctx, "POST", c.repoURL(owner, repo, fmt.Sprintf("deployments/%d/statuses", id)), tok.Value, body, nil)
}
