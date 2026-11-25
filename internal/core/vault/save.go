package vault

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tobischo/gokeepasslib/v3"
)

// ErrModifiedExternally is returned by Save when the database file changed
// on disk after it was opened (typically because KeePassXC saved it). The
// caller should open it again, reapply its change and save again.
var ErrModifiedExternally = errors.New("the database file was modified by another program since it was opened")

// BackupSuffix is appended to the database path for the copy of the
// previous version kept by Save.
const BackupSuffix = ".bak"

// Save writes the in-memory database back to its file (decision D-020).
//
// The steps, and why each one is there:
//
//  1. Only KDBX 4 is written: it is what KeePassXC and "zzh init" create,
//     and limiting writes to one format limits the risk of damaging data.
//  2. The random values of the file format are regenerated (master seed,
//     encryption IV, KDF salt, inner stream key). The KDBX library reuses
//     the ones read from the file, which would encrypt the new content with
//     the same key and nonce as the old one: anyone holding both versions
//     could then combine them to learn about the plaintext.
//  3. The new file is encoded in memory and decoded again with the same
//     password, and its structure is compared with the database in memory:
//     a file that cannot be read back is never written.
//  4. The file on disk is compared with the bytes read by Open: if another
//     program saved it in the meantime, ErrModifiedExternally is returned
//     and nothing is written, so its changes are not overwritten.
//  5. With backup enabled, the previous file content is kept next to it
//     (path + BackupSuffix), still encrypted with the same password.
//  6. The new content is written to a temporary file in the same directory
//     and renamed over the original, so that the database file is always
//     either the old or the new version, never a truncated mix.
func (v *Vault) Save(backup bool) error {
	if v.readOnly {
		return ErrReadOnly
	}
	if !v.db.Header.IsKdbx4() {
		return errors.New("zzh only writes KDBX 4 databases: convert this one in KeePassXC (Database > Database Security)")
	}

	expected := summarize(v.db)

	if err := regenerateRandomValues(v.db); err != nil {
		return err
	}

	encoded, err := v.encode()
	if err != nil {
		return err
	}

	if err := verifyEncoded(encoded, v.db.Credentials, expected); err != nil {
		return err
	}

	current, err := os.ReadFile(v.path)
	if err != nil {
		return fmt.Errorf("cannot read %s before saving: %w", v.path, err)
	}
	if sha256.Sum256(current) != v.originalHash {
		return ErrModifiedExternally
	}

	if backup {
		if err := replaceFile(v.path+BackupSuffix, v.original); err != nil {
			return fmt.Errorf("cannot write the backup copy: %w", err)
		}
	}

	if err := replaceFile(v.path, encoded); err != nil {
		return err
	}

	// From now on the file on disk is the new version: further saves from
	// this same vault must compare against it.
	v.original = encoded
	v.originalHash = sha256.Sum256(encoded)
	return nil
}

// encode serializes the database. Protected values must be locked (stream
// encrypted) when the encoder runs; they are unlocked again afterwards so
// that the vault stays usable in memory.
func (v *Vault) encode() ([]byte, error) {
	if err := v.db.LockProtectedEntries(); err != nil {
		return nil, fmt.Errorf("cannot protect database fields: %w", err)
	}

	var buffer bytes.Buffer
	encodeErr := gokeepasslib.NewEncoder(&buffer).Encode(v.db)

	if err := v.db.UnlockProtectedEntries(); err != nil {
		return nil, fmt.Errorf("cannot unlock database fields after saving: %w", err)
	}
	if encodeErr != nil {
		return nil, fmt.Errorf("cannot encode the database: %w", encodeErr)
	}
	return buffer.Bytes(), nil
}

// regenerateRandomValues refreshes every per-save random value of a KDBX 4
// file, keeping the lengths already in use (they depend on the cipher).
func regenerateRandomValues(db *gokeepasslib.Database) error {
	headers := db.Header.FileHeaders

	fresh := func(target []byte) error {
		_, err := rand.Read(target)
		return err
	}

	headers.MasterSeed = make([]byte, len(headers.MasterSeed))
	headers.EncryptionIV = make([]byte, len(headers.EncryptionIV))
	db.Content.InnerHeader.InnerRandomStreamKey = make([]byte, len(db.Content.InnerHeader.InnerRandomStreamKey))

	for _, target := range [][]byte{
		headers.MasterSeed,
		headers.EncryptionIV,
		headers.KdfParameters.Salt[:],
		db.Content.InnerHeader.InnerRandomStreamKey,
	} {
		if err := fresh(target); err != nil {
			return fmt.Errorf("cannot generate random values: %w", err)
		}
	}
	return nil
}

// treeSummary counts the elements of a database, to compare the database in
// memory with the result of decoding the encoded file.
type treeSummary struct {
	groups, entries, historyEntries, attachments int
}

func summarize(db *gokeepasslib.Database) treeSummary {
	var summary treeSummary
	for index := range db.Content.Root.Groups {
		summarizeGroup(&db.Content.Root.Groups[index], &summary)
	}
	return summary
}

func summarizeGroup(group *gokeepasslib.Group, summary *treeSummary) {
	summary.groups++
	for index := range group.Entries {
		entry := &group.Entries[index]
		summary.entries++
		summary.attachments += len(entry.Binaries)
		for _, history := range entry.Histories {
			summary.historyEntries += len(history.Entries)
		}
	}
	for index := range group.Groups {
		summarizeGroup(&group.Groups[index], summary)
	}
}

// verifyEncoded decodes the encoded file with the same credentials and
// checks that it contains the expected tree.
func verifyEncoded(encoded []byte, credentials *gokeepasslib.DBCredentials, expected treeSummary) error {
	check := gokeepasslib.NewDatabase()
	check.Credentials = credentials

	if err := gokeepasslib.NewDecoder(bytes.NewReader(encoded)).Decode(check); err != nil {
		return fmt.Errorf("the new database content cannot be read back, nothing was written: %w", err)
	}
	if err := check.UnlockProtectedEntries(); err != nil {
		return fmt.Errorf("the new database content cannot be unlocked, nothing was written: %w", err)
	}

	if got := summarize(check); got != expected {
		return fmt.Errorf("the new database content differs from the expected one (%+v instead of %+v), nothing was written", got, expected)
	}
	return nil
}

// replaceFile atomically replaces path with content: the data is written
// and flushed to a temporary file in the same directory (a rename is only
// atomic within one file system), which is then renamed over path.
func replaceFile(path string, content []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".zzh-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot create a temporary file next to %s: %w", path, err)
	}
	temporaryPath := temporary.Name()

	// On any failure the temporary file is removed. Ignoring the removal
	// error is safe: at worst an unused ".zzh-*.tmp" file is left behind,
	// and it never replaces the database.
	cleanup := func() { _ = os.Remove(temporaryPath) }

	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		cleanup()
		return fmt.Errorf("cannot write %s: %w", temporaryPath, err)
	}
	// Sync makes sure the data is on disk before the rename makes it the
	// official file: otherwise a power loss could leave an empty database.
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		cleanup()
		return fmt.Errorf("cannot flush %s: %w", temporaryPath, err)
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return fmt.Errorf("cannot close %s: %w", temporaryPath, err)
	}

	// os.Rename replaces an existing file on every supported platform
	// (MoveFileEx with MOVEFILE_REPLACE_EXISTING on Windows).
	if err := os.Rename(temporaryPath, path); err != nil {
		cleanup()
		return fmt.Errorf("cannot replace %s: %w", path, err)
	}
	return nil
}
