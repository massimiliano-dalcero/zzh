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

// noExisting fails the test if init tries to use an existing file.
func noExisting(t *testing.T) Existing {
	return Existing{
		Confirm: func(string) (bool, error) {
			t.Fatalf("no confirmation expected")
			return false, nil
		},
		Add: func(string, string) error {
			t.Fatalf("no existing file expected")
			return nil
		},
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
	if err := Run([]string{"personal", dbPath}, loaded, nil, fixedPassword, noExisting(t), &out); err != nil {
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
	bookmarked := emptyConfig(t)
	bookmarked.Config.Databases = map[string]config.Database{"work": {Path: "w.kdbx"}}

	cases := map[string]struct {
		args    []string
		loaded  config.Loaded
		loadErr error
	}{
		"missing arguments": {[]string{"personal"}, emptyConfig(t), nil},
		"invalid name":      {[]string{"my db", "x.kdbx"}, emptyConfig(t), nil},
		"folder":            {[]string{"personal", t.TempDir()}, emptyConfig(t), nil},
		"name taken":        {[]string{"work", filepath.Join(t.TempDir(), "n.kdbx")}, bookmarked, nil},
		"broken config":     {[]string{"personal", "x.kdbx"}, emptyConfig(t), errors.New("invalid config")},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Run(tc.args, tc.loaded, tc.loadErr, failIfAsked(t), noExisting(t), &out); err == nil {
				t.Fatalf("expected an error")
			}
		})
	}
}

// An existing file is never overwritten: after a yes it is handed to the
// "db add" logic, after a no nothing happens. No new password is asked.
func TestInitOnExistingFile(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "existing.kdbx")
	if err := os.WriteFile(existing, []byte("original"), 0o600); err != nil {
		t.Fatalf("cannot write test file: %v", err)
	}

	for _, answer := range []bool{true, false} {
		var question, added string
		handler := Existing{
			Confirm: func(q string) (bool, error) { question = q; return answer, nil },
			Add:     func(name, path string) error { added = name + "=" + path; return nil },
		}
		var out bytes.Buffer
		if err := Run([]string{"personal", existing}, emptyConfig(t), nil, failIfAsked(t), handler, &out); err != nil {
			t.Fatalf("answer %v: unexpected error %v", answer, err)
		}
		if !strings.Contains(question, "already exists") {
			t.Fatalf("the user must be asked, got %q", question)
		}
		if answer && added != "personal="+existing {
			t.Fatalf("yes must use the existing file, got %q", added)
		}
		if !answer && (added != "" || !strings.Contains(out.String(), "Nothing done")) {
			t.Fatalf("no must change nothing: added %q, output %q", added, out.String())
		}
		if data, _ := os.ReadFile(existing); string(data) != "original" {
			t.Fatalf("the existing file must never be overwritten")
		}
	}
}
