package prompt

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

// terminal is where a question is asked: the file the answer is read from
// (it must be a terminal, so that echo can be turned off or raw mode set) and
// the writer the question is printed to.
type terminal struct {
	input  *os.File
	output io.Writer

	// close releases what openTerminal opened; it does nothing when the
	// standard streams were used, because those belong to the process.
	close func()
}

// openControllingTerminal opens the terminal the process is attached to,
// bypassing any redirection of the standard streams. It is a variable so
// that tests can replace it: under "go test" run from a console the real
// one would succeed and the test would wait for an answer.
var openControllingTerminal = openPlatformTerminal

// openTerminal finds where to ask a question.
//
// The first choice is stdin, when it is a terminal: this is the plain
// "zzh web01" typed in a shell, and the behaviour stays the one zzh always
// had (question on stderr, answer from stdin).
//
// When stdin is not a terminal, nobody can answer through it, but the user
// may still be sitting at a terminal. The typical case is "git push": git
// starts ssh (that is, zzh) with stdin and stdout connected to pipes that
// carry the git protocol, so zzh must never read or write them, while the
// window the user typed "git push" in is still there. OpenSSH handles this
// by asking on the controlling terminal (/dev/tty), and zzh does the same
// through openControllingTerminal.
//
// ErrNoTerminal means that there is nobody to ask: no terminal at all (cron,
// services, programs started from a desktop menu) or, on Windows, a console
// nobody can see (see terminal_windows.go).
func openTerminal() (*terminal, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return &terminal{input: os.Stdin, output: os.Stderr, close: func() {}}, nil
	}

	controlling, err := openControllingTerminal()
	if err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrNoTerminal, err)
	}
	return controlling, nil
}
