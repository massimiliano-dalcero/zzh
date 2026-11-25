package sshkey

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGenerateRoundTrip(t *testing.T) {
	cases := []struct {
		keyType string
		bits    int
		prefix  string
	}{
		{TypeEd25519, 0, "ssh-ed25519 "},
		{TypeECDSA, 0, "ecdsa-sha2-nistp256 "},
		{TypeECDSA, 384, "ecdsa-sha2-nistp384 "},
		{TypeRSA, 2048, "ssh-rsa "},
	}

	for _, tc := range cases {
		t.Run(tc.prefix, func(t *testing.T) {
			pair, err := Generate(tc.keyType, tc.bits, "max@laptop")
			if err != nil {
				t.Fatalf("generate failed: %v", err)
			}

			if !strings.HasPrefix(pair.PublicLine, tc.prefix) || !strings.HasSuffix(pair.PublicLine, " max@laptop") {
				t.Fatalf("unexpected public line %q", pair.PublicLine)
			}
			if !strings.Contains(string(pair.PrivatePEM), "BEGIN OPENSSH PRIVATE KEY") {
				t.Fatalf("private key not in OpenSSH format")
			}

			// The stored private key must parse back and match the public key.
			signer, err := ssh.ParsePrivateKey(pair.PrivatePEM)
			if err != nil {
				t.Fatalf("private key does not parse: %v", err)
			}
			if ssh.FingerprintSHA256(signer.PublicKey()) != pair.Fingerprint {
				t.Fatalf("fingerprint does not match the private key")
			}

			parsed, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(pair.PublicLine))
			if err != nil || comment != "max@laptop" || ssh.FingerprintSHA256(parsed) != pair.Fingerprint {
				t.Fatalf("public line not valid for authorized_keys: %v", err)
			}
		})
	}
}

func TestGenerateRejectsInvalidParameters(t *testing.T) {
	cases := []struct {
		keyType string
		bits    int
	}{
		{"dsa", 0},
		{TypeEd25519, 256},
		{TypeECDSA, 512},
		{TypeRSA, 1024},
		{TypeRSA, 0},
	}

	for _, tc := range cases {
		if _, err := Generate(tc.keyType, tc.bits, ""); err == nil {
			t.Errorf("%s/%d: expected an error", tc.keyType, tc.bits)
		}
	}
}

func noPassphrase(t *testing.T) func() (string, error) {
	return func() (string, error) {
		t.Fatalf("the passphrase must not be asked for an unencrypted key")
		return "", nil
	}
}

func TestImportOpenSSHKeepsKey(t *testing.T) {
	original, err := Generate(TypeEd25519, 0, "old comment")
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	imported, err := Import(original.PrivatePEM, "new comment", noPassphrase(t))
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if imported.Fingerprint != original.Fingerprint || imported.Type != TypeEd25519 {
		t.Fatalf("imported key differs: %+v", imported)
	}
	if !strings.HasSuffix(imported.PublicLine, " new comment") {
		t.Fatalf("comment not applied: %q", imported.PublicLine)
	}
}

func TestImportEncryptedKey(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("cannot generate key: %v", err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(privateKey, "", []byte("open sesame"))
	if err != nil {
		t.Fatalf("cannot encrypt key: %v", err)
	}
	data := pem.EncodeToMemory(block)

	asked := 0
	imported, err := Import(data, "c", func() (string, error) { asked++; return "open sesame", nil })
	if err != nil || asked != 1 {
		t.Fatalf("import failed: %v (asked %d times)", err, asked)
	}
	// The stored key must be usable without any passphrase.
	if _, err := ssh.ParsePrivateKey(imported.PrivatePEM); err != nil {
		t.Fatalf("stored key still needs a passphrase: %v", err)
	}

	_, err = Import(data, "c", func() (string, error) { return "wrong", nil })
	if err == nil || !strings.Contains(err.Error(), "wrong passphrase") {
		t.Fatalf("expected a wrong passphrase error, got %v", err)
	}
}

func TestImportPEMFormats(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("cannot generate EC key: %v", err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(ecKey)
	if err != nil {
		t.Fatalf("cannot encode PKCS#8: %v", err)
	}

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("cannot generate RSA key: %v", err)
	}

	cases := map[string]struct {
		data    []byte
		keyType string
	}{
		"PKCS#8 EC":  {pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}), TypeECDSA},
		"PKCS#1 RSA": {pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaKey)}), TypeRSA},
	}
	for name, tc := range cases {
		imported, err := Import(tc.data, "c", noPassphrase(t))
		if err != nil || imported.Type != tc.keyType {
			t.Errorf("%s: unexpected result %+v (%v)", name, imported, err)
			continue
		}
		if !strings.Contains(string(imported.PrivatePEM), "BEGIN OPENSSH PRIVATE KEY") {
			t.Errorf("%s: not converted to the OpenSSH format", name)
		}
	}
}

func TestImportRejects(t *testing.T) {
	weak, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("cannot generate RSA key: %v", err)
	}
	weakPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(weak)})

	for name, data := range map[string][]byte{
		"weak RSA":   weakPEM,
		"garbage":    []byte("not a key"),
		"public key": []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEFlZbtRw1v7rD9xkcPR2COTujSW9rI3wFP77Fui3yLe x"),
	} {
		if _, err := Import(data, "c", noPassphrase(t)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestPublicKeyComment(t *testing.T) {
	pair, err := Generate(TypeEd25519, 0, "max@laptop")
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	other, err := Generate(TypeEd25519, 0, "someone")
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	comment, ok := PublicKeyComment([]byte(pair.PublicLine+"\n"), pair.Fingerprint)
	if !ok || comment != "max@laptop" {
		t.Fatalf("comment not read: %q %v", comment, ok)
	}
	// A .pub file of a different key must be ignored.
	if _, ok := PublicKeyComment([]byte(other.PublicLine), pair.Fingerprint); ok {
		t.Fatalf("a mismatching public key must be ignored")
	}
}

func TestWithComment(t *testing.T) {
	pair, err := Generate(TypeEd25519, 0, "first")
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	renamed, err := WithComment(pair, "second")
	if err != nil {
		t.Fatalf("with comment failed: %v", err)
	}
	if renamed.Fingerprint != pair.Fingerprint || !strings.HasSuffix(renamed.PublicLine, " second") {
		t.Fatalf("unexpected result: %+v", renamed)
	}
}
