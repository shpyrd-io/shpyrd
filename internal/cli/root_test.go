package cli

import (
	"bytes"
	"strings"
	"testing"
)

// `shpyrd` alone prints the help: a one-line description, then the usage
// line carrying the pointer to the docs written for LLMs and agents, then
// the command list. Every subcommand's help carries the same pointer.
func TestRootHelpPointsAgentsToDocs(t *testing.T) {
	help := func(args ...string) string {
		var out bytes.Buffer
		root := New()
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	got := help()
	want := "shpyrd manages applications and agents from one place, from deploy to monitoring.\n\n" +
		"Usage (LLM, IA and Agents, read the Docs: " + agentDocsURL + "):\n  shpyrd [command]\n\nAvailable Commands:\n"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("root help starts with:\n%s", got[:min(len(got), 300)])
	}

	sub := help("deploy", "--help")
	if !strings.Contains(sub, "\nUsage (LLM, IA and Agents, read the Docs: "+agentDocsURL+"):\n  shpyrd deploy") {
		t.Fatalf("subcommand help lacks the docs pointer:\n%s", sub)
	}
}
