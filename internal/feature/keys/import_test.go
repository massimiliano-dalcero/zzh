package keys

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"zzh/internal/core/config"
	"zzh/internal/core/sshkey"
)

func refuseSecret(t *testing.T) SecretReader {
	return func(string) (string, error) {
		t.Fatalf("no secret must be asked here")
		return "", nil
	}
}

// writeKeyFiles writes a private key and, optionally, its .pub file, as
// they would be handed over.
func writeKeyFiles(t *testing.T, pair sshkey.Pair, withPublic bool) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(path, pair.PrivatePEM, 0o600); err != nil {
		t.Fatalf("cannot write key: %v", err)
	}
	if withPublic {
		if err := os.WriteFile(path+".pub", []byte(pair.PublicLine+"\n"), 0o600); err != nil {
			t.Fatalf("cannot write public key: %v", err)
		}
	}
	return path
}

func importArgs(t *testing.T, store Store, in string, askSecret SecretReader, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Run(append([]string{"import"}, args...), store, config.Defaults().Keys, strings.NewReader(in), askSecret, refuseConfirm(t), &out)
	return out.String(), err
}

func TestImportFileTakesCommentFromPub(t *testing.T) {
	store, _ := newStore(t)
	pair, err := sshkey.Generate(sshkey.TypeEd25519, 0, "max@old-laptop")
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	path := writeKeyFiles(t, pair, true)

	out, err := importArgs(t, store, "", refuseSecret(t), "dd-key", path)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if !strings.Contains(out, "max@old-laptop") || !strings.Contains(out, "can be deleted") {
		t.Fatalf("unexpected output:\n%s", out)
	}

	v, err := store.Open()
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}
	key, err := v.FindKey("dd-key")
	if err != nil || key.Fingerprint != pair.Fingerprint {
		t.Fatalf("key not stored correctly: %+v (%v)", key, err)
	}

	// Importing the same key again, under any name, is refused.
	if _, err := importArgs(t, store, "", refuseSecret(t), "again", path); err == nil || !strings.Contains(err.Error(), `already stored as "dd-key"`) {
		t.Fatalf("duplicate not detected: %v", err)
	}

	// The original files are never deleted by zzh.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the source file must be left in place")
	}
}

func TestImportExplicitCommentAndNoPub(t *testing.T) {
	store, _ := newStore(t)
	pair, err := sshkey.Generate(sshkey.TypeEd25519, 0, "ignored")
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	withPub := writeKeyFiles(t, pair, true)
	out, err := importArgs(t, store, "", refuseSecret(t), "a", withPub, "--comment", "chosen")
	if err != nil || !strings.Contains(out, " chosen\n") {
		t.Fatalf("explicit comment not used: %v\n%s", err, out)
	}

	other, err := sshkey.Generate(sshkey.TypeEd25519, 0, "x")
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	withoutPub := writeKeyFiles(t, other, false)
	out, err = importArgs(t, store, "", refuseSecret(t), "b", withoutPub)
	if err != nil || !strings.Contains(out, " b\n") {
		t.Fatalf("the key name must be the default comment: %v\n%s", err, out)
	}
}

func TestImportPastedEncryptedKey(t *testing.T) {
	store, _ := newStore(t)

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("cannot generate key: %v", err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(privateKey, "", []byte("open sesame"))
	if err != nil {
		t.Fatalf("cannot encrypt key: %v", err)
	}

	askSecret := func(message string) (string, error) {
		if !strings.Contains(message, "Passphrase") {
			t.Fatalf("unexpected prompt %q", message)
		}
		return "open sesame", nil
	}

	if _, err := importArgs(t, store, string(pem.EncodeToMemory(block)), askSecret, "pasted", "-"); err != nil {
		t.Fatalf("import from stdin failed: %v", err)
	}

	v, err := store.Open()
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}
	privatePEM, err := v.PrivateKey("pasted")
	if err != nil {
		t.Fatalf("key not stored: %v", err)
	}
	if _, err := ssh.ParsePrivateKey(privatePEM); err != nil {
		t.Fatalf("stored key must not need a passphrase: %v", err)
	}
}

func TestImportErrorsDoNotWrite(t *testing.T) {
	store, dbPath := newStore(t)
	before, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("cannot read database: %v", err)
	}

	notAKey := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(notAKey, []byte("hello"), 0o600); err != nil {
		t.Fatalf("cannot write file: %v", err)
	}

	for _, args := range [][]string{
		{"k"},
		{"bad name", notAKey},
		{"k", filepath.Join(t.TempDir(), "missing")},
		{"k", t.TempDir()},
		{"k", notAKey},
	} {
		if _, err := importArgs(t, store, "", refuseSecret(t), args...); err == nil {
			t.Errorf("%q: expected an error", args)
		}
	}

	after, _ := os.ReadFile(dbPath)
	if !bytes.Equal(before, after) {
		t.Fatalf("failed imports must not modify the database")
	}
}
