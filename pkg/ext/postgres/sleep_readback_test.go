package postgres

import (
	"bytes"
	"strings"
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/ext/resources"
)

// #120: `shpyrd pg sleep <name>` with no flag says what applies and where
// it comes from; a shared database is simply always on.
func TestPgSleepReadBack(t *testing.T) {
	for _, tc := range []struct {
		details map[string]string
		want    []string
	}{
		{map[string]string{"type": "shared"}, []string{"db is a shared database: always on, it never sleeps."}},
		{map[string]string{"type": "dedicated", "sleepState": "awake", "sleepAfter": "30m", "sleepSource": "project", "sleepMessage": "awake: idle for 2m0s of 30m0s"}, []string{"db hibernates after 30m without client connections, by a policy of its own.", "Awake: idle for 2m0s of 30m0s", "Change it: shpyrd pg sleep db --after 30m|off|default"}},
		{map[string]string{"type": "dedicated", "sleepState": "sleeping", "sleepAfter": "10m0s", "sleepSource": "workspace"}, []string{"db follows the workspace's default: it hibernates after 10m0s without client connections.", "Asleep now; the first connection wakes it."}},
		{map[string]string{"type": "dedicated", "sleepState": "awake", "sleepSource": "workspace"}, []string{"db follows the workspace's default, which sets no sleep for databases: it stays awake."}},
		{map[string]string{"type": "dedicated", "sleepState": "suspended", "sleepSource": "off"}, []string{"Sleep is off for db: it stays awake, also when the workspace has a default for databases.", "Suspended now: connections are refused until `shpyrd pg resume db`."}},
		{map[string]string{"type": "dedicated"}, []string{"db has no sleep state yet"}},
	} {
		var out bytes.Buffer
		printPgSleepReadBack(&out, "db", &resources.View{Name: "db", Details: tc.details})
		for _, w := range tc.want {
			if !strings.Contains(out.String(), w) {
				t.Errorf("%v printed:\n%s\nwant %q", tc.details, out.String(), w)
			}
		}
	}
}
