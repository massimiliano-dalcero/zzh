//go:build windows

package localipc

import (
	"fmt"
	"net"
	"os/user"
	"time"

	"github.com/Microsoft/go-winio"
)

// Listen creates a named pipe \\.\pipe\zzh-<purpose>-<random>.
//
// The security descriptor "D:P(A;;GA;;;<user SID>)" is a protected DACL
// (P: no inherited entries) with a single entry granting full access to the
// current user: no other account, not even administrators through
// inheritance, can open the pipe.
func Listen(purpose string) (*Endpoint, error) {
	suffix, err := randomSuffix()
	if err != nil {
		return nil, err
	}

	current, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("cannot determine the current user: %w", err)
	}

	path := `\\.\pipe\zzh-` + purpose + "-" + suffix
	listener, err := winio.ListenPipe(path, &winio.PipeConfig{
		// On Windows user.Current().Uid is the account SID.
		SecurityDescriptor: "D:P(A;;GA;;;" + current.Uid + ")",
	})
	if err != nil {
		return nil, fmt.Errorf("cannot create the pipe %s: %w", path, err)
	}

	return &Endpoint{
		Listener: listener,
		Path:     path,
		// A named pipe disappears with its last handle: nothing to remove.
		cleanup: func() error { return nil },
	}, nil
}

// Dial connects to an endpoint created by Listen.
func Dial(path string, timeout time.Duration) (net.Conn, error) {
	return winio.DialPipe(path, &timeout)
}
