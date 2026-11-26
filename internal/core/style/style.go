// Package style colors zzh's output on terminals, with colors that carry a
// meaning instead of decoration:
//
//	red      errors, failed checks
//	yellow   warnings, situations to notice
//	green    success, passed checks
//	cyan     commands to type, tags
//	magenta  stored login passwords ("stored"), to stand out in listings
//	bold     names of hosts, keys, databases; table headers
//	dim      secondary details: paths, fingerprints, table borders
//
// Colors are used only when the output goes to a terminal: a pipe, a file or
// git always receive plain text, so scripts keep working. The NO_COLOR
// environment variable (https://no-color.org) and the output.color setting
// ("auto", "always", "never") turn them off or force them.
//
// Each attribute is switched off with its own code ("normal intensity",
// "default color") rather than with a full reset, so that coloring one cell
// does not cancel the background of a striped table row around it.
package style

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// Color modes, as written in output.color.
const (
	ModeAuto   = "auto"
	ModeAlways = "always"
	ModeNever  = "never"
)

// settings are chosen once at startup by Configure, from the configuration
// file; before that (or when the file cannot be read) "auto" applies.
var settings = struct {
	mode    string
	stripes bool
}{mode: ModeAuto, stripes: true}

// Configure applies output.color and output.stripes.
func Configure(mode string, stripes bool) {
	settings.mode = mode
	settings.stripes = stripes
}

// Styler colors text for one output stream. The zero value is a styler with
// colors off, which returns every text unchanged.
type Styler struct {
	enabled bool
	stripes bool
}

// For returns the styler for w. Only an *os.File connected to a terminal
// can get colors; anything else (buffers in tests, pipes, files) stays
// plain, so output written to it is exactly what zzh always printed.
func For(w io.Writer) Styler {
	file, ok := w.(*os.File)
	if !ok {
		return Styler{}
	}

	switch settings.mode {
	case ModeNever:
		return Styler{}
	case ModeAlways:
		enableColors(file) // best effort: the user asked for colors anyway
		return Styler{enabled: true, stripes: settings.stripes}
	}

	if os.Getenv("NO_COLOR") != "" || !IsTerminal(w) || !enableColors(file) {
		return Styler{}
	}
	return Styler{enabled: true, stripes: settings.stripes}
}

// IsTerminal reports whether w is a terminal; tables use it to choose
// between the boxed layout and the plain one meant for grep.
func IsTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

// Enabled reports whether this styler adds colors.
func (s Styler) Enabled() bool { return s.enabled }

// Stripes reports whether tables should alternate row backgrounds.
func (s Styler) Stripes() bool { return s.enabled && s.stripes }

// paint wraps text between the codes that switch an attribute on and off.
func (s Styler) paint(text, on, off string) string {
	if !s.enabled || text == "" {
		return text
	}
	return "\x1b[" + on + "m" + text + "\x1b[" + off + "m"
}

// The SGR codes used below: 1/22 bold on/off, 2/22 dim on/off, 3x/39 a
// foreground color / default color. Bright variants (9x) read better on
// the dark backgrounds most terminals use.

func (s Styler) Error(text string) string   { return s.paint(text, "1;91", "22;39") }
func (s Styler) Warning(text string) string { return s.paint(text, "93", "39") }
func (s Styler) Success(text string) string { return s.paint(text, "92", "39") }
func (s Styler) Command(text string) string { return s.paint(text, "96", "39") }
func (s Styler) Tag(text string) string     { return s.paint(text, "36", "39") }
func (s Styler) Stored(text string) string  { return s.paint(text, "1;95", "22;39") }
func (s Styler) Name(text string) string    { return s.paint(text, "1", "22") }
func (s Styler) Dim(text string) string     { return s.paint(text, "2", "22") }

// StripeBackground returns the codes that give a table row its alternate
// background (a dark gray from the 256-color palette) and switch it off.
func (s Styler) StripeBackground() (on, off string) {
	if !s.Stripes() {
		return "", ""
	}
	return "\x1b[48;5;236m", "\x1b[49m"
}

// Commands colors every "zzh …" command quoted in a message, the suggestions
// such as `run "zzh unlock"`, so that what to type stands out.
func (s Styler) Commands(message string) string {
	if !s.enabled {
		return message
	}
	var out strings.Builder
	rest := message
	for {
		start := strings.Index(rest, `"zzh `)
		if start < 0 {
			out.WriteString(rest)
			return out.String()
		}
		end := strings.Index(rest[start+1:], `"`)
		if end < 0 {
			out.WriteString(rest)
			return out.String()
		}
		end += start + 2 // include both quotes
		out.WriteString(rest[:start])
		out.WriteString(s.Command(rest[start:end]))
		rest = rest[end:]
	}
}

// Visible returns the length of text as seen on screen, ignoring color
// codes: tables need it to align columns.
func Visible(text string) int {
	width := 0
	inEscape := false
	for _, r := range text {
		switch {
		case inEscape:
			if r == 'm' {
				inEscape = false
			}
		case r == '\x1b':
			inEscape = true
		default:
			width++
		}
	}
	return width
}

// Forced returns a styler with colors on, for tests of colored output.
func Forced(stripes bool) Styler {
	return Styler{enabled: true, stripes: stripes}
}

// PrintError writes an error as zzh always did ("zzh: <message>"), with the
// prefix in red and the suggested commands in cyan when w is a terminal.
func PrintError(w io.Writer, message string) {
	s := For(w)
	fmt.Fprintln(w, s.Error("zzh:"), s.Commands(message))
}

// PrintNotice writes a warning or notice ("zzh: <message>") with the prefix
// in yellow: something to notice that did not stop the command.
func PrintNotice(w io.Writer, message string) {
	s := For(w)
	fmt.Fprintln(w, s.Warning("zzh:"), s.Commands(message))
}
