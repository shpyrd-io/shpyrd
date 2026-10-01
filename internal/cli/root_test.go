package cli

import (
	"bytes"
	"strings"
	"testing"
)

// `shpyrd` alone prints the help: a one-line description, the command list,
// and a closing pointer to the docs written for LLMs and agents. Subcommand
// help keeps cobra's shape and does not repeat the pointer.
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
	if !strings.HasPrefix(got, "shpyrd manages applications and agents from one place, from deploy to monitoring.\n\nUsage:\n") {
		t.Fatalf("root help starts with:\n%s", got[:min(len(got), 200)])
	}
	if !strings.Contains(got, "\nAvailable Commands:\n") {
		t.Fatal("root help lost the command list")
	}
	footer := "\nLLM, IA and Agents, read the Docs: " + agentDocsURL + "\n"
	if !strings.HasSuffix(got, footer) {
		t.Fatalf("root help ends with:\n%s", got[max(0, len(got)-200):])
	}

	sub := help("version", "--help")
	if strings.Contains(sub, agentDocsURL) {
		t.Fatalf("subcommand help repeats the docs pointer:\n%s", sub)
	}
}
