package controller

import "regexp"

// The rule for every message a customer reads (#52): what happened and how
// to resolve it, in words. Never a command, a flag, a tool or a Kubernetes
// name: the reader is a person or their agent in the dashboard, the CLI or
// an assistant, each of which shows a log or runs the next step its own
// way, and none of which has the cluster. Output of the customer's own
// build or app is shown as the log, theirs, never inside a message.
//
// PlatformWordingFault names the first thing a message must not say, ""
// when it says none. Tests hold the platform's messages to it.
func PlatformWordingFault(msg string) string {
	for _, re := range wordingFaults {
		if m := re.FindString(msg); m != "" {
			return m
		}
	}
	return ""
}

var wordingFaults = []*regexp.Regexp{
	regexp.MustCompile(`\bkubectl\b`),
	regexp.MustCompile(`(?i)\bpods?\b|-build-pod\b`),
	regexp.MustCompile(`(?i)\bnamespaces?\b|\bp-[a-z0-9]{25}\b`),
	regexp.MustCompile(`\bJobs?\b`),
	regexp.MustCompile(`(?i)\bReplicaSet\b|\bDeployment\b|\bResourceQuota\b|\bcontainer \w+ terminated\b`),
	// The CLI's name followed by one of its commands.
	regexp.MustCompile(`\bshpyrd(?:-ctl)? (?:deploy|logs|secrets|config|scale|resize|attach|detach|pg|redis|sizes|run|shell|login|projects|domains|access|members|sleep|rollback|releases|builds|volumes|extensions|globals|cluster|auth|tokens|restart|open|status|plans|workspaces)\b`),
}
