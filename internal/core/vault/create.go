package vault

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tobischo/gokeepasslib/v3"
	"github.com/tobischo/gokeepasslib/v3/wrappers"
	"golang.org/x/crypto/argon2"
)

// Key derivation settings for new databases.
//
// The KDF is what makes guessing the master password expensive: every guess
// costs an attacker as much as one unlock costs the user. The library's own
// defaults (1 MiB, 2 iterations) are far too cheap, so zzh follows the
// KeePassXC approach instead: 64 MiB of memory and as many iterations as
// fit in about one second on the machine creating the database.
//
// Argon2d is used because it is the only Argon2 variant the KDBX library
// can read back (see ErrUnsupportedKDF); it is also the KeePassXC default.
const (
	newDatabaseMemoryBytes = 64 * 1024 * 1024
	newDatabaseParallelism = 2
	targetUnlockTime       = time.Second

	// Floor and ceiling for the calibrated value: very fast machines must
	// not produce an absurd number, and slow ones must not go below the
	// minimum recommended by the Argon2 authors.
	minimumIterations = 2
	maximumIterations = 1000
)

// kdfSettings are the Argon2 parameters written in the database header.
type kdfSettings struct {
	Iterations  uint64
	MemoryBytes uint64
	Parallelism uint32
}

// calibrateKDF measures how long one Argon2 pass takes with the chosen
// memory size and returns the number of passes that fits in target.
//
// The measurement uses Argon2id from x/crypto: the KDBX library computes
// Argon2d with a fork of the same code, and both variants perform the same
// amount of work per pass, so the timing is representative.
func calibrateKDF(target time.Duration) kdfSettings {
	salt := make([]byte, 32)
	derive := func(passes uint32) {
		argon2.IDKey([]byte("calibration"), salt, passes, newDatabaseMemoryBytes/1024, newDatabaseParallelism, 32)
	}

	// The first run also pays for allocating and faulting in 64 MiB, which
	// would make a pass look slower than it is and lead to too few
	// iterations; it is therefore excluded from the measurement, and the
	// time is averaged over a few passes to smooth out scheduling noise.
	const measuredPasses = 4
	derive(1)
	start := time.Now()
	derive(measuredPasses)
	onePass := time.Since(start) / measuredPasses

	iterations := uint64(minimumIterations)
	if onePass > 0 {
		iterations = uint64(target / onePass)
	}
	iterations = max(iterations, minimumIterations)
	iterations = min(iterations, maximumIterations)

	return kdfSettings{
		Iterations:  iterations,
		MemoryBytes: newDatabaseMemoryBytes,
		Parallelism: newDatabaseParallelism,
	}
}

// Create writes a new, empty zzh database at path:
//
//	Root/<rootGroup>/Hosts
//	Root/<rootGroup>/Keys
//
// It never overwrites an existing file. The file is reopened afterwards to
// make sure that it can really be decrypted with the given password before
// the user starts relying on it.
func Create(path, password, databaseName, rootGroup string) error {
	return createWithKDF(path, password, databaseName, rootGroup, calibrateKDF(targetUnlockTime))
}

// createWithKDF is Create with explicit KDF settings, so that tests can use
// cheap parameters instead of a one-second derivation.
func createWithKDF(path, password, databaseName, rootGroup string, kdf kdfSettings) error {
	if password == "" {
		return errors.New("the master password cannot be empty")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("cannot create directory for %s: %w", path, err)
	}

	// O_EXCL makes the existence check and the creation a single atomic
	// operation: an existing database can never be truncated, not even if
	// it appears between a check and the write.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s already exists: zzh never overwrites a database", path)
	}
	if err != nil {
		return fmt.Errorf("cannot create %s: %w", path, err)
	}

	writeErr := writeNewDatabase(file, password, databaseName, rootGroup, kdf)
	closeErr := file.Close()

	if writeErr == nil && closeErr != nil {
		writeErr = fmt.Errorf("cannot finish writing %s: %w", path, closeErr)
	}
	if writeErr != nil {
		// Best effort: the file was created by this function and contains
		// an incomplete database, useless to anyone; leaving it would only
		// block a retry with "already exists".
		_ = os.Remove(path)
		return writeErr
	}

	v, err := Open(path, password, rootGroup)
	if err != nil {
		return fmt.Errorf("the new database cannot be read back: %w", err)
	}
	if _, err := v.Hosts(); err != nil {
		return fmt.Errorf("the new database has an unexpected layout: %w", err)
	}

	return nil
}

// writeNewDatabase builds the database in memory and encodes it to file.
func writeNewDatabase(file *os.File, password, databaseName, rootGroup string, kdf kdfSettings) error {
	// KDBX 4.0 is what KeePassXC writes by default; 4.1 is only needed for
	// features zzh does not use.
	db := gokeepasslib.NewDatabase(gokeepasslib.WithDatabaseKDBXVersion40())
	db.Credentials = gokeepasslib.NewPasswordCredentials(password)

	params := db.Header.FileHeaders.KdfParameters
	params.Iterations = kdf.Iterations
	params.Memory = kdf.MemoryBytes
	params.Parallelism = kdf.Parallelism

	// A fresh random salt: the library generates one too, but setting it
	// here makes the requirement explicit (same password, different key).
	if _, err := rand.Read(params.Salt[:]); err != nil {
		return fmt.Errorf("cannot generate salt: %w", err)
	}

	db.Content.Meta.DatabaseName = databaseName

	// Like KeePassXC, new databases use the recycle bin: removed entries
	// can be restored (D-035). The bin group itself is created on first use.
	db.Content.Meta.RecycleBinEnabled = wrappers.NewBoolWrapper(true)
	db.Content.Root = &gokeepasslib.RootData{
		Groups: []gokeepasslib.Group{
			newGroup("Root",
				newGroup(rootGroup,
					newGroup(hostsGroupName),
					newGroup(keysGroupName),
				),
			),
		},
	}

	if err := db.LockProtectedEntries(); err != nil {
		return fmt.Errorf("cannot protect database fields: %w", err)
	}
	if err := gokeepasslib.NewEncoder(file).Encode(db); err != nil {
		return fmt.Errorf("cannot write database: %w", err)
	}
	return nil
}

func newGroup(name string, children ...gokeepasslib.Group) gokeepasslib.Group {
	group := gokeepasslib.NewGroup()
	group.Name = name
	group.Groups = children
	return group
}
