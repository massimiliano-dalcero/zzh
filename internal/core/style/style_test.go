package style

import (
	"bytes"
	"testing"
)

// Anything that is not a terminal gets plain text: this keeps the output
// for pipes, files and tests exactly as before.
func TestPlainWhenNotTerminal(t *testing.T) {
	s := For(&bytes.Buffer{})
	if s.Enabled() || s.Error("boom") != "boom" || s.Stored("stored") != "stored" {
		t.Fatalf("a buffer must never receive colors")
	}
	if on, off := s.StripeBackground(); on != "" || off != "" {
		t.Fatalf("no stripes without colors")
	}
}

func TestPaintUsesSpecificOffCodes(t *testing.T) {
	s := Forced(true)
	// Bold is switched off with 22, not with a full reset (0), which would
	// also cancel the row background of a striped table.
	if got := s.Name("web01"); got != "\x1b[1mweb01\x1b[22m" {
		t.Fatalf("unexpected codes %q", got)
	}
	if got := s.Error(""); got != "" {
		t.Fatalf("empty text must stay empty, got %q", got)
	}
}

func TestCommands(t *testing.T) {
	s := Forced(false)
	got := s.Commands(`the database is locked: run "zzh unlock" first, or "zzh help"`)
	expected := `the database is locked: run ` + s.Command(`"zzh unlock"`) + ` first, or ` + s.Command(`"zzh help"`)
	if got != expected {
		t.Fatalf("got %q", got)
	}
	if plain := (Styler{}).Commands(`run "zzh unlock"`); plain != `run "zzh unlock"` {
		t.Fatalf("without colors the message must be unchanged")
	}
	if odd := s.Commands(`a "zzh without end`); odd != `a "zzh without end` {
		t.Fatalf("an unterminated quote must be left alone, got %q", odd)
	}
}

func TestVisible(t *testing.T) {
	s := Forced(false)
	if got := Visible(s.Stored("stored") + " " + s.Name("città")); got != len("stored ")+5 {
		t.Fatalf("visible width ignores codes and counts runes, got %d", got)
	}
}
