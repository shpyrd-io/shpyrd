package gitauth

import "testing"

// AllowLoopbackForTests lets another package's tests list repositories on
// test servers at 127.0.0.1, until the test ends. It panics outside a test
// binary: the guard against the server calling itself stays on.
func AllowLoopbackForTests(t testing.TB) {
	if !testing.Testing() {
		panic("gitauth: AllowLoopbackForTests outside a test")
	}
	was := allowLoopbackForTests.Swap(true)
	t.Cleanup(func() { allowLoopbackForTests.Store(was) })
}
