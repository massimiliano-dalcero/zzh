package aliases

import (
	"bytes"
	"os"
	"testing"

	"zzh/internal/core/vault"
	"zzh/internal/core/vault/vaulttest"
)

var reserved = []string{"ls", "add", "alias"}

func newDatabase(t *testing.T) (string, Modifier) {
	t.Helper()
	path := vaulttest.NewDatabase(t)
	modify := func(change func(v *vault.Vault) error) error { return vaulttest.Modify(path, change) }

	if err := modify(func(v *vault.Vault) error {
		return v.AddHost(vault.NewHost{Name: "centauri", Address: "192.168.3.1", User: "max"})
	}); err != nil {
		t.Fatalf("cannot prepare database: %v", err)
	}
	return path, modify
}

func TestAddAndRemoveAliases(t *testing.T) {
	path, modify := newDatabase(t)
	var out bytes.Buffer

	if err := Run([]string{"add", "centauri", "c", "centauri.lan"}, reserved, modify, &out); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	v, err := vaulttest.Open(path)
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}
	if host, err := v.FindHost("c"); err != nil || host.Name != "centauri" {
		t.Fatalf("alias not usable: %+v (%v)", host, err)
	}

	if err := Run([]string{"rm", "c"}, reserved, modify, &out); err != nil {
		t.Fatalf("rm failed: %v", err)
	}
	v, _ = vaulttest.Open(path)
	if _, err := v.FindHost("c"); err == nil {
		t.Fatalf("removed alias still resolves")
	}
	if _, err := v.FindHost("centauri.lan"); err != nil {
		t.Fatalf("the other alias must remain: %v", err)
	}
}

func TestErrorsDoNotWrite(t *testing.T) {
	path, modify := newDatabase(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read database: %v", err)
	}

	var out bytes.Buffer
	for _, args := range [][]string{
		{},
		{"add", "centauri"},
		{"add", "centauri", "ls"},
		{"add", "missing", "x"},
		{"add", "centauri", "bad alias"},
		{"rm"},
		{"rm", "nope"},
		{"rm", "centauri"},
		{"list"},
	} {
		if err := Run(args, reserved, modify, &out); err == nil {
			t.Errorf("%q: expected an error", args)
		}
	}

	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatalf("failed commands must not modify the database")
	}
}
