package keys

import (
	"bytes"
	"testing"

	"zzh/internal/core/style"
	"zzh/internal/core/vault"
)

// The plain layout (pipes, --grep): one line per key, an unused key is
// reported as such so that "zzh key ls | grep unused" finds candidates for
// removal.
func TestKeyTableGrepLayout(t *testing.T) {
	keyList := []vault.Key{
		{ID: "A", Name: "laptop", Type: "ed25519", Fingerprint: "SHA256:aaa"},
		{ID: "B", Name: "old", Type: "rsa", Fingerprint: "SHA256:bbb"},
	}
	usedBy := map[string][]string{"A": {"web01", "web02"}}

	var out bytes.Buffer
	if err := keyTable(keyList, usedBy, style.Styler{}).Render(&out, true); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	expected := "laptop\ted25519\tSHA256:aaa\tweb01,web02\nold\trsa\tSHA256:bbb\tunused\n"
	if out.String() != expected {
		t.Fatalf("got %q, expected %q", out.String(), expected)
	}
}

func TestKeyListRejectsUnknownArguments(t *testing.T) {
	if err := list([]string{"--all"}, Store{}, &bytes.Buffer{}); err == nil {
		t.Fatalf("an unknown argument must be refused before opening the database")
	}
}
