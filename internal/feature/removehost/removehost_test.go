package removehost

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"zzh/internal/core/vault"
	"zzh/internal/core/vault/vaulttest"
)

func newDatabase(t *testing.T) (string, Modifier) {
	t.Helper()
	path := vaulttest.NewDatabase(t)
	modify := func(change func(v *vault.Vault) error) error { return vaulttest.Modify(path, change) }
	if err := modify(func(v *vault.Vault) error {
		return v.AddHost(vault.NewHost{Name: "centauri", Address: "192.168.3.1", Aliases: []string{"c"}})
	}); err != nil {
		t.Fatalf("cannot prepare database: %v", err)
	}
	return path, modify
}

func answer(value bool) Confirmer {
	return func(string) (bool, error) { return value, nil }
}

func TestRemoveAfterConfirmation(t *testing.T) {
	path, modify := newDatabase(t)

	var out bytes.Buffer
	if err := Run([]string{"c"}, modify, answer(true), &out); err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if !strings.Contains(out.String(), "deleted") {
		t.Fatalf("unexpected message: %s", out.String())
	}

	v, err := vaulttest.Open(path)
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}
	if _, err := v.FindHost("centauri"); !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("host still present: %v", err)
	}
}

func TestRefusedOrUnconfirmableRemovalChangesNothing(t *testing.T) {
	path, modify := newDatabase(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read database: %v", err)
	}

	var out bytes.Buffer
	if err := Run([]string{"centauri"}, modify, answer(false), &out); err != nil {
		t.Fatalf("a refused confirmation is not an error: %v", err)
	}

	// No terminal to ask (scripts): refuse and suggest --yes.
	noTerminal := func(string) (bool, error) { return false, errors.New("stdin is not a terminal") }
	err = Run([]string{"centauri"}, modify, noTerminal, &out)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected an error suggesting --yes, got %v", err)
	}

	for _, args := range [][]string{{}, {"a", "b"}, {"missing", "--yes"}} {
		if err := Run(args, modify, answer(true), &out); err == nil {
			t.Errorf("%q: expected an error", args)
		}
	}

	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatalf("nothing must be written")
	}
}

func TestYesSkipsConfirmation(t *testing.T) {
	_, modify := newDatabase(t)
	neverAsk := func(string) (bool, error) {
		t.Fatalf("--yes must not ask")
		return false, nil
	}

	var out bytes.Buffer
	if err := Run([]string{"centauri", "--yes"}, modify, neverAsk, &out); err != nil {
		t.Fatalf("remove failed: %v", err)
	}
}
