package vault

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/tobischo/gokeepasslib/v3"
)

// Snapshots let the zzh daemon hand an already decrypted database to a
// command in a few milliseconds (daemon.keep_open, D-034).
//
// Opening the real file costs about a second, on purpose: the key
// derivation (Argon2) is what makes guessing the master password slow. The
// daemon pays it once, then re-encodes the content in memory with a random
// one-time key and an almost free key derivation, and sends those bytes and
// the key to the command over the private local endpoint. The snapshot never
// touches the disk, and the weak key derivation does not weaken anything:
// the random key is not guessable and travels together with the data.
//
// A vault opened from a snapshot is read-only. Writes always open the real
// file, so there is a single, well-tested write path whether the daemon is
// running or not.

// ErrReadOnly is returned by Save on a vault opened from a snapshot.
var ErrReadOnly = errors.New("this copy of the database is read-only")

// ErrSnapshotUnsupported is returned for KDBX 3 databases: zzh only builds
// snapshots (and only writes) in the KDBX 4 format.
var ErrSnapshotUnsupported = errors.New("snapshots are only supported for KDBX 4 databases")

// Key derivation of snapshots: the minimum Argon2 accepts. See above for
// why this is safe.
const (
	snapshotIterations  = 1
	snapshotMemoryBytes = 64 * 1024
	snapshotParallelism = 1
)

// ExportSnapshot re-encodes the database with a random one-time key and a
// cheap key derivation, and returns the bytes and the key.
//
// The vault is consumed: its header and credentials are replaced, so it
// must not be used afterwards (the daemon discards it right away).
func (v *Vault) ExportSnapshot() ([]byte, string, error) {
	if !v.db.Header.IsKdbx4() {
		return nil, "", ErrSnapshotUnsupported
	}

	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, "", fmt.Errorf("cannot generate the snapshot key: %w", err)
	}
	key := hex.EncodeToString(keyBytes)

	params := v.db.Header.FileHeaders.KdfParameters
	if bytes.Equal(params.UUID, gokeepasslib.KdfArgon2) {
		params.Iterations = snapshotIterations
		params.Memory = snapshotMemoryBytes
		params.Parallelism = snapshotParallelism
	} else {
		// AES-KDF, the other key derivation of KDBX 4: one round.
		params.Rounds = 1
	}

	v.db.Credentials = gokeepasslib.NewPasswordCredentials(key)
	if err := regenerateRandomValues(v.db); err != nil {
		return nil, "", err
	}

	data, err := v.encode()
	if err != nil {
		return nil, "", err
	}
	return data, key, nil
}

// OpenSnapshot opens a snapshot produced by ExportSnapshot. path is the real
// database file, recorded for messages; the returned vault is read-only.
func OpenSnapshot(data []byte, key, path, rootGroup string) (*Vault, error) {
	v, err := OpenBytes(data, path, key, rootGroup)
	if err != nil {
		return nil, fmt.Errorf("cannot open the copy of the database received from the daemon: %w", err)
	}
	v.readOnly = true
	return v, nil
}
