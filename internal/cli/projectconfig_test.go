package cli

import (
	"testing"

	"sigs.k8s.io/yaml"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

func TestProjectGlobalsKey(t *testing.T) {
	cases := map[string]*shpyrdv1.Globals{
		"project: x\n":                                   nil,
		"project: x\nglobals: true\n":                    nil,
		"project: x\nglobals: false\n":                   {Disabled: true},
		"project: x\nglobals:\n  exclude: [A, B]\n":      {Exclude: []string{"A", "B"}},
		"project: x\nglobals: {exclude: [OPENAI_KEY]}\n": {Exclude: []string{"OPENAI_KEY"}},
	}
	for in, want := range cases {
		var pc projectConfig
		if err := yaml.Unmarshal([]byte(in), &pc); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		app := &shpyrdv1.App{}
		if err := pc.applyTo(app); err != nil {
			t.Fatal(err)
		}
		got := app.Spec.Globals
		switch {
		case want == nil && got != nil:
			t.Errorf("%q: got %+v, want nil", in, got)
		case want != nil && (got == nil || got.Disabled != want.Disabled || len(got.Exclude) != len(want.Exclude)):
			t.Errorf("%q: got %+v, want %+v", in, got, want)
		}
	}
	var pc projectConfig
	if err := yaml.Unmarshal([]byte("project: x\nglobals: [nope]\n"), &pc); err == nil {
		t.Error("a list is not a valid globals value")
	}
}

// The env key declares plain variables for every process, sorted so the
// spec is stable; an empty map clears them and the deploy request carries
// the empty list; platform names are refused.
func TestProjectEnvKey(t *testing.T) {
	var pc projectConfig
	if err := yaml.Unmarshal([]byte("project: x\nenv:\n  RAILS_ENV: production\n  APP_NAME: demo\n"), &pc); err != nil {
		t.Fatal(err)
	}
	app := &shpyrdv1.App{}
	if err := pc.applyTo(app); err != nil {
		t.Fatal(err)
	}
	if len(app.Spec.Env) != 2 || app.Spec.Env[0].Name != "APP_NAME" || app.Spec.Env[1].Name != "RAILS_ENV" || app.Spec.Env[1].Value != "production" {
		t.Errorf("env = %+v", app.Spec.Env)
	}
	req, err := pc.deployRequest(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Env) != 2 {
		t.Errorf("deploy request env = %+v", req.Env)
	}

	var none projectConfig
	if err := yaml.Unmarshal([]byte("project: x\n"), &none); err != nil {
		t.Fatal(err)
	}
	if req, _ := none.deployRequest(nil); req.Env != nil {
		t.Errorf("no env key must leave the variables alone, got %+v", req.Env)
	}
	var empty projectConfig
	if err := yaml.Unmarshal([]byte("project: x\nenv: {}\n"), &empty); err != nil {
		t.Fatal(err)
	}
	if req, _ := empty.deployRequest(nil); req.Env == nil || len(req.Env) != 0 {
		t.Errorf("env: {} must send an empty list to clear the variables, got %#v", req.Env)
	}

	for _, bad := range []string{"env:\n  PORT: \"80\"\n", "env:\n  SHPYRD_PROJECT: y\n", "env:\n  1ABC: y\n", "env:\n  A-B: y\n"} {
		var pc projectConfig
		if err := yaml.Unmarshal([]byte("project: x\n"+bad), &pc); err != nil {
			t.Fatal(err)
		}
		if err := pc.applyTo(&shpyrdv1.App{}); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}
