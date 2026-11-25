// Package vaulttest creates throw-away zzh databases for the tests of the
// feature modules.
//
// The databases have the zzh layout (Root/SSH/Hosts and Root/SSH/Keys) but
// use the KDBX library's default key derivation, which is very cheap: with
// the real one used by vault.Create every open and save would take about a
// second. The real derivation is covered by the vault and session tests.
package vaulttest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tobischo/gokeepasslib/v3"

	"zzh/internal/core/vault"
)

// Password is the master password of every test database.
const Password = "test-password"

// NewDatabase writes an empty zzh database in a temporary directory of the
// test and returns its path.
func NewDatabase(t testing.TB) string {
	t.Helper()

	named := func(name string, children ...gokeepasslib.Group) gokeepasslib.Group {
		group := gokeepasslib.NewGroup()
		group.Name = name
		group.Groups = children
		return group
	}

	db := gokeepasslib.NewDatabase(gokeepasslib.WithDatabaseKDBXVersion40())
	db.Credentials = gokeepasslib.NewPasswordCredentials(Password)
	db.Content.Root = &gokeepasslib.RootData{
		Groups: []gokeepasslib.Group{named("Root", named("SSH", named("Hosts"), named("Keys")))},
	}
	if err := db.LockProtectedEntries(); err != nil {
		t.Fatalf("cannot lock test database: %v", err)
	}

	path := filepath.Join(t.TempDir(), "test.kdbx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("cannot create test database: %v", err)
	}
	defer file.Close()

	if err := gokeepasslib.NewEncoder(file).Encode(db); err != nil {
		t.Fatalf("cannot encode test database: %v", err)
	}
	return path
}

// Open opens a database created by NewDatabase.
func Open(path string) (*vault.Vault, error) {
	return vault.Open(path, Password, "SSH")
}

// Modify opens the database, applies change and saves it, like
// session.Modify without the password prompt and the retries.
func Modify(path string, change func(v *vault.Vault) error) error {
	v, err := Open(path)
	if err != nil {
		return err
	}
	if err := change(v); err != nil {
		return err
	}
	return v.Save(false)
}
