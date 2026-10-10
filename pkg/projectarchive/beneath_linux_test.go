//go:build linux

package projectarchive

import "testing"

// The kernel's one call and the walk refuse the same writes; a node whose
// kernel or runtime lacks the call is as safe as one that has it.
func TestStagingTreeRefusesWritingThroughLinksWithoutOpenat2(t *testing.T) {
	was := openat2Available
	openat2Available = false
	t.Cleanup(func() { openat2Available = was })
	checkStagingTreeRefusals(t)
}

func TestStagingTreeUsesOpenat2WhereTheKernelHasIt(t *testing.T) {
	openat2Available = true
	checkStagingTreeRefusals(t)
	if !openat2Available {
		t.Skip("this kernel or runtime has no openat2; the walk was checked")
	}
}
