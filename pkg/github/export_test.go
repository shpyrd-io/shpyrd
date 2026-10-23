package github

// SetPageCaps lowers the page caps of repository and branch lists, and
// returns what restores them.
func SetPageCaps(repoPages, branchPages int) func() {
	r, b := maxPages, maxBranchPages
	maxPages, maxBranchPages = repoPages, branchPages
	return func() { maxPages, maxBranchPages = r, b }
}
