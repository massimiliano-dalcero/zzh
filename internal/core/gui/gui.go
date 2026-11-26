// Package gui shows the confirmation dialog used when a program without a
// visible terminal asks zzh to connect: who is asking, for which host, the
// master password when the database is locked, and three answers.
//
// No GUI library is linked into zzh. Each system's own tools draw the
// dialog, run as a child process:
//
//	Windows  a WinForms dialog through the PowerShell shipped with Windows
//	macOS    osascript "display dialog"
//	Linux    zenity, or kdialog
//
// The texts reach the child through environment variables, never through
// its command line (which other processes can read), and the password comes
// back through a pipe only zzh reads.
package gui

import (
	"errors"
	"strings"
	"time"
)

// Choice is the user's answer.
type Choice int

const (
	// Deny is also the answer when the dialog times out or is closed.
	Deny Choice = iota
	AllowOnce
	AllowForAWhile
)

// Request describes one dialog.
type Request struct {
	// Heading says who asks for what, in one sentence shown in bold where
	// the platform allows it.
	Heading string

	// Message explains the consequences of the answers.
	Message string

	// Detail is secondary information (the program's full path), shown
	// dimmed where the platform allows it.
	Detail string

	// AskPassword adds a hidden field for the master password, labelled
	// PasswordLabel.
	AskPassword   bool
	PasswordLabel string

	// RememberLabel is the text of the "allow for a while" button, e.g.
	// "Allow for 15 minutes".
	RememberLabel string

	// Timeout closes the dialog with Deny when nobody answers, so that a
	// background program is never blocked for long.
	Timeout time.Duration
}

// Answer is the outcome of a dialog.
type Answer struct {
	Choice   Choice
	Password string
}

// Labels of the fixed buttons.
const (
	labelOnce = "Allow once"
	labelDeny = "Deny"
	title     = "zzh"
)

// ErrUnavailable means that no dialog can be shown: no graphical session
// (a service, an ssh session, a server without display) or no tool to draw
// it. The caller then behaves as before the dialog existed.
var ErrUnavailable = errors.New("no graphical dialog available")

// Available reports whether a dialog can be shown in this session.
func Available() bool {
	return available()
}

// Ask shows the dialog and waits for the answer or the timeout.
func Ask(request Request) (Answer, error) {
	if !available() {
		return Answer{}, ErrUnavailable
	}
	if request.Timeout <= 0 {
		request.Timeout = time.Minute
	}
	return ask(request)
}

// plainText joins heading, message and detail for the platforms whose
// dialogs show a single block of text (macOS, Linux).
func (r Request) plainText() string {
	var parts []string
	for _, part := range []string{r.Heading, r.Message, r.Detail} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "\n\n")
}

// parseAnswer reads what the platform helpers print: the choice ("once",
// "remember" or "deny") on the first line, the password on the rest. Only
// the final newline added by some tools is removed, so that a password
// ending with spaces is kept intact.
func parseAnswer(output string) Answer {
	choice, password, _ := strings.Cut(output, "\n")
	password = strings.TrimSuffix(password, "\n")
	password = strings.TrimSuffix(password, "\r")

	switch strings.TrimSpace(choice) {
	case "once":
		return Answer{Choice: AllowOnce, Password: password}
	case "remember":
		return Answer{Choice: AllowForAWhile, Password: password}
	}
	return Answer{Choice: Deny}
}
