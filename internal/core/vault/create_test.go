package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tobischo/gokeepasslib/v3"
)

// cheapKDF keeps the tests fast; real databases use calibrateKDF.
var cheapKDF = kdfSettings{Iterations: 1, MemoryBytes: 1024 * 1024, Parallelism: 1}

func TestCreateBuildsReadableDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "new.kdbx")

	if err := createWithKDF(path, testPassword, "personal", "SSH", cheapKDF); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	v, err := Open(path, testPassword, "SSH")
	if err != nil {
		t.Fatalf("cannot reopen: %v", err)
	}
	hosts, err := v.Hosts()
	if err != nil || len(hosts) != 0 {
		t.Fatalf("expected an empty Hosts group, got %v, %v", hosts, err)
	}

	zzhRoot := childGroup(&v.db.Content.Root.Groups[0], "SSH")
	if childGroup(zzhRoot, keysGroupName) == nil {
		t.Fatalf("Keys group missing")
	}
	if v.db.Content.Meta.DatabaseName != "personal" {
		t.Fatalf("database name not stored: %q", v.db.Content.Meta.DatabaseName)
	}
	if !v.db.Content.Meta.RecycleBinEnabled.Bool {
		t.Fatalf("new databases must use the recycle bin, like KeePassXC")
	}

	// The KDF parameters must be the requested ones, not the library's.
	params := v.db.Header.FileHeaders.KdfParameters
	if !bytes.Equal(params.UUID, gokeepasslib.KdfArgon2) || params.Memory != cheapKDF.MemoryBytes {
		t.Fatalf("unexpected KDF parameters: %+v", params)
	}
}

func TestCreateNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.kdbx")
	original := []byte("precious data")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("cannot write test file: %v", err)
	}

	if err := createWithKDF(path, testPassword, "x", "SSH", cheapKDF); err == nil {
		t.Fatalf("create must refuse an existing file")
	}

	content, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(content, original) {
		t.Fatalf("the existing file was modified")
	}
}

func TestCreateRejectsEmptyPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.kdbx")

	if err := createWithKDF(path, "", "x", "SSH", cheapKDF); err == nil {
		t.Fatalf("an empty password must be rejected")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no file must be left behind")
	}
}

func TestCalibrateKDFBounds(t *testing.T) {
	// A target shorter than one pass must still give the minimum.
	settings := calibrateKDF(time.Nanosecond)
	if settings.Iterations != minimumIterations {
		t.Fatalf("expected %d iterations, got %d", minimumIterations, settings.Iterations)
	}
	if settings.MemoryBytes != newDatabaseMemoryBytes {
		t.Fatalf("unexpected memory size %d", settings.MemoryBytes)
	}
}

// A database whose header declares Argon2id must be refused with a clear
// error instead of being reported as a wrong password.
func TestOpenRejectsArgon2id(t *testing.T) {
	db := gokeepasslib.NewDatabase(gokeepasslib.WithDatabaseKDBXVersion40())
	db.Credentials = gokeepasslib.NewPasswordCredentials(testPassword)
	db.Header.FileHeaders.KdfParameters.UUID = argon2idUUID

	path := filepath.Join(t.TempDir(), "argon2id.kdbx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("cannot create file: %v", err)
	}
	if err := db.LockProtectedEntries(); err != nil {
		t.Fatalf("cannot lock entries: %v", err)
	}
	if err := gokeepasslib.NewEncoder(file).Encode(db); err != nil {
		t.Fatalf("cannot encode: %v", err)
	}
	file.Close()

	if _, err := Open(path, testPassword, "SSH"); !errors.Is(err, ErrUnsupportedKDF) {
		t.Fatalf("expected ErrUnsupportedKDF, got %v", err)
	}
}
