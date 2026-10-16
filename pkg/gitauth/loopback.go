package gitauth

// AllowLoopbackForTests lets another package's tests list repositories on
// test servers at 127.0.0.1, until the test ends. Production code never
// calls it: the guard against the server calling itself stays on.
func AllowLoopbackForTests(t interface{ Cleanup(func()) }) {
	was := allowLoopbackForTests
	allowLoopbackForTests = true
	t.Cleanup(func() { allowLoopbackForTests = was })
}
