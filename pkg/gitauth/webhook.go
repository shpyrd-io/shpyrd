package gitauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Deploy on push (RFC-0018): checking a delivery from GitHub, GitLab or
// Gitea against a project's webhook secret, reading the push it carries,
// and the records the source watcher keeps on the App.

const (
	// HookSecretName is a project's webhook Secret, in its namespace.
	HookSecretName = "shpyrd-git-hook"
	// HookKeyToken is the random part of the webhook's URL; HookKeySecret
	// signs deliveries (GitHub, Gitea) or is sent as is (GitLab).
	HookKeyToken  = "token"
	HookKeySecret = "secret"
)

// ErrBadSignature: no header of the delivery matches the secret.
var ErrBadSignature = errors.New("the delivery's signature does not match the webhook's secret")

// VerifyDelivery checks a delivery against the project's secret: GitHub's
// X-Hub-Signature-256 or Gitea's X-Gitea-Signature (HMAC-SHA256 of the
// body), or GitLab's X-Gitlab-Token (the secret itself). One must match.
func VerifyDelivery(h http.Header, body []byte, secret string) error {
	if secret == "" {
		return ErrBadSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sum := mac.Sum(nil)
	if sig, ok := strings.CutPrefix(h.Get("X-Hub-Signature-256"), "sha256="); ok && hexEqual(sig, sum) {
		return nil
	}
	if sig := h.Get("X-Gitea-Signature"); sig != "" && hexEqual(sig, sum) {
		return nil
	}
	if tok := h.Get("X-Gitlab-Token"); tok != "" && subtle.ConstantTimeCompare([]byte(tok), []byte(secret)) == 1 {
		return nil
	}
	return ErrBadSignature
}

// VerifyGitHubDelivery checks GitHub's X-Hub-Signature-256 alone: the
// GitHub App's webhook (RFC-0054) is only ever called by GitHub.
func VerifyGitHubDelivery(h http.Header, body []byte, secret string) error {
	if secret == "" {
		return ErrBadSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if sig, ok := strings.CutPrefix(h.Get("X-Hub-Signature-256"), "sha256="); ok && hexEqual(sig, mac.Sum(nil)) {
		return nil
	}
	return ErrBadSignature
}

func hexEqual(given string, sum []byte) bool {
	got, err := hex.DecodeString(strings.TrimSpace(given))
	return err == nil && hmac.Equal(got, sum)
}

// Hook is what a delivery's headers say: its event (push, ping or other)
// and its id, which a host repeats when it delivers again.
type Hook struct {
	Event    string
	Delivery string
}

// HookOf reads a delivery's headers. Gitea sends GitHub's headers too;
// its own are read first.
func HookOf(h http.Header) Hook {
	switch {
	case h.Get("X-Gitlab-Event") != "":
		ev := "other"
		if h.Get("X-Gitlab-Event") == "Push Hook" {
			ev = "push"
		}
		return Hook{Event: ev, Delivery: h.Get("X-Gitlab-Event-UUID")}
	case h.Get("X-Gitea-Event") != "":
		return Hook{Event: eventOf(h.Get("X-Gitea-Event")), Delivery: h.Get("X-Gitea-Delivery")}
	}
	return Hook{Event: eventOf(h.Get("X-GitHub-Event")), Delivery: h.Get("X-GitHub-Delivery")}
}

func eventOf(e string) string {
	if e == "push" || e == "ping" {
		return e
	}
	return "other"
}

// Push is a push to a ref, as the webhook keeps it in the App's
// shpyrd.io/git-push annotation for the source watcher.
type Push struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
	// Before is the branch's commit before the push: a push applies
	// directly only when it follows the commit last seen.
	Before   string    `json:"before,omitempty"`
	Message  string    `json:"message,omitempty"`
	Author   string    `json:"author,omitempty"`
	Delivery string    `json:"delivery,omitempty"`
	At       time.Time `json:"at"`
	// Skip: the whole message, before it was clipped, asks not to deploy.
	Skip bool `json:"skip,omitempty"`
	// Deleted: the push removed the ref (its new commit is all zeros).
	Deleted bool `json:"-"`
}

// maxMessage caps the commit message kept: annotations are small.
const maxMessage = 1000

type payloadCommit struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	Author  struct {
		Name     string `json:"name"`
		Username string `json:"username"`
	} `json:"author"`
}

// ParsePush reads a push event's payload: GitHub's and Gitea's
// head_commit, GitLab's checkout_sha among its commits.
func ParsePush(body []byte) (Push, error) {
	var p struct {
		Ref          string          `json:"ref"`
		Before       string          `json:"before"`
		After        string          `json:"after"`
		HeadCommit   *payloadCommit  `json:"head_commit"`
		Commits      []payloadCommit `json:"commits"`
		UserUsername string          `json:"user_username"`
		Pusher       struct {
			Name     string `json:"name"`
			Login    string `json:"login"`
			Username string `json:"username"`
		} `json:"pusher"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return Push{}, fmt.Errorf("the push is not JSON shpyrd can read: %w", err)
	}
	if p.Ref == "" || p.After == "" {
		return Push{}, errors.New("the push names no ref or no commit")
	}
	out := Push{Ref: p.Ref, SHA: strings.ToLower(p.After), Before: strings.ToLower(p.Before)}
	if strings.Trim(out.SHA, "0") == "" {
		out.Deleted = true
		return out, nil
	}
	if !isHexSHA(out.SHA) {
		return Push{}, errors.New("the push's commit is not a SHA")
	}
	head := p.HeadCommit
	if head == nil {
		// GitLab: the commit pushed among the commits, when it is there.
		// Another commit's words and author are not taken for it.
		for i := range p.Commits {
			if strings.ToLower(p.Commits[i].ID) == out.SHA {
				head = &p.Commits[i]
			}
		}
	}
	if head != nil {
		out.Skip = SkipsDeploy(head.Message)
		out.Message = clip(head.Message, maxMessage)
		out.Author = firstNonEmpty(head.Author.Username, head.Author.Name)
	}
	out.Author = firstNonEmpty(out.Author, p.UserUsername, p.Pusher.Login, p.Pusher.Username, p.Pusher.Name)
	return out, nil
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ReadPush reads the git-push annotation; nil when there is none.
func ReadPush(annotation string) (*Push, error) {
	if annotation == "" {
		return nil, nil
	}
	var p Push
	if err := json.Unmarshal([]byte(annotation), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

var skipMarker = regexp.MustCompile(`(?i)\[(skip deploy|deploy skip)\]`)

// SkipsDeploy says a commit message asks not to deploy it.
func SkipsDeploy(message string) bool { return skipMarker.MatchString(message) }

// SourceRecord is what the source watcher last saw of a branch, kept in
// the App's shpyrd.io/git-source annotation; the App controller shows it
// as status.source.
type SourceRecord struct {
	// Ref is the revision the record is about.
	Ref string `json:"ref,omitempty"`
	// Latest is the newest commit seen on the branch, when, what it says
	// and who made it (when a push said), and how it was seen.
	Latest   string     `json:"latest,omitempty"`
	LatestAt *time.Time `json:"latestAt,omitempty"`
	Message  string     `json:"message,omitempty"`
	Author   string     `json:"author,omitempty"`
	Via      string     `json:"via,omitempty"`
	// Skipped is a commit whose message asked not to deploy it.
	Skipped string `json:"skipped,omitempty"`
	// PinnedAt is when the watcher last pinned a commit (RFC 3339, to the
	// second): a build that failed before it is not the pin's.
	PinnedAt *time.Time `json:"pinnedAt,omitempty"`
	// Held is a commit waiting for its CI on GitHub, since HeldSince: the
	// time the commit was first seen, or the nudge of a re-run (RFC-0054).
	Held      string     `json:"held,omitempty"`
	HeldSince *time.Time `json:"heldSince,omitempty"`
	// NotDeployed is the last commit decided not to deploy, and why:
	// ReasonNoWatchedChanges, ReasonCIFailedPrefix and the check's name,
	// ReasonCITimeout, ReasonCINotCovered or ReasonCIPermissions.
	NotDeployed       string     `json:"notDeployed,omitempty"`
	NotDeployedReason string     `json:"notDeployedReason,omitempty"`
	LastWebhookAt     *time.Time `json:"lastWebhookAt,omitempty"`
	CheckedAt         *time.Time `json:"checkedAt,omitempty"`
	// Error says why the branch could not be read, in words.
	Error string `json:"error,omitempty"`
	// Delivery is the id of the last delivery applied: a host delivering
	// it again is ignored.
	Delivery string `json:"delivery,omitempty"`
	// Failures counts the listings that failed in a row, for the backoff.
	Failures int `json:"failures,omitempty"`
}

// ReadSourceRecord reads the git-source annotation; an unreadable one is
// an empty record.
func ReadSourceRecord(annotation string) SourceRecord {
	var r SourceRecord
	if annotation != "" {
		if err := json.Unmarshal([]byte(annotation), &r); err != nil {
			return SourceRecord{}
		}
	}
	return r
}

// Why a commit was not deployed (RFC-0054), as SourceRecord's
// NotDeployedReason says it.
const (
	ReasonNoWatchedChanges = "No changes in the watch paths"
	ReasonCIFailedPrefix   = "CI failed: "
	ReasonCITimeout        = "CI did not finish within an hour"
	// ReasonCINotCovered: a commit held for its CI when the GitHub App
	// stopped covering the repository, not read again within the hour.
	ReasonCINotCovered = "CI could not be read: the GitHub App no longer covers the repository"
	// ReasonCIPermissions: the installation has not accepted the App's
	// permissions to read checks and statuses; a definite answer, given at
	// once.
	ReasonCIPermissions = "CI could not be read: accept the GitHub App's updated permissions on GitHub"
)

// WaitsOnCI says a change of sha's checks matters to the record: sha is
// held for its CI, or CI failed it and it is still the branch's newest
// commit, so a re-run may pass.
func (r SourceRecord) WaitsOnCI(sha string) bool {
	sha = strings.ToLower(sha)
	if sha == "" {
		return false
	}
	return r.Held == sha || (r.NotDeployed == sha && r.Latest == sha && strings.HasPrefix(r.NotDeployedReason, ReasonCIFailedPrefix))
}

// CINudge is a change of a commit's checks or statuses, left by the GitHub
// App's webhook in the App's shpyrd.io/git-ci annotation for the source
// watcher.
type CINudge struct {
	SHA      string    `json:"sha"`
	Delivery string    `json:"delivery,omitempty"`
	At       time.Time `json:"at"`
}

// ReadCINudge reads the git-ci annotation; nil when there is none or it
// cannot be read.
func ReadCINudge(annotation string) *CINudge {
	if annotation == "" {
		return nil
	}
	var n CINudge
	if err := json.Unmarshal([]byte(annotation), &n); err != nil || n.SHA == "" {
		return nil
	}
	n.SHA = strings.ToLower(n.SHA)
	return &n
}
