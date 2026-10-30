package host

import "testing"

// setupTempHome points HOME (and USERPROFILE for Windows) at a fresh temp dir
// so the local metadata file resolves inside it. Deliberately duplicated per
// package instead of shared: five lines do not justify a test helper package.
func setupTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}
