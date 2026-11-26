//go:build !windows

package prompt

import (
	"errors"
	"os"

	"golang.org/x/term"
)

// openPlatformTerminal opens /dev/tty, the controlling terminal of the
// process: the same device OpenSSH uses for its own questions. Opening it
// fails when the process has no controlling terminal, which is exactly the
// "nobody to ask" case (cron, systemd services, apps started from the
// desktop). The same file is used to read the answer and print the question.
func openPlatformTerminal() (*terminal, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}

	// /dev/tty should always be a terminal; the check protects against odd
	// environments (containers with a fake /dev) where raw mode would fail
	// later with a less clear error.
	if !term.IsTerminal(int(tty.Fd())) {
		tty.Close()
		return nil, errors.New("/dev/tty is not a terminal")
	}

	return &terminal{
		input:  tty,
		output: tty,
		close:  func() { tty.Close() },
	}, nil
}
