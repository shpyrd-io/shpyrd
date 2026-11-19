package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/api"
	"github.com/shpyrd-io/shpyrd/pkg/catalog"
)

const fluxyrEntryYAML = `catalog:
  name: Fluxyr
  description: A Python framework for agents.
  category: agents
  source:
    git: https://github.com/shpyrd-io/fluxyr
  inputs:
    - name: OPENROUTER_API_KEY
      label: OpenRouter API key
      secret: true
      required: true
    - name: FLUXYR_MODEL
      label: Model
      default: minimax/minimax-m3
  volumes:
    - name: data
      size: 5Gi
processes:
  web:
    port: 5050
    volumes: [{name: data, path: /var/lib/fluxyr}]
build:
  strategy: dockerfile
`

func writeCatalogEntry(t *testing.T, dir, name, yaml string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	files[catalog.EntryFile] = yaml
	for n, c := range files {
		if err := os.WriteFile(filepath.Join(dir, name, n), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := New()
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err := root.Execute()
	return out.String() + errOut.String(), err
}

func TestCatalogValidateAndPublishNeedNoServer(t *testing.T) {
	dir := t.TempDir()
	writeCatalogEntry(t, dir, "fluxyr", fluxyrEntryYAML, map[string]string{"Dockerfile": "FROM python"})
	writeCatalogEntry(t, dir, "broken", strings.Replace(fluxyrEntryYAML, "category: agents", "category: ''", 1), map[string]string{})

	text, err := runCLI(t, "catalog", "validate", dir)
	if err == nil || !strings.Contains(text, "fluxyr: ok") || !strings.Contains(text, "broken:\n  - catalog.category is missing") {
		t.Fatalf("validate: %v\n%s", err, text)
	}
	if err := os.RemoveAll(filepath.Join(dir, "broken")); err != nil {
		t.Fatal(err)
	}
	if text, err := runCLI(t, "catalog", "validate", dir); err != nil || !strings.Contains(text, "fluxyr: ok") {
		t.Fatalf("validate: %v\n%s", err, text)
	}

	defer func(prev catalog.RefLister) { catalogRefs = prev }(catalogRefs)
	catalogRefs = func(context.Context, string) (map[string]string, error) {
		return map[string]string{"refs/heads/main": strings.Repeat("a", 40)}, nil
	}
	out := t.TempDir()
	text, err = runCLI(t, "catalog", "publish", dir, "--out", out)
	if err != nil || !strings.Contains(text, "Published 1 entries") || !strings.Contains(text, "fluxyr (overlay: Dockerfile)") {
		t.Fatalf("publish: %v\n%s", err, text)
	}
	raw, err := os.ReadFile(filepath.Join(out, catalog.IndexFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ParseIndex(raw); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogListShowAndInstallThroughTheAPI(t *testing.T) {
	entry := api.CatalogEntryView{
		Name: "fluxyr", DisplayName: "Fluxyr", Description: "A Python framework for agents.", Category: "agents",
		Source: catalog.Source{Git: "https://github.com/shpyrd-io/fluxyr", Revision: "main"}, Build: "dockerfile", Overlay: []string{"Dockerfile"},
		Processes: []string{"web"}, Volumes: []catalog.Volume{{Name: "data", Size: "5Gi"}}, Resources: []catalog.Resource{},
		Inputs: []catalog.Input{{Name: "OPENROUTER_API_KEY", Label: "OpenRouter API key", Secret: true, Required: true, Help: "From openrouter.ai/keys."}, {Name: "FLUXYR_MODEL", Label: "Model", Default: "minimax/minimax-m3"}, {Name: "VAULT_ENCRYPTION_KEY", Generate: 32}},
	}
	var installed api.CatalogInstallRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/catalog":
			_ = json.NewEncoder(w).Encode(api.CatalogView{URL: "https://catalog.test/index.json", Categories: []string{"agents"}, Entries: []api.CatalogEntryView{entry}})
		case r.URL.Path == "/api/catalog/entries/fluxyr" && r.Method == http.MethodGet:
			e := entry
			e.Readme = "# Fluxyr\n\nAn agent."
			_ = json.NewEncoder(w).Encode(e)
		case r.URL.Path == "/api/catalog/entries/fluxyr/install" && r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&installed)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(api.CatalogInstallResponse{Project: api.AppSummary{Slug: "agent", URL: "https://agent.example.test"}, Notes: []string{"Its disk is 10Gi, the minimum size."}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("SHPYRD_URL", srv.URL)
	t.Setenv("SHPYRD_TOKEN", "test-token")
	defer func(prev bool) { preferKubeconfig = prev }(preferKubeconfig)

	text, err := runCLI(t, "catalog", "list")
	if err != nil || !strings.HasPrefix(text, "ENTRY") || !strings.Contains(text, "fluxyr") || !strings.Contains(text, "1 input(s), disk") {
		t.Fatalf("list: %v\n%s", err, text)
	}
	text, err = runCLI(t, "catalog", "show", "fluxyr")
	if err != nil || !strings.Contains(text, "Fluxyr (fluxyr)") || !strings.Contains(text, "OPENROUTER_API_KEY: OpenRouter API key (required), kept secret") || !strings.Contains(text, "FLUXYR_MODEL: Model (default minimax/minimax-m3)") || strings.Contains(text, "VAULT_ENCRYPTION_KEY") || !strings.Contains(text, "# Fluxyr") {
		t.Fatalf("show: %v\n%s", err, text)
	}
	text, err = runCLI(t, "catalog", "install", "fluxyr", "--slug", "agent", "--input", "OPENROUTER_API_KEY=sk-or-1", "--input", "FLUXYR_MODEL=x/y", "--revision", "v1")
	if err != nil || !strings.Contains(text, "Installed fluxyr as project agent, answering at https://agent.example.test once built.") || !strings.Contains(text, "Note: Its disk is 10Gi") {
		t.Fatalf("install: %v\n%s", err, text)
	}
	if installed.Slug != "agent" || installed.Revision != "v1" || installed.Inputs["OPENROUTER_API_KEY"] != "sk-or-1" || installed.Inputs["FLUXYR_MODEL"] != "x/y" {
		t.Fatalf("the server received %+v", installed)
	}
	if _, err := runCLI(t, "catalog", "install", "fluxyr", "--input", "nope"); err == nil || !strings.Contains(err.Error(), "NAME=value") {
		t.Fatalf("a bad --input: %v", err)
	}
	stdout, err := runCLI(t, "catalog", "list", "--jq", ".entries[].name")
	if err != nil || stdout != "fluxyr\n" {
		t.Fatalf("--jq: %v %q", err, stdout)
	}
}

func TestCatalogPublishSaysTheRevisionAndTheCommit(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	dir := t.TempDir()
	writeCatalogEntry(t, dir, "tagged", strings.Replace(fluxyrEntryYAML, "https://github.com/shpyrd-io/fluxyr", "https://github.com/shpyrd-io/fluxyr\n    revision: v1", 1), map[string]string{})
	writeCatalogEntry(t, dir, "branched", fluxyrEntryYAML, map[string]string{})
	list := func(_ context.Context, _ string) (map[string]string, error) {
		return map[string]string{"refs/heads/main": commit, "refs/tags/v1": strings.Repeat("b", 40)}, nil
	}
	root := New()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	cmd, _, err := root.Find([]string{"catalog", "publish"})
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	g := &globalFlags{}
	if err := publishCatalog(cmd, g, dir, t.TempDir(), time.Now(), list); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "tagged: v1 at bbbbbbbbbbbb\n") || !strings.Contains(text, "branched: main at 0123456789ab (BRANCH)") || !strings.Contains(text, "1 entry names a branch") {
		t.Fatalf("publish said:\n%s", text)
	}
	if strings.Contains(text, "tagged: v1 at bbbbbbbbbbbb (BRANCH)") {
		t.Fatalf("a tag is not marked:\n%s", text)
	}
}

// catalogVersionServer is a fake API that publishes fluxyr at a commit and
// serves the update endpoints; applied records what the apply received.
func catalogVersionServer(t *testing.T, state string, applied *api.CatalogUpdateRequest, refuse bool) {
	t.Helper()
	const commit = "0123456789abcdef0123456789abcdef01234567"
	offered := &api.CatalogVersionView{Revision: "v2", Commit: commit, ShortCommit: commit[:12], Digest: "sha256:new"}
	running := &api.CatalogVersionView{Revision: "v1", Commit: strings.Repeat("a", 40), ShortCommit: strings.Repeat("a", 12), Digest: "sha256:old"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/catalog":
			e := api.CatalogEntryView{Name: "fluxyr", Digest: "sha256:new", DisplayName: "Fluxyr", Category: "agents", Source: catalog.Source{Git: "https://github.com/shpyrd-io/fluxyr", Revision: "v2"}}
			raw, _ := json.Marshal(map[string]any{"url": "https://catalog.test/index.json", "categories": []string{"agents"}, "entries": []any{struct {
				api.CatalogEntryView
				Commit string `json:"commit"`
			}{e, commit}}})
			_, _ = w.Write(raw)
		case r.URL.Path == "/api/catalog/entries/fluxyr":
			e := api.CatalogEntryView{Name: "fluxyr", Digest: "sha256:0123abcd", DisplayName: "Fluxyr", Category: "agents", Source: catalog.Source{Git: "https://github.com/shpyrd-io/fluxyr", Revision: "v2"}, Build: "dockerfile"}
			raw, _ := json.Marshal(struct {
				api.CatalogEntryView
				Commit string `json:"commit"`
			}{e, commit})
			_, _ = w.Write(raw)
		case r.URL.Path == "/api/projects/agent/catalog/update" && r.Method == http.MethodGet:
			p := api.CatalogUpdatePreview{Entry: "fluxyr", State: state, From: running, Inputs: []api.CatalogUpdateInput{}, Volumes: []string{}, Resources: []string{}, ProcessesAdded: []string{"worker"}, ProcessesChanged: []string{}, EnvAdded: []string{"FLUXYR_WORKERS"}, EnvKept: []string{"FLUXYR_HOST"}}
			switch state {
			case api.CatalogUpdateAvailable:
				p.To, p.Repository = offered, "https://github.com/shpyrd-io/fluxyr"
				p.Inputs = []api.CatalogUpdateInput{{Name: "NEW_NEEDED", Required: true, Action: "ask"}, {Name: "VAULT_KEY", Secret: true, Action: "generate"}}
				p.Volumes = []string{"extra"}
			case api.CatalogUpToDate:
				p.From, p.Reason = offered, "the project already runs the version the catalog offers"
			}
			_ = json.NewEncoder(w).Encode(p)
		case r.URL.Path == "/api/projects/agent/catalog/update" && r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(applied)
			if refuse {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"error":"the update needs Needed (NEW_NEEDED)"}`))
				return
			}
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(api.CatalogUpdateResult{Changed: true, From: running, To: offered})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("SHPYRD_URL", srv.URL)
	t.Setenv("SHPYRD_TOKEN", "test-token")
	prev := preferKubeconfig
	t.Cleanup(func() { preferKubeconfig = prev })
}

func TestCatalogListAndShowSayTheEntrysVersion(t *testing.T) {
	catalogVersionServer(t, api.CatalogUpToDate, &api.CatalogUpdateRequest{}, false)
	text, err := runCLI(t, "catalog", "list")
	if err != nil || !strings.Contains(text, "VERSION") || !strings.Contains(text, "v2 @ 0123456789ab") {
		t.Fatalf("list: %v\n%s", err, text)
	}
	text, err = runCLI(t, "catalog", "show", "fluxyr")
	if err != nil || !strings.Contains(text, "Version") || !strings.Contains(text, "v2 (commit 0123456789abcdef0123456789abcdef01234567, digest 0123abcd)") {
		t.Fatalf("show: %v\n%s", err, text)
	}
	out, err := runCLI(t, "catalog", "list", "--jq", ".entries[0].commit")
	if err != nil || strings.TrimSpace(out) != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("--jq: %v %q", err, out)
	}
}

func TestCatalogHelpSaysTheProjectStaysOnTheCatalogsVersion(t *testing.T) {
	for _, args := range [][]string{{"catalog", "--help"}, {"catalog", "install", "--help"}} {
		text, err := runCLI(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(text, "follows the upstream branch") || strings.Contains(text, "follow the entry's branch") || strings.Contains(text, "follows the entry's branch") || !strings.Contains(text, "version") {
			t.Fatalf("%v:\n%s", args, text)
		}
	}
}

func TestCatalogStatusShowsTheVersionAndThePreview(t *testing.T) {
	catalogVersionServer(t, api.CatalogUpdateAvailable, &api.CatalogUpdateRequest{}, false)
	text, err := runCLI(t, "catalog", "status", "agent")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Runs:     v1 @ aaaaaaaaaaaa", "Offered:  v2 @ 0123456789ab", "makes volumes: extra", "adds processes: worker", "adds variables: FLUXYR_WORKERS", "keeps the variables at your value: FLUXYR_HOST", "asks for NEW_NEEDED (required): --input NEW_NEEDED=value", "generates VAULT_KEY"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "sets variables") || strings.Contains(text, "adds variables: FLUXYR_HOST") {
		t.Fatalf("a kept variable is not one the update sets: %s", text)
	}
	out, err := runCLI(t, "catalog", "status", "agent", "--jq", ".to.commit")
	if err != nil || strings.TrimSpace(out) != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("--jq: %v %q", err, out)
	}
}

func TestCatalogUpdateAppliesWithTheInputsAndTheDigestPreviewed(t *testing.T) {
	var applied api.CatalogUpdateRequest
	catalogVersionServer(t, api.CatalogUpdateAvailable, &applied, false)
	// Without --yes and with no terminal, nothing is applied.
	if _, err := runCLI(t, "catalog", "update", "agent", "--input", "NEW_NEEDED=yes"); err == nil || !strings.Contains(err.Error(), "aborted") || applied.Digest != "" {
		t.Fatalf("no confirmation: %v %+v", err, applied)
	}
	text, err := runCLI(t, "catalog", "update", "agent", "--yes", "--input", "NEW_NEEDED=yes")
	if err != nil || !strings.Contains(text, "Updating agent to v2 @ 0123456789ab") {
		t.Fatalf("update: %v\n%s", err, text)
	}
	if applied.Digest != "sha256:new" || applied.Inputs["NEW_NEEDED"] != "yes" || len(applied.Inputs) != 1 {
		t.Fatalf("the server received %+v", applied)
	}
	out, err := runCLI(t, "catalog", "update", "agent", "-y", "--json")
	if err != nil || !strings.Contains(out, `"changed": true`) && !strings.Contains(out, `"changed":true`) {
		t.Fatalf("--json: %v %s", err, out)
	}
	if _, err := runCLI(t, "catalog", "update", "agent", "--yes", "--input", "nope"); err == nil || !strings.Contains(err.Error(), "NAME=value") {
		t.Fatalf("a bad --input: %v", err)
	}
}

func TestCatalogUpdateSaysWhyWhenThereIsNothingToApplyAndRelaysRefusals(t *testing.T) {
	var applied api.CatalogUpdateRequest
	catalogVersionServer(t, api.CatalogUpToDate, &applied, false)
	text, err := runCLI(t, "catalog", "update", "agent", "--yes")
	if err != nil || !strings.Contains(text, "up to date") || applied.Digest != "" {
		t.Fatalf("up to date: %v %s %+v", err, text, applied)
	}
	catalogVersionServer(t, api.CatalogUpdateAvailable, &applied, true)
	if _, err := runCLI(t, "catalog", "update", "agent", "--yes"); err == nil || !strings.Contains(err.Error(), "the update needs Needed (NEW_NEEDED)") {
		t.Fatalf("the server's words: %v", err)
	}
}
