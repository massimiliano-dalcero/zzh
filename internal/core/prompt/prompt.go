// Package prompt asks the user for secrets on the terminal.
package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/term"
)

// ErrNoTerminal is returned when there is nobody to ask: neither stdin nor
// the controlling terminal can be used (scripts, scheduled tasks, programs
// started without a visible console). See openTerminal.
var ErrNoTerminal = errors.New("no terminal to ask")

// Password asks for a secret without echoing it.
//
// The question goes to the terminal chosen by openTerminal: stdin/stderr in
// an ordinary shell, the controlling terminal when the standard streams are
// redirected (git). Either way it never ends up in the output of a command
// redirected to a file or a pipe. The secret is never read from a pipe:
// that would encourage storing it in scripts, which zzh is meant to avoid.
//
// x/term handles the platform differences (termios on Linux/macOS, console
// mode on Windows) and restores the terminal state even if reading fails.
func Password(message string) (string, error) {
	tty, err := openTerminal()
	if err != nil {
		return "", fmt.Errorf("cannot ask for the password: %w", err)
	}
	defer tty.close()

	fmt.Fprint(tty.output, message)
	secret, err := term.ReadPassword(int(tty.input.Fd()))

	// ReadPassword swallows the Enter key, so the newline is printed here to
	// keep the following output on its own line.
	fmt.Fprintln(tty.output)

	if err != nil {
		return "", fmt.Errorf("cannot read the password: %w", err)
	}
	return string(secret), nil
}

// ErrCancelled is returned when the user presses Ctrl+C at a question.
var ErrCancelled = errors.New("cancelled")

// Line asks a question whose answer is not secret (for example the yes/no
// confirmation of a host key) and reads it with echo, from the terminal.
//
// The terminal is switched to raw mode and the line is edited here (echo,
// backspace, Enter, Ctrl+C) instead of relying on the terminal's own line
// mode. The reason: the question may be asked while ssh is running, and the
// Windows OpenSSH client puts the console in a mode where Enter produces
// only "\r" and nothing is echoed, so a normal line read waits forever for
// a "\n" that never comes. Raw mode gives the same behaviour whatever mode
// the console was left in, and the previous mode is restored afterwards.
func Line(message string) (string, error) {
	tty, err := openTerminal()
	if err != nil {
		return "", fmt.Errorf("cannot ask the question: %w", err)
	}
	defer tty.close()

	inputHandle := int(tty.input.Fd())
	fmt.Fprint(tty.output, message)

	previous, err := term.MakeRaw(inputHandle)
	if err != nil {
		return "", fmt.Errorf("cannot prepare the terminal: %w", err)
	}
	line, readErr := readLine(tty.input, tty.output)
	restoreErr := term.Restore(inputHandle, previous)

	// In raw mode Enter does not move to the next line by itself.
	fmt.Fprint(tty.output, "\r\n")

	if readErr != nil {
		return "", readErr
	}
	if restoreErr != nil {
		return "", fmt.Errorf("cannot restore the terminal: %w", restoreErr)
	}
	return line, nil
}

// readLine edits one line from a terminal in raw mode, echoing printable
// characters to echo. It ends at Enter ("\r" or "\n"), handles Backspace
// (DEL or BS) and returns ErrCancelled on Ctrl+C. It is separated from Line
// so that the editing rules can be tested without a terminal.
func readLine(input io.Reader, echo io.Writer) (string, error) {
	var line []rune
	reader := bufio.NewReader(input)

	for {
		char, _, err := reader.ReadRune()
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) > 0 {
				return string(line), nil
			}
			return "", fmt.Errorf("cannot read the answer: %w", err)
		}

		switch {
		case char == '\r' || char == '\n':
			return string(line), nil

		case char == 3: // Ctrl+C
			return "", ErrCancelled

		case char == 127 || char == 8: // Backspace (DEL on most terminals, BS on some)
			if len(line) > 0 {
				line = line[:len(line)-1]
				// Move back, overwrite with a space, move back again.
				fmt.Fprint(echo, "\b \b")
			}

		case char == 27: // ESC: start of an arrow or function key sequence
			skipEscapeSequence(reader)

		case char >= 32:
			line = append(line, char)
			fmt.Fprint(echo, string(char))

			// Other control characters are ignored: a yes/no answer does
			// not need them.
		}
	}
}

// Confirm asks a yes/no question; only an explicit "y" or "yes" confirms,
// so that pressing Enter by habit never performs a destructive action.
func Confirm(message string) (bool, error) {
	answer, err := Line(message + " [y/N] ")
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}

// skipEscapeSequence consumes the rest of a terminal escape sequence, so
// that an arrow key pressed by mistake ("ESC [ A") does not insert "[A"
// into the answer. CSI and SS3 sequences ("ESC [" and "ESC O") end with a
// byte between '@' and '~'.
func skipEscapeSequence(reader *bufio.Reader) {
	introducer, _, err := reader.ReadRune()
	if err != nil || (introducer != '[' && introducer != 'O') {
		return
	}
	for {
		char, _, err := reader.ReadRune()
		if err != nil || (char >= '@' && char <= '~') {
			return
		}
	}
}

// NewPassword asks for a new secret twice and returns it only if both
// entries match: with echo disabled the user cannot see a typo, and a typo
// in a new master password would lock them out of the database.
func NewPassword(message string) (string, error) {
	first, err := Password(message)
	if err != nil {
		return "", err
	}

	second, err := Password("Repeat to confirm: ")
	if err != nil {
		return "", err
	}

	if first != second {
		return "", errors.New("the two passwords do not match")
	}
	return first, nil
}
