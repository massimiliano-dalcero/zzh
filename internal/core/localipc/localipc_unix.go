//go:build !windows

package localipc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Listen creates a Unix socket inside a new private directory.
//
// os.MkdirTemp creates the directory with mode 0700, so only the current
// user can reach the socket inside it, whatever the socket's own mode (some
// systems ignore socket permissions, none ignores the directory's). The
// directory has a short name in the system temp folder because socket
// paths are limited to about 100 characters.
func Listen(purpose string) (*Endpoint, error) {
	suffix, err := randomSuffix()
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "zzh-"+suffix[:8]+"-")
	if err != nil {
		return nil, fmt.Errorf("cannot create a private directory: %w", err)
	}

	path := filepath.Join(dir, purpose+".sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		// Best effort: the directory is empty and was created just above.
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("cannot create the socket %s: %w", path, err)
	}

	return &Endpoint{
		Listener: listener,
		Path:     path,
		cleanup:  func() error { return os.RemoveAll(dir) },
	}, nil
}

// Dial connects to an endpoint created by Listen.
func Dial(path string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", path, timeout)
}
