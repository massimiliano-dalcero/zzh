//go:build !windows

package syncout

// restrictToOwner is a no-op on Linux and macOS: private files are created
// with mode 0600 in writeFile, which is exactly what ssh requires.
func restrictToOwner(path string) error {
	return nil
}
