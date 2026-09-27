package ids

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestShortRoundTrip(t *testing.T) {
	for _, id := range []string{uuid.NewString(), uuid.NewString(), "00000000-0000-0000-0000-000000000000", "00000000-0000-0000-0000-000000000001", "ffffffff-ffff-ffff-ffff-ffffffffffff"} {
		short := Short(id)
		if len(short) != 25 {
			t.Errorf("Short(%s) = %q, want 25 characters", id, short)
		}
		if strings.ToLower(short) != short || strings.ContainsAny(short, "-_/.") {
			t.Errorf("Short(%s) = %q must be lowercase alphanumeric (an OCI repository component)", id, short)
		}
		back, err := Decode(short)
		if err != nil || back != id {
			t.Errorf("Decode(Short(%s)) = %q %v", id, back, err)
		}
	}
	if _, err := Decode("not-a-short-id"); err == nil {
		t.Error("garbage must not decode")
	}
	// Distinct ids render distinctly (trivially, but the padding must not
	// collide two values).
	a, b := Short("00000000-0000-0000-0000-000000000001"), Short("00000000-0000-0000-0000-000000000100")
	if a == b {
		t.Error("padding collision")
	}
}
