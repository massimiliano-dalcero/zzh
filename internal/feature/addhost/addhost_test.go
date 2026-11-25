package addhost

import (
	"bytes"
	"errors"
	"testing"

	"zzh/internal/core/cliflags"
	"zzh/internal/core/vault"
)

var reserved = []string{"ls", "add", "key"}

func parse(t *testing.T, args ...string) cliflags.Result {
	t.Helper()
	parsed, err := cliflags.Parse(args, cliflags.Spec{
		Values:   []string{"user", "port", "folder", "key", "tag", "option", "alias"},
		Switches: []string{"password"},
	})
	if err != nil {
		t.Fatalf("cannot parse %q: %v", args, err)
	}
	return parsed
}

func TestBuildHost(t *testing.T) {
	host, err := buildHost(parse(t, "web02", "deploy@10.0.0.2:2222", "--folder", "prod", "--tag", "web", "--tag", "prod",
		"--option", "ServerAliveInterval=30", "--key", "laptop"), reserved)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if host.Name != "web02" || host.Address != "10.0.0.2" || host.Port != 2222 || host.User != "deploy" ||
		host.Folder != "prod" || host.KeyName != "laptop" || len(host.Tags) != 2 || len(host.Options) != 1 {
		t.Fatalf("unexpected host: %+v", host)
	}
}

func TestBuildHostOptionsOverrideAddress(t *testing.T) {
	host, err := buildHost(parse(t, "web02", "deploy@10.0.0.2:2222", "--user", "root", "--port", "22"), reserved)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host.User != "root" || host.Port != 22 {
		t.Fatalf("explicit options must win: %+v", host)
	}
}

func TestBuildHostErrors(t *testing.T) {
	cases := map[string][]string{
		"reserved name": {"ls", "10.0.0.1"},
		"invalid name":  {"web 02", "10.0.0.1"},
		"bad address":   {"web02", "https://web02"},
		"bad port":      {"web02", "10.0.0.1", "--port", "0"},
	}
	for name, args := range cases {
		if _, err := buildHost(parse(t, args...), reserved); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// The vault itself is covered by its own tests: here a fake modifier only
// records that the database would be changed, and the confirmation message
// shows what was going to be stored.
func TestRunAsksPasswordAndStoresHost(t *testing.T) {
	modified := false
	modify := func(change func(v *vault.Vault) error) error {
		modified = true
		return nil
	}

	asked := ""
	askPassword := func(message string) (string, error) {
		asked = message
		return "secret", nil
	}

	var out bytes.Buffer
	err := Run([]string{"web02", "10.0.0.2", "--user", "deploy", "--password"}, reserved, modify, askPassword, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !modified {
		t.Fatalf("the database must be modified")
	}
	if asked != "Password for deploy@web02: " {
		t.Fatalf("unexpected password prompt %q", asked)
	}
	if !bytes.Contains(out.Bytes(), []byte("password stored")) {
		t.Fatalf("confirmation must mention the stored password: %s", out.String())
	}
}

func TestRunStopsWhenPasswordFails(t *testing.T) {
	modify := func(change func(v *vault.Vault) error) error {
		t.Fatalf("the database must not be touched when the password prompt fails")
		return nil
	}
	askPassword := func(string) (string, error) { return "", errors.New("mismatch") }

	var out bytes.Buffer
	if err := Run([]string{"web02", "10.0.0.2", "--password"}, reserved, modify, askPassword, &out); err == nil {
		t.Fatalf("expected the prompt error")
	}
}

func TestBuildHostAliases(t *testing.T) {
	host, err := buildHost(parse(t, "centauri", "max@192.168.3.1", "--alias", "c", "--alias", "centauri.lan"), reserved)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(host.Aliases) != 2 || host.Aliases[0] != "c" || host.Aliases[1] != "centauri.lan" {
		t.Fatalf("aliases not passed: %q", host.Aliases)
	}

	// An alias equal to a subcommand could never be used.
	if _, err := buildHost(parse(t, "centauri", "192.168.3.1", "--alias", "ls"), reserved); err == nil {
		t.Fatalf("a reserved alias must be refused")
	}
}
