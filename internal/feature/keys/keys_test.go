package keys

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"zzh/internal/core/config"
	"zzh/internal/core/vault"
	"zzh/internal/core/vault/vaulttest"
)

// newStore returns a Store backed by a fresh test database, with one host
// ("web01") already present.
func newStore(t *testing.T) (Store, string) {
	t.Helper()
	path := vaulttest.NewDatabase(t)

	store := Store{
		Open: func() (*vault.Vault, error) { return vaulttest.Open(path) },
		Modify: func(change func(v *vault.Vault) error) error {
			return vaulttest.Modify(path, change)
		},
	}

	err := store.Modify(func(v *vault.Vault) error {
		return v.AddHost(vault.NewHost{Name: "web01", Address: "10.0.0.1", User: "deploy"})
	})
	if err != nil {
		t.Fatalf("cannot add test host: %v", err)
	}
	return store, path
}

func run(t *testing.T, store Store, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Run(args, store, config.Defaults().Keys, strings.NewReader(""), refuseSecret(t), refuseConfirm(t), &out)
	return out.String(), err
}

func TestGenerateUseAndList(t *testing.T) {
	store, _ := newStore(t)

	out, err := run(t, store, "gen", "laptop", "--comment", "max@laptop")
	if err != nil {
		t.Fatalf("gen failed: %v", err)
	}
	if !strings.Contains(out, "ssh-ed25519 ") || !strings.Contains(out, "max@laptop") {
		t.Fatalf("gen must print the public key:\n%s", out)
	}

	if _, err := run(t, store, "use", "web01", "laptop"); err != nil {
		t.Fatalf("use failed: %v", err)
	}

	out, err = run(t, store, "ls")
	if err != nil {
		t.Fatalf("ls failed: %v", err)
	}
	if !strings.Contains(out, "laptop") || !strings.Contains(out, "web01") {
		t.Fatalf("ls must show the key and the host using it:\n%s", out)
	}

	// pub prints exactly one valid authorized_keys line.
	out, err = run(t, store, "pub", "laptop")
	if err != nil {
		t.Fatalf("pub failed: %v", err)
	}
	if _, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(out)); err != nil || comment != "max@laptop" {
		t.Fatalf("pub output is not a valid public key line: %q (%v)", out, err)
	}

	if _, err := run(t, store, "use", "web01", "--none"); err != nil {
		t.Fatalf("use --none failed: %v", err)
	}
	v, err := store.Open()
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}
	host, err := v.FindHost("web01")
	if err != nil || host.KeyID != "" {
		t.Fatalf("association not removed: %+v (%v)", host, err)
	}
}

func TestGenerateRSAUsesConfiguredSize(t *testing.T) {
	store, _ := newStore(t)

	keysConfig := config.Defaults().Keys
	keysConfig.RSABits = 2048

	var out bytes.Buffer
	if err := Run([]string{"gen", "legacy", "--type", "rsa"}, store, keysConfig, strings.NewReader(""), refuseSecret(t), refuseConfirm(t), &out); err != nil {
		t.Fatalf("gen failed: %v", err)
	}

	v, err := store.Open()
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}
	privatePEM, err := v.PrivateKey("legacy")
	if err != nil {
		t.Fatalf("private key missing: %v", err)
	}
	signer, err := ssh.ParsePrivateKey(privatePEM)
	if err != nil || signer.PublicKey().Type() != "ssh-rsa" {
		t.Fatalf("unexpected stored key: %v", err)
	}
}

func TestErrorsDoNotWrite(t *testing.T) {
	store, path := newStore(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read database: %v", err)
	}

	cases := [][]string{
		{},
		{"gen"},
		{"gen", "bad name"},
		{"gen", "k", "--type", "dsa"},
		{"gen", "k", "--bits", "abc"},
		{"use", "web01"},
		{"use", "web01", "missing-key"},
		{"use", "nowhere", "--none"},
		{"pub", "missing"},
		{"frobnicate"},
	}
	for _, args := range cases {
		if _, err := run(t, store, args...); err == nil {
			t.Errorf("%q: expected an error", args)
		}
	}

	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatalf("failed commands must not modify the database")
	}
}
