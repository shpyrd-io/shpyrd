package gitauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

const hookSecret = "9a41d0c7e2b85f369a41d0c7e2b85f36"

func hmacHex(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func headers(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

func TestVerifyDeliveryGitHubGiteaGitLab(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	for name, h := range map[string]http.Header{
		"github": headers("X-Hub-Signature-256", "sha256="+hmacHex(hookSecret, body)),
		"gitea":  headers("X-Gitea-Signature", hmacHex(hookSecret, body)),
		"gitlab": headers("X-Gitlab-Token", hookSecret),
	} {
		if err := VerifyDelivery(h, body, hookSecret); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, h := range map[string]http.Header{
		"another secret": headers("X-Hub-Signature-256", "sha256="+hmacHex("other", body)),
		"another body":   headers("X-Hub-Signature-256", "sha256="+hmacHex(hookSecret, []byte("{}"))),
		"sha1 only":      headers("X-Hub-Signature", "sha1=abc"),
		"gitlab wrong":   headers("X-Gitlab-Token", "guess"),
		"nothing":        headers(),
		"not hex":        headers("X-Gitea-Signature", "zz"),
	} {
		if err := VerifyDelivery(h, body, hookSecret); !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := VerifyDelivery(headers("X-Gitlab-Token", ""), body, ""); !errors.Is(err, ErrBadSignature) {
		t.Error("an empty secret verifies nothing")
	}
}

func TestHookOf(t *testing.T) {
	for _, tc := range []struct {
		h    http.Header
		want Hook
	}{
		{headers("X-GitHub-Event", "push", "X-GitHub-Delivery", "gh-1"), Hook{"push", "gh-1"}},
		{headers("X-GitHub-Event", "ping", "X-GitHub-Delivery", "gh-2"), Hook{"ping", "gh-2"}},
		{headers("X-GitHub-Event", "pull_request"), Hook{"other", ""}},
		// Gitea also sends GitHub's headers; its own win.
		{headers("X-Gitea-Event", "push", "X-Gitea-Delivery", "te-1", "X-GitHub-Event", "push", "X-GitHub-Delivery", "te-1"), Hook{"push", "te-1"}},
		{headers("X-Gitlab-Event", "Push Hook", "X-Gitlab-Event-UUID", "gl-1"), Hook{"push", "gl-1"}},
		{headers("X-Gitlab-Event", "Tag Push Hook", "X-Gitlab-Event-UUID", "gl-2"), Hook{"other", "gl-2"}},
	} {
		if got := HookOf(tc.h); got != tc.want {
			t.Errorf("%v: got %+v, want %+v", tc.h, got, tc.want)
		}
	}
}

const (
	beforeSHA = "0123456789abcdef0123456789abcdef01234567"
	afterSHA  = "89abcdef0123456789abcdef0123456789abcdef"
)

func TestParsePushGitHubAndGitea(t *testing.T) {
	body := `{"ref":"refs/heads/main","before":"` + beforeSHA + `","after":"` + afterSHA + `",
	  "head_commit":{"id":"` + afterSHA + `","message":"fix login\n\nThe redirect lost the session.","author":{"name":"Alice","username":"alice"}},
	  "pusher":{"name":"alice"}}`
	p, err := ParsePush([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if p.Ref != "refs/heads/main" || p.SHA != afterSHA || p.Author != "alice" || !strings.HasPrefix(p.Message, "fix login") || p.Deleted {
		t.Errorf("push = %+v", p)
	}
}

func TestParsePushGitLabTakesTheCheckedOutCommit(t *testing.T) {
	body := `{"object_kind":"push","ref":"refs/heads/main","before":"` + beforeSHA + `","after":"` + afterSHA + `",
	  "checkout_sha":"` + afterSHA + `","user_username":"bruno",
	  "commits":[{"id":"` + afterSHA + `","message":"bump deps","author":{"name":"Bruno"}},
	             {"id":"` + beforeSHA + `","message":"older","author":{"name":"Carla"}}]}`
	p, err := ParsePush([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if p.SHA != afterSHA || p.Message != "bump deps" || p.Author != "Bruno" {
		t.Errorf("push = %+v", p)
	}
}

func TestParsePushOfADeletedBranch(t *testing.T) {
	p, err := ParsePush([]byte(`{"ref":"refs/heads/main","before":"` + beforeSHA + `","after":"0000000000000000000000000000000000000000","head_commit":null}`))
	if err != nil || !p.Deleted {
		t.Errorf("push %+v err %v", p, err)
	}
}

func TestParsePushRefusesWhatItCannotRead(t *testing.T) {
	for _, body := range []string{`{"ref":`, `{}`, `{"ref":"refs/heads/main","after":"main"}`, `[]`} {
		if _, err := ParsePush([]byte(body)); err == nil {
			t.Errorf("%s: want an error", body)
		}
	}
}

func TestParsePushKeepsALongMessageShort(t *testing.T) {
	long := strings.Repeat("é", 5000)
	body, _ := json.Marshal(map[string]any{"ref": "refs/heads/main", "after": afterSHA, "head_commit": map[string]any{"message": long}})
	p, err := ParsePush(body)
	if err != nil || len([]rune(p.Message)) != maxMessage {
		t.Errorf("message of %d runes, err %v", len([]rune(p.Message)), err)
	}
}

func TestSkipsDeploy(t *testing.T) {
	for msg, want := range map[string]bool{
		"docs: typo [skip deploy]": true,
		"[Deploy Skip] tidy":       true,
		"Fix\n\n[SKIP DEPLOY]":     true,
		"skip deploy, said nobody": false,
		"[skip ci] tests only":     false,
		"":                         false,
	} {
		if got := SkipsDeploy(msg); got != want {
			t.Errorf("%q: %v, want %v", msg, got, want)
		}
	}
}

func TestPushAndRecordAnnotations(t *testing.T) {
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	raw, _ := json.Marshal(Push{Ref: "refs/heads/main", SHA: afterSHA, Author: "alice", Delivery: "d-1", At: at, Deleted: true})
	p, err := ReadPush(string(raw))
	if err != nil || p == nil || p.SHA != afterSHA || p.Delivery != "d-1" || !p.At.Equal(at) || p.Deleted {
		t.Errorf("push %+v err %v", p, err)
	}
	if p, err := ReadPush(""); p != nil || err != nil {
		t.Errorf("no annotation: %+v %v", p, err)
	}
	if _, err := ReadPush("{"); err == nil {
		t.Error("a broken annotation is an error")
	}
	rec := ReadSourceRecord(`{"ref":"main","latest":"` + afterSHA + `","failures":2,"checkedAt":"2026-10-16T12:00:00Z"}`)
	if rec.Ref != "main" || rec.Latest != afterSHA || rec.Failures != 2 || rec.CheckedAt == nil || !rec.CheckedAt.Equal(at) {
		t.Errorf("record %+v", rec)
	}
	if rec := ReadSourceRecord("not json"); rec.Latest != "" || rec.CheckedAt != nil {
		t.Errorf("an unreadable record is an empty one: %+v", rec)
	}
}

func TestParsePushReadsBefore(t *testing.T) {
	p, err := ParsePush([]byte(`{"ref":"refs/heads/main","before":"` + strings.ToUpper(beforeSHA) + `","after":"` + afterSHA + `"}`))
	if err != nil || p.Before != beforeSHA {
		t.Errorf("push %+v err %v", p, err)
	}
}

// The marker is read on the whole message, before it is clipped.
func TestParsePushSkipsALongMessageEndingInTheMarker(t *testing.T) {
	msg := strings.Repeat("x", 5000-len("[SKIP DEPLOY]")) + "[SKIP DEPLOY]"
	body, _ := json.Marshal(map[string]any{"ref": "refs/heads/main", "after": afterSHA, "head_commit": map[string]any{"id": afterSHA, "message": msg}})
	p, err := ParsePush(body)
	if err != nil || !p.Skip || SkipsDeploy(p.Message) {
		t.Errorf("skip %v, clipped message skips %v, err %v", p.Skip, SkipsDeploy(p.Message), err)
	}
	short, _ := ParsePush([]byte(`{"ref":"refs/heads/main","after":"` + afterSHA + `","head_commit":{"id":"` + afterSHA + `","message":"docs [skip deploy]"}}`))
	plain, _ := ParsePush([]byte(`{"ref":"refs/heads/main","after":"` + afterSHA + `","head_commit":{"id":"` + afterSHA + `","message":"docs"}}`))
	if !short.Skip || plain.Skip {
		t.Errorf("short %v plain %v", short.Skip, plain.Skip)
	}
}

// GitLab without checkout_sha, and none of the commits is the one pushed:
// no other commit's words or author are taken for it.
func TestParsePushGitLabWithoutTheHeadCommit(t *testing.T) {
	body := `{"object_kind":"push","ref":"refs/heads/main","before":"` + beforeSHA + `","after":"` + afterSHA + `",
	  "commits":[{"id":"` + beforeSHA + `","message":"older [skip deploy]","author":{"name":"Carla"}}]}`
	p, err := ParsePush([]byte(body))
	if err != nil || p.Message != "" || p.Author != "" || p.Skip {
		t.Errorf("push = %+v err %v", p, err)
	}
}

func TestVerifyGitHubDeliveryTakesOnlyGitHubsSignature(t *testing.T) {
	body := []byte(`{"zen":"Keep it logically awesome."}`)
	mac := hmac.New(sha256.New, []byte("whsec"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	h := http.Header{}
	h.Set("X-Hub-Signature-256", "sha256="+sig)
	if err := VerifyGitHubDelivery(h, body, "whsec"); err != nil {
		t.Errorf("GitHub's signature: %v", err)
	}
	for name, h := range map[string]http.Header{
		"GitLab's token":    {"X-Gitlab-Token": {"whsec"}},
		"Gitea's signature": {"X-Gitea-Signature": {sig}},
		"none":              {},
	} {
		if err := VerifyGitHubDelivery(h, body, "whsec"); !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := VerifyGitHubDelivery(h, body, ""); !errors.Is(err, ErrBadSignature) {
		t.Errorf("no secret: %v", err)
	}
}
