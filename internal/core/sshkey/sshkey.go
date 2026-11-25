// Package sshkey generates SSH key pairs in the formats OpenSSH uses.
//
// Keys are generated in memory and returned as bytes: the caller stores
// them in KeePass. Nothing is ever written to disk here.
package sshkey

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Supported key types, as typed by the user ("--type ed25519").
const (
	TypeEd25519 = "ed25519"
	TypeECDSA   = "ecdsa"
	TypeRSA     = "rsa"
)

// Pair is a freshly generated key pair.
type Pair struct {
	// Type is one of the Type* constants.
	Type string

	// PrivatePEM is the private key in the OpenSSH format
	// ("-----BEGIN OPENSSH PRIVATE KEY-----"), without passphrase: the
	// KeePass database that stores it is already encrypted.
	PrivatePEM []byte

	// PublicLine is the public key as it appears in authorized_keys,
	// including the comment ("ssh-ed25519 AAAA... comment").
	PublicLine string

	// Fingerprint is the SHA256 fingerprint shown by "ssh-keygen -l".
	Fingerprint string
}

// Generate creates a key pair.
//
// bits is only used for RSA (key size) and ECDSA (curve: 256, 384 or 521);
// zero selects the default (ECDSA P-256; RSA requires an explicit size,
// which the caller takes from the configuration). The comment is embedded in
// both the private and the public key and shows up in authorized_keys,
// which makes it easy to recognize the key on a remote host later.
func Generate(keyType string, bits int, comment string) (Pair, error) {
	privateKey, err := newPrivateKey(keyType, bits)
	if err != nil {
		return Pair{}, err
	}
	return encodePair(keyType, privateKey, comment)
}

// Import turns an existing private key file into a Pair, normalized exactly
// like a generated key: OpenSSH format, no passphrase, the given comment.
//
// Accepted inputs are the formats people usually hand over: OpenSSH
// ("BEGIN OPENSSH PRIVATE KEY") and classic PEM (PKCS#1 RSA, SEC1 EC,
// PKCS#8). When the key is encrypted, askPassphrase is called once to
// decrypt it; the passphrase is not kept, because the KeePass database is
// what protects stored keys (D-021).
func Import(data []byte, comment string, askPassphrase func() (string, error)) (Pair, error) {
	privateKey, err := ssh.ParseRawPrivateKey(data)

	var missing *ssh.PassphraseMissingError
	if errors.As(err, &missing) {
		passphrase, promptErr := askPassphrase()
		if promptErr != nil {
			return Pair{}, promptErr
		}
		privateKey, err = ssh.ParseRawPrivateKeyWithPassphrase(data, []byte(passphrase))
		if errors.Is(err, x509.IncorrectPasswordError) {
			return Pair{}, errors.New("wrong passphrase")
		}
	}
	if err != nil {
		return Pair{}, fmt.Errorf("not a supported private key (OpenSSH or PEM; PuTTY .ppk files must be converted first): %w", err)
	}

	keyType, privateKey, err := normalize(privateKey)
	if err != nil {
		return Pair{}, err
	}
	return encodePair(keyType, privateKey, comment)
}

// normalize identifies the key type and returns the key in the form the
// rest of the code expects. The parser returns Ed25519 keys sometimes as a
// value and sometimes as a pointer, depending on the file format.
func normalize(privateKey crypto.PrivateKey) (string, crypto.PrivateKey, error) {
	switch key := privateKey.(type) {
	case ed25519.PrivateKey:
		return TypeEd25519, key, nil
	case *ed25519.PrivateKey:
		return TypeEd25519, *key, nil
	case *ecdsa.PrivateKey:
		return TypeECDSA, key, nil
	case *rsa.PrivateKey:
		if key.N.BitLen() < 2048 {
			return "", nil, fmt.Errorf("RSA keys shorter than 2048 bits are not accepted (this one has %d)", key.N.BitLen())
		}
		return TypeRSA, key, nil
	default:
		// DSA keys are deprecated and disabled in current OpenSSH releases.
		return "", nil, fmt.Errorf("unsupported key type %T (use ed25519, ecdsa or rsa)", privateKey)
	}
}

// ParsePrivateKey reads an unencrypted private key, as stored in KeePass by
// zzh, and returns it in the form expected by the ssh libraries.
func ParsePrivateKey(privatePEM []byte) (crypto.PrivateKey, error) {
	privateKey, err := ssh.ParseRawPrivateKey(privatePEM)
	if err != nil {
		return nil, fmt.Errorf("cannot read the private key: %w", err)
	}
	_, privateKey, err = normalize(privateKey)
	return privateKey, err
}

// WithComment returns the same key pair with a different comment. The pair's
// private key is already unencrypted, so no passphrase is involved.
func WithComment(pair Pair, comment string) (Pair, error) {
	privateKey, err := ssh.ParseRawPrivateKey(pair.PrivatePEM)
	if err != nil {
		return Pair{}, fmt.Errorf("cannot read the private key: %w", err)
	}
	keyType, privateKey, err := normalize(privateKey)
	if err != nil {
		return Pair{}, err
	}
	return encodePair(keyType, privateKey, comment)
}

// PublicKeyComment returns the comment of an authorized_keys style public
// key line, provided that the line belongs to the given fingerprint. It is
// used to take the comment from the ".pub" file handed over with a private
// key; a .pub of another key is ignored rather than trusted.
func PublicKeyComment(line []byte, fingerprint string) (string, bool) {
	publicKey, comment, _, _, err := ssh.ParseAuthorizedKey(line)
	if err != nil || ssh.FingerprintSHA256(publicKey) != fingerprint {
		return "", false
	}
	return comment, true
}

// encodePair produces the stored representation of a private key.
func encodePair(keyType string, privateKey crypto.PrivateKey, comment string) (Pair, error) {
	block, err := ssh.MarshalPrivateKey(privateKey, comment)
	if err != nil {
		return Pair{}, fmt.Errorf("cannot encode the private key: %w", err)
	}

	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return Pair{}, fmt.Errorf("cannot derive the public key: %w", err)
	}
	publicKey := signer.PublicKey()

	// MarshalAuthorizedKey ends with a newline and has no comment.
	publicLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey)))
	if comment != "" {
		publicLine += " " + comment
	}

	return Pair{
		Type:        keyType,
		PrivatePEM:  pem.EncodeToMemory(block),
		PublicLine:  publicLine,
		Fingerprint: ssh.FingerprintSHA256(publicKey),
	}, nil
}

func newPrivateKey(keyType string, bits int) (crypto.PrivateKey, error) {
	switch keyType {
	case TypeEd25519:
		// Ed25519 has a single size; a value would only be a user mistake.
		if bits != 0 {
			return nil, fmt.Errorf("ed25519 keys have a fixed size, --bits is not allowed")
		}
		_, privateKey, err := ed25519.GenerateKey(rand.Reader)
		return privateKey, err

	case TypeECDSA:
		curve, err := ecdsaCurve(bits)
		if err != nil {
			return nil, err
		}
		return ecdsa.GenerateKey(curve, rand.Reader)

	case TypeRSA:
		// Same limits as the configuration (see config.Validate): below 2048
		// RSA is broken, above 16384 generation takes too long.
		if bits < 2048 || bits > 16384 {
			return nil, fmt.Errorf("RSA key size must be between 2048 and 16384 bits, got %d", bits)
		}
		return rsa.GenerateKey(rand.Reader, bits)

	default:
		return nil, fmt.Errorf("unsupported key type %q (use ed25519, ecdsa or rsa)", keyType)
	}
}

func ecdsaCurve(bits int) (elliptic.Curve, error) {
	switch bits {
	case 0, 256:
		return elliptic.P256(), nil
	case 384:
		return elliptic.P384(), nil
	case 521:
		return elliptic.P521(), nil
	default:
		return nil, fmt.Errorf("ECDSA keys support 256, 384 or 521 bits, got %d", bits)
	}
}

// AttachmentName is the file name used for the private key attachment in
// KeePass, the same name ssh-keygen would use ("id_ed25519"), so that an
// exported attachment is immediately recognizable.
func AttachmentName(keyType string) string {
	return "id_" + keyType
}
