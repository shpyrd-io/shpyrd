package controller

import "testing"

// #93: the same archive at another address (another port, a token, a
// renamed host) is not a new source; another archive is.
func TestSameArchiveIsNamedByItsDigest(t *testing.T) {
	sha := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	a := "http://shpyrd-server.shpyrd-system.svc:8080/api/sources/" + sha + ".tgz"
	b := "http://shpyrd-server.shpyrd-system.svc:8081/api/sources/" + sha + ".tgz?token=abc"
	if !sameArchive(a, b) {
		t.Error("another port and a token: the same archive")
	}
	other := "http://shpyrd-server.shpyrd-system.svc:8081/api/sources/" + "f" + sha[1:] + ".tgz"
	if sameArchive(a, other) {
		t.Error("another digest: another archive")
	}
	if sameArchive(a, "http://shpyrd-server/api/sources/latest.tgz") || sameArchive("", "") {
		t.Error("a file not named by a digest, or nothing, is never the same archive")
	}
}
