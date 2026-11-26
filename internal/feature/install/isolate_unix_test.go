//go:build !windows

package install

import "testing"

// isolateRegistry has nothing to isolate on Linux and macOS: no PATH is
// written there.
func isolateRegistry(t *testing.T) {}

// checkUserPath has nothing to check: zzh only gives advice about PATH.
func checkUserPath(t *testing.T, l layout, expected bool) {}
