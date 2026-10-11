package api

import (
	"strings"
	"testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// build.workspace names a package inside the source; with a Dockerfile or
// a buildpack list it means nothing, and the deploy says so (#145).
func TestValidateDeployRequestWorkspace(t *testing.T) {
	ok := []string{"apps/web", "web", "packages/a/b"}
	for _, p := range ok {
		req := &DeployRequest{Build: &shpyrdv1.Build{Workspace: p}}
		if err := validateDeployRequest(req); err != nil {
			t.Errorf("%q refused: %v", p, err)
		}
	}
	bad := []string{"/apps/web", "../web", "apps/../web", "apps//web", "./apps/web", "apps/web/", `apps\web`}
	for _, p := range bad {
		req := &DeployRequest{Build: &shpyrdv1.Build{Workspace: p}}
		err := validateDeployRequest(req)
		if err == nil || !strings.Contains(err.Error(), "build.workspace") {
			t.Errorf("%q accepted or unclear: %v", p, err)
		}
	}
	docker := &DeployRequest{Build: &shpyrdv1.Build{Workspace: "apps/web", Strategy: shpyrdv1.StrategyDockerfile}}
	if err := validateDeployRequest(docker); err == nil || !strings.Contains(err.Error(), "dockerfile") {
		t.Errorf("with a Dockerfile: %v", err)
	}
	listed := &DeployRequest{Build: &shpyrdv1.Build{Workspace: "apps/web", Buildpacks: []string{"node"}}}
	if err := validateDeployRequest(listed); err == nil || !strings.Contains(err.Error(), "build.buildpacks") {
		t.Errorf("with a buildpack list: %v", err)
	}
	full := &DeployRequest{Build: &shpyrdv1.Build{Workspace: "apps/web", Stack: "full"}}
	if err := validateDeployRequest(full); err != nil {
		t.Errorf("with the full stack: %v", err)
	}
}
