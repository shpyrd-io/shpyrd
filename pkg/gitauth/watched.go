package gitauth

import shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"

// Watched says the App follows a branch or tag of a repository shpyrd
// can read, so the source watcher pins its commit and the webhook takes
// its pushes. A URL Parse refuses (git://) builds as given, as before.
func Watched(app *shpyrdv1.App) bool {
	if !app.WatchesGit() {
		return false
	}
	_, err := Parse(app.Spec.Source.Git.URL)
	return err == nil
}
