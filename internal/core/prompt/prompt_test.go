package prompt

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestReadLine(t *testing.T) {
	cases := []struct {
		name  string
		input string
		line  string
		echo  string
	}{
		// The Windows console in raw mode sends only "\r" for Enter.
		{"enter as CR", "yes\r", "yes", "yes"},
		{"enter as LF", "yes\n", "yes", "yes"},
		{"backspace DEL", "yex\x7fs\r", "yes", "yex\b \bs"},
		{"backspace BS", "yex\x08s\r", "yes", "yex\b \bs"},
		{"backspace on empty line", "\x7fno\r", "no", "no"},
		{"arrow keys ignored", "y\x1b[Ae\x1bOBs\r", "yes", "yes"},
		{"delete key ignored", "ye\x1b[3~s\r", "yes", "yes"},
		{"utf-8", "sì\r", "sì", "sì"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var echo bytes.Buffer
			line, err := readLine(strings.NewReader(tc.input), &echo)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if line != tc.line || echo.String() != tc.echo {
				t.Fatalf("got line=%q echo=%q, expected line=%q echo=%q", line, echo.String(), tc.line, tc.echo)
			}
		})
	}
}

func TestReadLineCancel(t *testing.T) {
	var echo bytes.Buffer
	if _, err := readLine(strings.NewReader("ye\x03s\r"), &echo); !errors.Is(err, ErrCancelled) {
		t.Fatalf("Ctrl+C must cancel, got %v", err)
	}
}

func TestReadLineEOF(t *testing.T) {
	var echo bytes.Buffer
	if line, err := readLine(strings.NewReader("yes"), &echo); err != nil || line != "yes" {
		t.Fatalf("a final line without Enter must be accepted: %q %v", line, err)
	}
	if _, err := readLine(strings.NewReader(""), &echo); err == nil {
		t.Fatalf("an empty input must be an error")
	}
}

func TestLineWithoutTerminal(t *testing.T) {
	if _, err := Line("question? "); !errors.Is(err, ErrNoTerminal) {
		t.Fatalf("expected ErrNoTerminal under go test, got %v", err)
	}
}
