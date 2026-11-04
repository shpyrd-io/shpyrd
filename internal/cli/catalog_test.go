package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
			_ = json.NewEncoder(w).Encode(api.CatalogInstallResponse{Project: api.AppSummary{Slug: "agent", URL: "https://agent.example.test"}, Notes: []string{"Its disk is 10Gi, the provider minimum."}})
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
