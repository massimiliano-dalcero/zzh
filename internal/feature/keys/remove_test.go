package keys

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"zzh/internal/core/config"
	"zzh/internal/core/vault"
)

func refuseConfirm(t *testing.T) Confirmer {
	return func(string) (bool, error) {
		t.Fatalf("no confirmation must be asked here")
		return false, nil
	}
}

func runRemove(t *testing.T, store Store, confirmed bool, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	confirm := func(string) (bool, error) { return confirmed, nil }
	err := Run(append([]string{"rm"}, args...), store, config.Defaults().Keys, strings.NewReader(""), refuseSecret(t), confirm, &out)
	return out.String(), err
}

func TestRemoveKey(t *testing.T) {
	store, _ := newStore(t)
	if _, err := run(t, store, "gen", "old"); err != nil {
		t.Fatalf("gen failed: %v", err)
	}
	if _, err := run(t, store, "use", "web01", "old"); err != nil {
		t.Fatalf("use failed: %v", err)
	}

	// In use: refused, and the message names the host.
	if _, err := runRemove(t, store, true, "old"); err == nil || !strings.Contains(err.Error(), "web01") {
		t.Fatalf("a key in use must be refused naming its hosts, got %v", err)
	}

	// Not confirmed: nothing happens.
	if output, err := runRemove(t, store, false, "old", "--force"); err != nil || !strings.Contains(output, "Nothing removed") {
		t.Fatalf("an unconfirmed removal must do nothing: %v %q", err, output)
	}

	output, err := runRemove(t, store, true, "old", "--force")
	if err != nil || !strings.Contains(output, "No longer used by: web01") {
		t.Fatalf("forced removal failed: %v %q", err, output)
	}

	v, err := store.Open()
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}
	if _, err := v.FindKey("old"); !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("key still present: %v", err)
	}
	if host, _ := v.FindHost("web01"); host.KeyID != "" {
		t.Fatalf("web01 must no longer use the removed key")
	}
}

func TestRemoveKeyWithoutTerminalNeedsYes(t *testing.T) {
	store, _ := newStore(t)
	if _, err := run(t, store, "gen", "k"); err != nil {
		t.Fatalf("gen failed: %v", err)
	}

	var out bytes.Buffer
	noTerminal := func(string) (bool, error) { return false, errors.New("stdin is not a terminal") }
	err := Run([]string{"rm", "k"}, store, config.Defaults().Keys, strings.NewReader(""), refuseSecret(t), noTerminal, &out)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected an error suggesting --yes, got %v", err)
	}

	if _, err := runRemove(t, store, false, "k", "--yes"); err != nil {
		t.Fatalf("--yes must remove without asking: %v", err)
	}
}
