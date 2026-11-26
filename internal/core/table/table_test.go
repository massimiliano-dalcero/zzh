package table

import (
	"bytes"
	"strings"
	"testing"

	"zzh/internal/core/style"
)

func sample(s style.Styler) Table {
	t := Table{Headers: []string{"NAME", "PASSWORD"}}
	t.Add(Cell{Text: "web01", Paint: s.Name}, Cell{Text: "stored", Paint: s.Stored})
	t.Add(Plain("città"), Plain(""))
	return t
}

// A buffer is not a terminal: plain layout, as in a pipe.
func TestPlainLayout(t *testing.T) {
	var out bytes.Buffer
	if err := sample(style.Styler{}).Render(&out, false); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	expected := "web01\tstored\ncittà\t-\n"
	if out.String() != expected {
		t.Fatalf("got %q, expected %q", out.String(), expected)
	}
}

func TestBoxedLayoutWithoutColors(t *testing.T) {
	var out bytes.Buffer
	if err := sample(style.Styler{}).RenderBoxedForTest(&out, style.Styler{}); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	expected := strings.Join([]string{
		"┌───────┬──────────┐",
		"│ NAME  │ PASSWORD │",
		"├───────┼──────────┤",
		"│ web01 │ stored   │",
		"│ città │          │",
		"└───────┴──────────┘",
		"",
	}, "\n")
	if out.String() != expected {
		t.Fatalf("got:\n%s\nexpected:\n%s", out.String(), expected)
	}
}

// With colors every line keeps the same visible width (alignment ignores
// color codes) and the second data row carries the stripe background.
func TestBoxedLayoutWithColors(t *testing.T) {
	s := style.Forced(true)
	var out bytes.Buffer
	if err := sample(s).RenderBoxedForTest(&out, s); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	for _, line := range lines {
		if style.Visible(line) != style.Visible(lines[0]) {
			t.Fatalf("misaligned line %q", line)
		}
	}
	on, off := s.StripeBackground()
	if !strings.HasPrefix(lines[4], on) || !strings.HasSuffix(lines[4], off) || strings.Contains(lines[3], on) {
		t.Fatalf("only the second data row must be striped:\n%q\n%q", lines[3], lines[4])
	}
	if !strings.Contains(lines[3], s.Stored("stored")) {
		t.Fatalf("cell colors missing: %q", lines[3])
	}
}
