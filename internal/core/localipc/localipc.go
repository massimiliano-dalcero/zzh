// Package localipc creates private, temporary communication endpoints
// between local processes: a named pipe on Windows, a Unix socket on Linux
// and macOS.
//
// zzh uses them while ssh runs, to serve the KeePass key as an ssh-agent and
// to answer askpass requests (D-006), and later the daemon will use them for
// its commands. Every endpoint is reachable only by the current user and
// has a random name, so that it cannot be guessed or reused.
package localipc

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
)

// Endpoint is a listening endpoint.
type Endpoint struct {
	// Listener accepts the connections.
	Listener net.Listener

	// Path is what a client passes to Dial (and what ssh receives as
	// IdentityAgent): a pipe path on Windows, a socket file path elsewhere.
	Path string

	cleanup func() error
}

// Close stops listening and removes what the endpoint left on disk (the
// socket and its private directory on Unix; nothing on Windows).
func (e *Endpoint) Close() error {
	listenErr := e.Listener.Close()
	cleanupErr := e.cleanup()
	if listenErr != nil {
		return listenErr
	}
	return cleanupErr
}

// randomSuffix makes endpoint names unpredictable: another program cannot
// create the same name first, and cannot find the endpoint by guessing.
func randomSuffix() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("cannot generate an endpoint name: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}
