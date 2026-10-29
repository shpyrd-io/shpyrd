package store

import (
	"context"
	"testing"
	"time"
)

// #119: every out-of-memory kill is kept, once, and read back by namespace
// and time, newest first.
func TestOOMKillsAreKeptOnce(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			at := time.Date(2026, 10, 8, 14, 3, 7, 0, time.UTC)
			web := OOMKill{WorkspaceID: "ws-1", Project: "abc", Namespace: "p-abc", Pod: "shop-web-7d9f-l8tzg", Container: "app", Process: "web", Instance: "web.1", At: at, LimitBytes: 256 << 20, Size: "shared-m", Release: 12}
			again := web
			again.At = at.Add(90 * time.Second)
			release := OOMKill{WorkspaceID: "ws-1", Project: "abc", Namespace: "p-abc", Pod: "shop-release-v13-x", Container: "release", Process: "release", At: at.Add(time.Hour), LimitBytes: 256 << 20, Release: 13}
			backup := OOMKill{Namespace: "shpyrd-system", Pod: "platform-backup-29330-q", Container: "backup", Process: "platform-backup", At: at.Add(-time.Hour), LimitBytes: 512 << 20}

			n, err := s.RecordOOMKills(ctx, []OOMKill{web, again, release, backup})
			if err != nil || n != 4 {
				t.Fatalf("record = %d, %v", n, err)
			}
			// The same death reported on the next status change is nothing new.
			n, err = s.RecordOOMKills(ctx, []OOMKill{web, again.Clone(), release})
			if err != nil || n != 0 {
				t.Fatalf("second record = %d, %v", n, err)
			}

			got, err := s.QueryOOMKills(ctx, OOMKillQuery{Namespace: "p-abc", From: at.Add(-time.Minute), To: at.Add(2 * time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 3 || got[0].Pod != release.Pod || got[1].Pod != web.Pod || !got[1].At.Equal(again.At) || got[2].Instance != "web.1" || got[2].Size != "shared-m" || got[2].Release != 12 || got[2].LimitBytes != 256<<20 {
				t.Fatalf("project kills = %+v", got)
			}
			// The window has a start and an end; the end is open.
			got, err = s.QueryOOMKills(ctx, OOMKillQuery{Namespace: "p-abc", From: at, To: at.Add(time.Hour)})
			if err != nil || len(got) != 2 {
				t.Fatalf("window = %+v, %v", got, err)
			}
			// The operator's list: every namespace, the platform's own among them.
			got, err = s.QueryOOMKills(ctx, OOMKillQuery{From: at.Add(-2 * time.Hour), To: at.Add(2 * time.Hour)})
			if err != nil || len(got) != 4 || got[3].Namespace != "shpyrd-system" || got[3].WorkspaceID != "" || got[3].Project != "" {
				t.Fatalf("all kills = %+v, %v", got, err)
			}
			if got, err := s.QueryOOMKills(ctx, OOMKillQuery{Namespace: "p-other"}); err != nil || len(got) != 0 {
				t.Fatalf("other namespace = %+v, %v", got, err)
			}
		})
	}
}

// Clone is the same kill as a different value, the way a second status
// read reports it.
func (k OOMKill) Clone() OOMKill { return k }
