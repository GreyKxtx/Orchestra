package provision_test

import "testing"

// noServersOnPath hides the developer's own language servers: Resolve looks on
// PATH before the cache, and a gopls installed on the machine answered for
// the fake one the test put there.
func noServersOnPath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}
