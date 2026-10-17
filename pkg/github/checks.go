package github

import (
	"context"
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
	tok, err := c.InstallationToken(ctx, installation, repo, WatchRead)
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
	if err := c.call(ctx, "GET", c.repoURL(owner, repo, "compare/"+url.PathEscape(base)+"..."+url.PathEscape(head)), tok.Value, nil, &res); err != nil {
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
	tok, err := c.InstallationToken(ctx, installation, repo, WatchRead)
	if err != nil {
		return nil, err
	}
	out := []CheckRun{}
	for page := 1; page <= 10; page++ {
		var res struct {
			Total int `json:"total_count"`
			Runs  []struct {
				ID         int64  `json:"id"`
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				App        struct {
					ID int64 `json:"id"`
				} `json:"app"`
			} `json:"check_runs"`
		}
		endpoint := c.repoURL(owner, repo, fmt.Sprintf("commits/%s/check-runs?per_page=%d&page=%d", url.PathEscape(sha), perPage, page))
		if err := c.call(ctx, "GET", endpoint, tok.Value, nil, &res); err != nil {
			return nil, err
		}
		for _, r := range res.Runs {
			out = append(out, CheckRun{ID: r.ID, Name: r.Name, Status: r.Status, Conclusion: r.Conclusion, AppID: r.App.ID})
		}
		if len(res.Runs) == 0 || len(out) >= res.Total {
			break
		}
	}
	return out, nil
}

// CommitStatuses are the latest status of each context on a commit. They
// come from the individual statuses, never the combined state, which reads
// pending when there are none.
func (c *Client) CommitStatuses(ctx context.Context, installation int64, owner, repo, sha string) ([]CommitStatus, error) {
	tok, err := c.InstallationToken(ctx, installation, repo, WatchRead)
	if err != nil {
		return nil, err
	}
	out := []CommitStatus{}
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
		for _, s := range res.Statuses {
			out = append(out, CommitStatus{Context: s.Context, State: s.State})
		}
		if len(res.Statuses) == 0 || len(out) >= res.Total {
			break
		}
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
	tok, err := c.InstallationToken(ctx, installation, repo, ReportWrite)
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
	tok, err := c.InstallationToken(ctx, installation, repo, ReportWrite)
	if err != nil {
		return err
	}
	return c.call(ctx, "PATCH", c.repoURL(owner, repo, fmt.Sprintf("check-runs/%d", id)), tok.Value, w.body(false), nil)
}

// DeploymentWrite is a Deployment as shpyrd makes it: shpyrd decided to
// deploy, so GitHub merges nothing and waits for no context.
type DeploymentWrite struct {
	Ref, Environment, Description string
}

// CreateDeployment makes a Deployment of d.Ref and gives its id.
func (c *Client) CreateDeployment(ctx context.Context, installation int64, owner, repo string, d DeploymentWrite) (int64, error) {
	tok, err := c.InstallationToken(ctx, installation, repo, ReportWrite)
	if err != nil {
		return 0, err
	}
	body := map[string]any{"ref": d.Ref, "environment": d.Environment, "description": d.Description,
		"auto_merge": false, "required_contexts": []string{}, "transient_environment": false}
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

// DeploymentStatusWrite is a Deployment's new state: in_progress, success
// or failure. GitHub marks the previous Deployment inactive by itself.
type DeploymentStatusWrite struct {
	State, EnvironmentURL, LogURL, Description string
}

// CreateDeploymentStatus adds a status to a Deployment; a deleted one is
// IsNotFound.
func (c *Client) CreateDeploymentStatus(ctx context.Context, installation int64, owner, repo string, id int64, s DeploymentStatusWrite) error {
	tok, err := c.InstallationToken(ctx, installation, repo, ReportWrite)
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
