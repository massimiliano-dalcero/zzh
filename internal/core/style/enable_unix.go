//go:build !windows

package style

import "os"

// enableColors has nothing to do on Linux and macOS: terminals understand
// color codes natively.
func enableColors(file *os.File) bool {
	return true
}
