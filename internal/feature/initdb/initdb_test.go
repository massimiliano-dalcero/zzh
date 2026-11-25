package initdb

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zzh/internal/core/config"
	"zzh/internal/core/vault"
)

const testPassword = "correct horse battery staple"

func fixedPassword(string) (string, error) { return testPassword, nil }

func failIfAsked(t *testing.T) PasswordReader {
	return func(string) (string, error) {
		t.Fatalf("the password must not be asked when the arguments are invalid")
		return "", nil
	}
}

func emptyConfig(t *testing.T) config.Loaded {
	return config.Loaded{
		Config: config.Defaults(),
		Path:   filepath.Join(t.TempDir(), "zzh", "config.json"),
	}
}

// This test runs the real key derivation (about one second per unlock), so
// it covers creation, reopening and the config update in one pass.
func TestInitCreatesDatabaseAndBookmark(t *testing.T) {
	loaded := emptyConfig(t)
	dbPath := filepath.Join(t.TempDir(), "personal.kdbx")

	var out bytes.Buffer
	if err := Run([]string{"personal", dbPath}, loaded, nil, fixedPassword, &out); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	v, err := vault.Open(dbPath, testPassword, "SSH")
	if err != nil {
		t.Fatalf("the new database cannot be opened: %v", err)
	}
	if _, err := v.Hosts(); err != nil {
		t.Fatalf("the new database has no Hosts group: %v", err)
	}

	saved, exists, err := config.Load(loaded.Path)
	if err != nil || !exists {
		t.Fatalf("config not saved: %v", err)
	}
	if saved.DefaultDatabase != "personal" || saved.Databases["personal"].Path != dbPath {
		t.Fatalf("bookmark not saved correctly: %+v", saved)
	}
	if !strings.Contains(out.String(), "default database") {
		t.Fatalf("the user must be told it is the default:\n%s", out.String())
	}
}

func TestInitRejectsBeforeAskingPassword(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "existing.kdbx")
	if err := os.WriteFile(existing, []byte("x"), 0o600); err != nil {
		t.Fatalf("cannot write test file: %v", err)
	}

	bookmarked := emptyConfig(t)
	bookmarked.Config.Databases = map[string]config.Database{"work": {Path: "w.kdbx"}}

	cases := map[string]struct {
		args    []string
		loaded  config.Loaded
		loadErr error
	}{
		"missing arguments": {[]string{"personal"}, emptyConfig(t), nil},
		"invalid name":      {[]string{"my db", "x.kdbx"}, emptyConfig(t), nil},
		"existing file":     {[]string{"personal", existing}, emptyConfig(t), nil},
		"name taken":        {[]string{"work", filepath.Join(t.TempDir(), "n.kdbx")}, bookmarked, nil},
		"broken config":     {[]string{"personal", "x.kdbx"}, emptyConfig(t), errors.New("invalid config")},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Run(tc.args, tc.loaded, tc.loadErr, failIfAsked(t), &out); err == nil {
				t.Fatalf("expected an error")
			}
		})
	}
}
