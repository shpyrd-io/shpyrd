package github_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
)

func TestEachScopeHasItsOwnToken(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	clone, err := c.InstallationToken(ctx, 42, "shop", github.ContentsRead())
	if err != nil {
		t.Fatal(err)
	}
	report, err := c.InstallationToken(ctx, 42, "shop", github.ReportWrite())
	if err != nil || report.Value == clone.Value || fake.MintedFor(42) != 2 {
		t.Fatalf("report token %+v, %v; minted %d", report, err, fake.MintedFor(42))
	}
	if body := fake.LastTokenRequest(); fmt.Sprint(body["permissions"]) != "map[checks:write deployments:write]" || fmt.Sprint(body["repositories"]) != "[shop]" {
		t.Errorf("asked for %v", body)
	}
	if again, _ := c.InstallationToken(ctx, 42, "shop", github.Permissions{"contents": "read"}); again.Value != clone.Value || fake.MintedFor(42) != 2 {
		t.Errorf("the clone scope keeps its own token: %+v", again)
	}
	if _, ok := c.CachedInstallationToken(42, "shop", github.WatchRead()); ok {
		t.Error("no watch token was asked for")
	}
}

func TestACompareListsARenamedFileUnderBothNames(t *testing.T) {
	c, fake := newClient(t)
	fake.SetCompare("Acme/shop", "aaa111", "bbb222", githubtest.Compare{Status: "ahead", Files: []githubtest.File{
		{Name: "services/api/main.go"}, {Name: "web/new.ts", Previous: "web/old.ts"},
	}})
	got, err := c.Compare(context.Background(), 42, "acme", "shop", "aaa111", "bbb222")
	if err != nil || got.Status != "ahead" || got.Capped || strings.Join(got.Files, ",") != "services/api/main.go,web/new.ts,web/old.ts" {
		t.Fatalf("%+v %v", got, err)
	}
	if q := fake.LastCompareQuery(); q != "per_page=1" {
		t.Errorf("a compare must stay small: %q", q)
	}
	if body := fake.LastTokenRequest(); fmt.Sprint(body["permissions"]) != "map[checks:read contents:read statuses:read]" || fmt.Sprint(body["repositories"]) != "[shop]" {
		t.Errorf("the watcher's scope: %v", body)
	}
}

func TestACompareOfThreeHundredFilesIsCapped(t *testing.T) {
	c, fake := newClient(t)
	var files []githubtest.File
	for i := range 300 {
		files = append(files, githubtest.File{Name: fmt.Sprintf("f%03d.txt", i)})
	}
	fake.SetCompare("Acme/shop", "aaa111", "bbb222", githubtest.Compare{Status: "ahead", Files: files})
	got, err := c.Compare(context.Background(), 42, "acme", "shop", "aaa111", "bbb222")
	if err != nil || !got.Capped {
		t.Errorf("%d files, capped %v, %v", len(got.Files), got.Capped, err)
	}
	if _, err := c.Compare(context.Background(), 42, "acme", "shop", "unknown", "bbb222"); !github.IsNotFound(err) {
		t.Errorf("a commit GitHub does not know: %v", err)
	}
}

func TestTheChecksAndStatusesOfACommitAreRead(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	fake.SetCheckRuns("bbb222", githubtest.CheckRun{Name: "test", Status: "completed", Conclusion: "success", AppID: 99},
		githubtest.CheckRun{Name: "lint", Status: "in_progress", AppID: 98})
	fake.SetStatuses("bbb222", githubtest.Status{Context: "ci/circle", State: "pending"})
	runs, err := c.CheckRuns(ctx, 42, "acme", "shop", "bbb222")
	if err != nil || len(runs) != 2 || runs[0].Name != "test" || runs[0].Conclusion != "success" || runs[0].AppID != 99 ||
		runs[1].Status != "in_progress" || runs[1].Conclusion != "" {
		t.Fatalf("%+v %v", runs, err)
	}
	statuses, err := c.CommitStatuses(ctx, 42, "acme", "shop", "bbb222")
	if err != nil || len(statuses) != 1 || statuses[0].Context != "ci/circle" || statuses[0].State != "pending" {
		t.Fatalf("%+v %v", statuses, err)
	}
	if runs, err := c.CheckRuns(ctx, 42, "acme", "shop", "ccc333"); err != nil || len(runs) != 0 {
		t.Errorf("a commit without runs: %+v %v", runs, err)
	}
}

func TestACheckRunIsWrittenWithTheReportTokenAndUpdated(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	if _, err := c.InstallationToken(ctx, 42, "shop", github.ContentsRead()); err != nil {
		t.Fatal(err)
	}
	id, err := c.CreateCheckRun(ctx, 42, "acme", "shop", github.CheckRunWrite{Name: "shpyrd / shop", HeadSHA: "bbb222", Status: "in_progress",
		Title: "Building", Summary: "Push bbb222 by alice: fix login", DetailsURL: "https://acme.test/projects/shop", ExternalID: "uid-1", StartedAt: t0})
	if err != nil || id == 0 {
		t.Fatalf("create: %d %v", id, err)
	}
	if err := c.UpdateCheckRun(ctx, 42, "acme", "shop", id, github.CheckRunWrite{Status: "completed", Conclusion: "success",
		Title: "Running at https://shop.test", Summary: "Push bbb222 by alice: fix login", CompletedAt: t0}); err != nil {
		t.Fatal(err)
	}
	runs := fake.CheckRunsOn("bbb222")
	if len(runs) != 1 || runs[0].Name != "shpyrd / shop" || runs[0].Status != "completed" || runs[0].Conclusion != "success" ||
		runs[0].Title != "Running at https://shop.test" || runs[0].DetailsURL != "https://acme.test/projects/shop" ||
		runs[0].ExternalID != "uid-1" || runs[0].AppID != githubtest.AppID {
		t.Fatalf("%+v", runs)
	}
	for _, w := range fake.Writes() {
		if fmt.Sprint(w.Permissions) != "map[checks:write deployments:write]" {
			t.Errorf("%s %s with %v", w.Method, w.Path, w.Permissions)
		}
	}
	fake.DeleteCheckRun(id)
	if err := c.UpdateCheckRun(ctx, 42, "acme", "shop", id, github.CheckRunWrite{Status: "in_progress", Title: "Building"}); !github.IsNotFound(err) {
		t.Errorf("a deleted run: %v", err)
	}
}

func TestADeploymentIsMadeForTheCommitAndShpyrdDecides(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	id, err := c.CreateDeployment(ctx, 42, "acme", "shop", github.DeploymentWrite{Ref: "bbb222", Environment: "shop", Description: "Push bbb222 by alice"})
	if err != nil || id == 0 {
		t.Fatalf("%d %v", id, err)
	}
	if err := c.CreateDeploymentStatus(ctx, 42, "acme", "shop", id, github.DeploymentStatusWrite{State: "success",
		EnvironmentURL: "https://shop.test", LogURL: "https://acme.test/projects/shop"}); err != nil {
		t.Fatal(err)
	}
	d := fake.Deployments()
	if len(d) != 1 || d[0].Ref != "bbb222" || d[0].Environment != "shop" || d[0].AutoMerge || d[0].Transient ||
		d[0].RequiredContexts == nil || len(d[0].RequiredContexts) != 0 ||
		len(d[0].Statuses) != 1 || d[0].Statuses[0].State != "success" || d[0].Statuses[0].EnvironmentURL != "https://shop.test" {
		t.Fatalf("%+v", d)
	}
	fake.DeleteDeployment(id)
	if err := c.CreateDeploymentStatus(ctx, 42, "acme", "shop", id, github.DeploymentStatusWrite{State: "failure"}); !github.IsNotFound(err) {
		t.Errorf("a deleted Deployment: %v", err)
	}
}

func TestAWriteWithTheCloneTokenIsRefusedAndRecorded(t *testing.T) {
	// What GitHub does to a token without checks: write; the fake keeps
	// the attempt, so the controller's tests can say none happens.
	c, fake := newClient(t)
	ctx := context.Background()
	tok, err := c.InstallationToken(ctx, 42, "shop", github.ContentsRead())
	if err != nil {
		t.Fatal(err)
	}
	status, _ := rawPost(fake, tok.Value, "/repos/acme/shop/check-runs", `{"name":"x","head_sha":"bbb222","status":"queued"}`)
	writes := fake.Writes()
	if status != 403 || len(writes) != 1 || fmt.Sprint(writes[0].Permissions) != "map[contents:read]" {
		t.Errorf("status %d, writes %+v", status, writes)
	}
}

func TestACompareOfTwoHundredNinetyNineFilesIsNotCapped(t *testing.T) {
	c, fake := newClient(t)
	var files []githubtest.File
	for i := range 299 {
		files = append(files, githubtest.File{Name: fmt.Sprintf("f%03d.txt", i)})
	}
	fake.SetCompare("Acme/shop", "aaa111", "bbb222", githubtest.Compare{Status: "ahead", Files: files})
	got, err := c.Compare(context.Background(), 42, "acme", "shop", "aaa111", "bbb222")
	if err != nil || got.Capped || len(got.Files) != 299 {
		t.Errorf("%d files, capped %v, %v", len(got.Files), got.Capped, err)
	}
}

func TestMoreThanAHundredCheckRunsAreAllRead(t *testing.T) {
	c, fake := newClient(t)
	var runs []githubtest.CheckRun
	for i := range 150 {
		runs = append(runs, githubtest.CheckRun{Name: fmt.Sprintf("job-%03d", i), Status: "completed", Conclusion: "success"})
	}
	fake.SetCheckRuns("bbb222", runs...)
	got, err := c.CheckRuns(context.Background(), 42, "acme", "shop", "bbb222")
	if err != nil || len(got) != 150 || got[149].Name != "job-149" {
		t.Errorf("%d runs, %v", len(got), err)
	}
}

func TestAListingGitHubHasMoreOfThanShpyrdReadsIsAnError(t *testing.T) {
	// A failing run beyond what was read must never read as "passed".
	c, fake := newClient(t)
	ctx := context.Background()
	fake.SetCheckRuns("bbb222", githubtest.CheckRun{Name: "test", Status: "completed", Conclusion: "success"})
	fake.SetCheckRunsTotal("bbb222", 1500)
	if _, err := c.CheckRuns(ctx, 42, "acme", "shop", "bbb222"); !errors.Is(err, github.ErrTooManyChecks) {
		t.Errorf("check runs: %v", err)
	}
	fake.SetStatuses("bbb222", githubtest.Status{Context: "ci", State: "success"})
	fake.SetStatusesTotal("bbb222", 1500)
	if _, err := c.CommitStatuses(ctx, 42, "acme", "shop", "bbb222"); !errors.Is(err, github.ErrTooManyChecks) {
		t.Errorf("statuses: %v", err)
	}
}

func TestTheScopesAreFreshMapsEachTime(t *testing.T) {
	github.ContentsRead()["checks"] = "write"
	if got := fmt.Sprint(github.ContentsRead()); got != "map[contents:read]" {
		t.Errorf("a caller widened the clone scope: %s", got)
	}
	github.WatchRead()["contents"] = "write"
	github.ReportWrite()["contents"] = "write"
	if fmt.Sprint(github.WatchRead()) != "map[checks:read contents:read statuses:read]" || fmt.Sprint(github.ReportWrite()) != "map[checks:write deployments:write]" {
		t.Error("the shared scopes changed")
	}
}

func TestAnUnapprovedPermissionIsNotAMissingRepository(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	fake.RefusePermission("checks")
	if _, err := c.CheckRuns(ctx, 42, "acme", "shop", "bbb222"); !errors.Is(err, github.ErrPermissionsNotGranted) || errors.Is(err, github.ErrRepoNotGranted) {
		t.Errorf("a scope with checks: %v", err)
	}
	if !strings.Contains(github.ErrPermissionsNotGranted.Error(), "accept the App's updated permissions on GitHub") {
		t.Errorf("the message: %v", github.ErrPermissionsNotGranted)
	}
	if _, err := c.InstallationToken(ctx, 42, "vault", github.ContentsRead()); !errors.Is(err, github.ErrRepoNotGranted) || errors.Is(err, github.ErrPermissionsNotGranted) {
		t.Errorf("the clone scope keeps its mapping: %v", err)
	}
}

// rawPost posts to the fake with a token, as a client without scopes would.
func rawPost(fake *githubtest.Server, token, path, body string) (int, error) {
	req, err := http.NewRequest("POST", fake.URL+path, strings.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := fake.Client().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func TestTheReporterListsItsOwnCheckRunsWithItsToken(t *testing.T) {
	c, fake := newClient(t)
	fake.SetCheckRuns("bbb222", githubtest.CheckRun{Name: "test", Status: "completed", Conclusion: "success"},
		githubtest.CheckRun{Name: "shpyrd / shop", Status: "in_progress", ExternalID: "uid-1"})
	got, err := c.ReportCheckRuns(context.Background(), 42, "acme", "shop", "bbb222", "shpyrd / shop")
	if err != nil || len(got) != 1 || got[0].Name != "shpyrd / shop" || got[0].ExternalID != "uid-1" {
		t.Fatalf("%+v, %v", got, err)
	}
	if perms := fake.LastTokenRequest()["permissions"]; fmt.Sprint(perms) != "map[checks:write deployments:write]" {
		t.Errorf("token %v", perms)
	}
}
