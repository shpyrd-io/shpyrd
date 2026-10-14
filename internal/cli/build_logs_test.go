package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

func TestBuildLogsBeforeFirstBuild(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("SHPYRD_TOKEN", "test-token")
	defer func(prev bool) { preferKubeconfig = prev }(preferKubeconfig)
	for _, tc := range []struct {
		name, phase, next, wantErr string
		follow                     bool
	}{
		{name: "preparing", phase: "Building"},
		{name: "pending", phase: "Pending"},
		{name: "failed", phase: "Failed", wantErr: "builder failed"},
		{name: "follow starts streaming", phase: "Building", next: "Building", follow: true},
		{name: "follow stops on failure", phase: "Building", next: "Failed", follow: true, wantErr: "builder failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads, streams := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/projects/shop":
					reads++
					phase, message, build := tc.phase, "Preparing this project's builder", ""
					if reads > 1 {
						phase = tc.next
						if phase != "Failed" {
							build = "shop-build-1"
						}
					}
					if phase == "Failed" {
						message = "builder failed"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"slug": "shop", "status": map[string]any{"phase": phase, "message": message, "latestBuild": build}})
				case "/api/projects/shop/builds/latest/logs":
					streams++
					if r.URL.Query().Get("follow") != "true" {
						t.Error("missing follow query")
					}
					_, _ = io.WriteString(w, "build output\n")
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			t.Setenv("SHPYRD_URL", srv.URL)
			root := New()
			var out, progress bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&progress)
			args := []string{"logs", "--project", "shop", "--build"}
			if tc.follow {
				args = append(args, "--follow")
			}
			root.SetArgs(args)
			err := root.Execute()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %s", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if tc.phase != "Failed" && !strings.Contains(progress.String(), "No build has started yet: Preparing") {
				t.Fatalf("missing preparation message: %s", progress.String())
			}
			wantReads, wantStreams := 1, 0
			if tc.follow {
				wantReads = 2
				if tc.next != "Failed" {
					wantStreams = 1
				}
			}
			if reads != wantReads || streams != wantStreams {
				t.Fatalf("reads=%d streams=%d, want %d/%d", reads, streams, wantReads, wantStreams)
			}
			if wantStreams == 1 && out.String() != "build output\n" {
				t.Fatalf("logs = %q", out.String())
			}
		})
	}
}

func TestWaitingForFirstBuildCanBeCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app := &shpyrdv1.App{Status: shpyrdv1.AppStatus{Phase: shpyrdv1.PhaseBuilding}}
	_, err := (&appClient{}).waitForFirstBuild(ctx, "shop", app, true, io.Discard)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
