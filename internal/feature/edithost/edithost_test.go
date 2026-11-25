package edithost

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"zzh/internal/core/vault"
	"zzh/internal/core/vault/vaulttest"
)

var reserved = []string{"ls", "add", "edit"}

func noPassword(t *testing.T) PasswordReader {
	return func(string) (string, error) {
		t.Fatalf("the password must not be asked")
		return "", nil
	}
}

// newDatabase holds the host of the user's example: centauri, max, aliases.
func newDatabase(t *testing.T) (string, Modifier) {
	t.Helper()
	path := vaulttest.NewDatabase(t)
	modify := func(change func(v *vault.Vault) error) error { return vaulttest.Modify(path, change) }

	err := modify(func(v *vault.Vault) error {
		return v.AddHost(vault.NewHost{Name: "centauri", Address: "192.168.3.1", User: "max",
			Password: "old", Aliases: []string{"c", "proxmox"}, Tags: []string{"lab"}})
	})
	if err != nil {
		t.Fatalf("cannot prepare database: %v", err)
	}
	return path, modify
}

func findHost(t *testing.T, path, name string) vault.Host {
	t.Helper()
	v, err := vaulttest.Open(path)
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}
	host, err := v.FindHost(name)
	if err != nil {
		t.Fatalf("host %s not found: %v", name, err)
	}
	return host
}

func TestEditUser(t *testing.T) {
	path, modify := newDatabase(t)

	var out bytes.Buffer
	if err := Run([]string{"centauri", "--user", "root"}, reserved, modify, noPassword(t), &out); err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	if !strings.Contains(out.String(), "user root") {
		t.Fatalf("unexpected confirmation: %s", out.String())
	}

	host := findHost(t, path, "centauri")
	if host.User != "root" || host.Password != "old" || len(host.Aliases) != 2 || host.Address != "192.168.3.1" {
		t.Fatalf("only the user must change: %+v", host)
	}
}

func TestEditThroughAliasAndClear(t *testing.T) {
	path, modify := newDatabase(t)

	var out bytes.Buffer
	args := []string{"proxmox", "--address", "root@10.0.0.5:2222", "--tag=", "--clear-password"}
	if err := Run(args, reserved, modify, noPassword(t), &out); err != nil {
		t.Fatalf("edit failed: %v", err)
	}

	host := findHost(t, path, "c")
	if host.Address != "10.0.0.5" || host.Port != 2222 || host.User != "root" || host.Password != "" || len(host.Tags) != 0 {
		t.Fatalf("unexpected host: %+v", host)
	}
}

func TestEditPasswordAndRename(t *testing.T) {
	path, modify := newDatabase(t)

	askPassword := func(message string) (string, error) {
		if !strings.Contains(message, "centauri") {
			t.Fatalf("unexpected prompt %q", message)
		}
		return "new-secret", nil
	}

	var out bytes.Buffer
	if err := Run([]string{"centauri", "--password", "--rename", "alpha"}, reserved, modify, askPassword, &out); err != nil {
		t.Fatalf("edit failed: %v", err)
	}

	host := findHost(t, path, "alpha")
	if host.Password != "new-secret" {
		t.Fatalf("password not changed: %q", host.Password)
	}
}

func TestEditErrorsDoNotWrite(t *testing.T) {
	path, modify := newDatabase(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read database: %v", err)
	}

	var out bytes.Buffer
	for _, args := range [][]string{
		{},
		{"centauri"},
		{"centauri", "extra", "--user", "x"},
		{"centauri", "--rename", "ls"},
		{"centauri", "--rename", "bad name"},
		{"centauri", "--port", "0"},
		{"centauri", "--address", "https://x"},
		{"centauri", "--password", "--clear-password"},
		{"missing", "--user", "x"},
		{"centauri", "--unknown"},
	} {
		if err := Run(args, reserved, modify, noPassword(t), &out); err == nil {
			t.Errorf("%q: expected an error", args)
		}
	}

	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatalf("failed edits must not modify the database")
	}
}

func TestEditStopsWhenPasswordPromptFails(t *testing.T) {
	_, modify := newDatabase(t)
	failing := func(string) (string, error) { return "", errors.New("mismatch") }

	var out bytes.Buffer
	if err := Run([]string{"centauri", "--password"}, reserved, modify, failing, &out); err == nil {
		t.Fatalf("expected the prompt error")
	}
}
