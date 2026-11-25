package vault

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/tobischo/gokeepasslib/v3"
	"golang.org/x/crypto/ssh"

	"zzh/internal/core/sshkey"
)

// openStandard opens a fresh copy of the standard test database.
func openStandard(t *testing.T) (*Vault, string) {
	t.Helper()
	path := standardDatabase(t)
	v, err := Open(path, testPassword, "SSH")
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}
	return v, path
}

func addTestKey(t *testing.T, v *Vault, name string) sshkey.Pair {
	t.Helper()
	pair, err := sshkey.Generate(sshkey.TypeEd25519, 0, name)
	if err != nil {
		t.Fatalf("cannot generate key: %v", err)
	}
	err = v.AddKey(NewKey{
		Name:        name,
		Type:        pair.Type,
		PublicKey:   pair.PublicLine,
		Fingerprint: pair.Fingerprint,
		PrivatePEM:  pair.PrivatePEM,
	})
	if err != nil {
		t.Fatalf("add key failed: %v", err)
	}
	return pair
}

// This is the main end-to-end scenario: a key is created, associated with
// an existing host and used by a new host; after saving, everything must be
// readable again, including the untouched passwords of other entries.
func TestEditSaveAndReopen(t *testing.T) {
	v, path := openStandard(t)
	originalBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read original: %v", err)
	}

	pair := addTestKey(t, v, "laptop-2026")

	if err := v.SetHostKey("web01", "laptop-2026"); err != nil {
		t.Fatalf("set host key failed: %v", err)
	}
	err = v.AddHost(NewHost{
		Name:     "web02",
		Folder:   "prod/new",
		Address:  "10.0.0.2",
		Port:     2222,
		User:     "deploy",
		Password: "fresh-pass",
		KeyName:  "laptop-2026",
		Tags:     []string{"prod", "web"},
		Options:  []string{"ServerAliveInterval=30"},
	})
	if err != nil {
		t.Fatalf("add host failed: %v", err)
	}

	if err := v.Save(true); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// The backup must be the exact previous file.
	backup, err := os.ReadFile(path + BackupSuffix)
	if err != nil || !bytes.Equal(backup, originalBytes) {
		t.Fatalf("backup missing or different from the previous file: %v", err)
	}

	reopened, err := Open(path, testPassword, "SSH")
	if err != nil {
		t.Fatalf("cannot reopen the saved database: %v", err)
	}

	web01, err := reopened.FindHost("web01")
	if err != nil {
		t.Fatalf("web01 lost: %v", err)
	}
	if web01.KeyName != "laptop-2026" {
		t.Fatalf("key association not saved: %+v", web01)
	}
	// Existing protected values must survive the re-encryption.
	if web01.Password != "s3cret" {
		t.Fatalf("existing password corrupted: %q", web01.Password)
	}

	web02, err := reopened.FindHost("web02")
	if err != nil {
		t.Fatalf("web02 not saved: %v", err)
	}
	if web02.Folder != "prod/new" || web02.Address != "10.0.0.2" || web02.Port != 2222 ||
		web02.Password != "fresh-pass" || web02.KeyName != "laptop-2026" ||
		strings.Join(web02.Tags, ",") != "prod,web" || len(web02.Options) != 1 {
		t.Fatalf("web02 saved incorrectly: %+v", web02)
	}

	// The private key must come back byte for byte and be usable.
	privatePEM, err := reopened.PrivateKey("laptop-2026")
	if err != nil {
		t.Fatalf("cannot read private key: %v", err)
	}
	if !bytes.Equal(privatePEM, pair.PrivatePEM) {
		t.Fatalf("private key changed")
	}
	if _, err := ssh.ParsePrivateKey(privatePEM); err != nil {
		t.Fatalf("stored private key does not parse: %v", err)
	}

	// The modified host keeps its previous version in the history.
	located, err := reopened.findHostEntry("web01")
	if err != nil {
		t.Fatalf("cannot find web01 entry: %v", err)
	}
	if len(located.entry.Histories) != 1 || len(located.entry.Histories[0].Entries) != 1 {
		t.Fatalf("history not recorded: %+v", located.entry.Histories)
	}
	if located.entry.Histories[0].Entries[0].GetContent(fieldKey) != "" {
		t.Fatalf("history must hold the state before the change")
	}
}

func TestSaveRegeneratesRandomValues(t *testing.T) {
	v, path := openStandard(t)
	before := readHeader(t, path)

	if err := v.Save(false); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	after := readHeader(t, path)

	if bytes.Equal(before.MasterSeed, after.MasterSeed) ||
		bytes.Equal(before.EncryptionIV, after.EncryptionIV) ||
		before.KdfParameters.Salt == after.KdfParameters.Salt {
		t.Fatalf("seed, IV and salt must change on every save")
	}
	if _, err := os.Stat(path + BackupSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no backup must be written when disabled")
	}
}

// readHeader decodes only what is needed to inspect the file header.
func readHeader(t *testing.T, path string) *gokeepasslib.FileHeaders {
	t.Helper()
	v, err := Open(path, testPassword, "SSH")
	if err != nil {
		t.Fatalf("cannot open: %v", err)
	}
	return v.db.Header.FileHeaders
}

func TestSaveDetectsExternalChanges(t *testing.T) {
	v, path := openStandard(t)

	// Simulate KeePassXC saving the file after zzh opened it.
	external := []byte("saved by another program")
	if err := os.WriteFile(path, external, 0o600); err != nil {
		t.Fatalf("cannot simulate external change: %v", err)
	}

	if err := v.AddHost(NewHost{Name: "new", Address: "new.local"}); err != nil {
		t.Fatalf("add host failed: %v", err)
	}
	if err := v.Save(true); !errors.Is(err, ErrModifiedExternally) {
		t.Fatalf("expected ErrModifiedExternally, got %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(content, external) {
		t.Fatalf("the externally modified file must be left untouched")
	}
}

func TestSaveTwiceFromSameVault(t *testing.T) {
	v, path := openStandard(t)

	if err := v.AddHost(NewHost{Name: "first", Address: "first.local"}); err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if err := v.Save(false); err != nil {
		t.Fatalf("first save failed: %v", err)
	}
	// The second save must compare against the file written by the first.
	if err := v.AddHost(NewHost{Name: "second", Address: "second.local"}); err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if err := v.Save(false); err != nil {
		t.Fatalf("second save failed: %v", err)
	}

	reopened, err := Open(path, testPassword, "SSH")
	if err != nil {
		t.Fatalf("cannot reopen: %v", err)
	}
	for _, name := range []string{"first", "second"} {
		if _, err := reopened.FindHost(name); err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
	}
}

func TestSetHostKeyRemovesAssociation(t *testing.T) {
	v, _ := openStandard(t)
	addTestKey(t, v, "k1")

	if err := v.SetHostKey("web01", "k1"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	if err := v.SetHostKey("web01", ""); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	host, err := v.FindHost("web01")
	if err != nil || host.KeyID != "" {
		t.Fatalf("association not removed: %+v, %v", host, err)
	}
}

func TestEditValidation(t *testing.T) {
	v, _ := openStandard(t)
	addTestKey(t, v, "k1")

	hostCases := map[string]NewHost{
		"duplicate name":  {Name: "web01", Address: "x"},
		"duplicate deep":  {Name: "db01", Address: "x"},
		"space in name":   {Name: "web 03", Address: "x"},
		"dash name":       {Name: "-web", Address: "x"},
		"at in name":      {Name: "root@web", Address: "x"},
		"empty segment":   {Name: "ok", Folder: "prod//eu", Address: "x"},
		"unknown key":     {Name: "ok", Address: "x", KeyName: "missing"},
		"no address":      {Name: "ok"},
		"bad option":      {Name: "ok", Address: "x", Options: []string{"NoEquals"}},
		"bad port":        {Name: "ok", Address: "x", Port: 70000},
		"ambiguous twins": {Name: "twin", Address: "x"},
	}
	for name, host := range hostCases {
		if err := v.AddHost(host); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}

	if err := v.AddKey(NewKey{Name: "k1", Type: "ed25519", PrivatePEM: []byte("x")}); err == nil {
		t.Errorf("duplicate key name must be rejected")
	}
	if err := v.SetHostKey("web01", "missing"); err == nil {
		t.Errorf("unknown key must be rejected")
	}
	if err := v.SetHostKey("twin", "k1"); err == nil {
		t.Errorf("ambiguous host must be rejected")
	}
}

func TestFormatURL(t *testing.T) {
	cases := map[string]struct {
		address string
		port    int
	}{
		"ssh://web01":              {"web01", 0},
		"ssh://web01:2222":         {"web01", 2222},
		"ssh://[2001:db8::1]":      {"2001:db8::1", 0},
		"ssh://[2001:db8::1]:2222": {"2001:db8::1", 2222},
	}
	for expected, tc := range cases {
		if got := formatURL(tc.address, tc.port); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	}
}

func TestHostAliases(t *testing.T) {
	v, path := openStandard(t)

	err := v.AddHost(NewHost{Name: "centauri", Address: "192.168.3.1", User: "max", Aliases: []string{"c", "centauri.lan"}})
	if err != nil {
		t.Fatalf("add host failed: %v", err)
	}
	if err := v.AddHostAliases("web01", []string{"w1", "w1", "front"}); err != nil {
		t.Fatalf("add aliases failed: %v", err)
	}
	if err := v.Save(false); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	reopened, err := Open(path, testPassword, "SSH")
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}

	// Every alias reaches its host, like the name.
	for word, expected := range map[string]string{"c": "centauri", "centauri.lan": "centauri", "w1": "web01", "front": "web01"} {
		host, err := reopened.FindHost(word)
		if err != nil || host.Name != expected {
			t.Errorf("%s: expected %s, got %+v (%v)", word, expected, host, err)
		}
	}
	host, _ := reopened.FindHost("web01")
	if strings.Join(host.Aliases, ",") != "w1,front" {
		t.Fatalf("duplicated alias must be stored once: %q", host.Aliases)
	}

	if err := reopened.RemoveHostAliases([]string{"front", "c"}); err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if _, err := reopened.FindHost("front"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed alias must not resolve: %v", err)
	}
	if host, err := reopened.FindHost("centauri.lan"); err != nil || host.Name != "centauri" {
		t.Fatalf("other aliases must survive: %v", err)
	}
}

// A word must reach at most one host, whether it is a name or an alias.
func TestAliasConflicts(t *testing.T) {
	v, _ := openStandard(t)
	if err := v.AddHost(NewHost{Name: "centauri", Address: "192.168.3.1", Aliases: []string{"c"}}); err != nil {
		t.Fatalf("add host failed: %v", err)
	}

	cases := map[string]error{
		"alias equal to a name":     v.AddHost(NewHost{Name: "x1", Address: "x", Aliases: []string{"web01"}}),
		"name equal to an alias":    v.AddHost(NewHost{Name: "c", Address: "x"}),
		"alias equal to an alias":   v.AddHostAliases("web01", []string{"c"}),
		"alias repeated in the add": v.AddHost(NewHost{Name: "x2", Address: "x", Aliases: []string{"a", "a"}}),
		"invalid alias":             v.AddHost(NewHost{Name: "x3", Address: "x", Aliases: []string{"bad alias"}}),
		"remove a name":             v.RemoveHostAliases([]string{"web01"}),
		"remove unknown alias":      v.RemoveHostAliases([]string{"nope"}),
	}
	for name, err := range cases {
		if err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}

	// An alias equal to the host's own name is simply ignored.
	if err := v.AddHostAliases("centauri", []string{"centauri", "c"}); err != nil {
		t.Fatalf("own words must not conflict: %v", err)
	}
}
