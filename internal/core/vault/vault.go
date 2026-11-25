// Package vault gives access to the zzh data stored in a KeePass (.kdbx)
// database: hosts, keys and, in later steps, host keys.
//
// It knows the zzh layout inside the database (see docs/project/README.md):
//
//	<database root>/<root group>/Hosts/...   host entries, any sub-folders
//	<database root>/<root group>/Keys/...    key entries, any sub-folders
//
// The package deliberately exposes zzh concepts (Host, Key) instead of raw
// KeePass entries, so that the rest of the code never deals with the
// details of the KDBX format.
package vault

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"

	"github.com/tobischo/gokeepasslib/v3"
)

const (
	hostsGroupName = "Hosts"
	keysGroupName  = "Keys"
)

// ErrWrongPassword is returned when the database cannot be decrypted.
// The KDBX format cannot distinguish a wrong password from a corrupted
// header, so the message mentions both.
var ErrWrongPassword = errors.New("cannot decrypt the database: wrong password or corrupted file")

// ErrNotFound is wrapped by the lookups (FindHost, FindKey...) when no
// entry has the requested name, so that callers can tell "absent" apart
// from "ambiguous" or "invalid" and react, for example by trying another
// kind of lookup.
var ErrNotFound = errors.New("not found")

// ErrUnsupportedKDF is returned for databases protected with Argon2id,
// which the KDBX library used by zzh cannot derive (see Q-006).
var ErrUnsupportedKDF = errors.New("the database uses the Argon2id key derivation, which zzh cannot read yet: " +
	"in KeePassXC open Database > Database Security > Encryption Settings and switch to Argon2d")

// argon2idUUID identifies the Argon2id KDF in the KDBX 4 header
// (9e298b19-56db-4773-b23d-fc3ec6f0a1e6, as defined by KeePass).
var argon2idUUID = []byte{
	0x9E, 0x29, 0x8B, 0x19,
	0x56, 0xDB, 0x47, 0x73,
	0xB2, 0x3D, 0xFC, 0x3E,
	0xC6, 0xF0, 0xA1, 0xE6,
}

func usesArgon2id(db *gokeepasslib.Database) bool {
	if db.Header == nil || db.Header.FileHeaders == nil || db.Header.FileHeaders.KdfParameters == nil {
		return false
	}
	return bytes.Equal(db.Header.FileHeaders.KdfParameters.UUID, argon2idUUID)
}

// Vault is an unlocked database together with the zzh root group to use.
//
// It also remembers the exact bytes it was decoded from: Save compares them
// with the file on disk to detect changes made meanwhile by KeePassXC, and
// uses them for the backup copy.
type Vault struct {
	path      string
	rootGroup string
	db        *gokeepasslib.Database

	original     []byte
	originalHash [sha256.Size]byte

	// readOnly marks a vault opened from a snapshot (see snapshot.go): its
	// content mirrors the file but it must never be saved over it.
	readOnly bool
}

// Open decrypts the database at path with the given master password.
//
// rootGroup is the name of the group, directly under the database root,
// that contains the zzh "Hosts" and "Keys" groups.
func Open(path, password, rootGroup string) (*Vault, error) {
	// The whole file is read first and decoded from memory, so that the
	// bytes used for change detection are exactly the decoded ones.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open database: %w", err)
	}
	return OpenBytes(data, path, password, rootGroup)
}

// OpenBytes decrypts a database already read into memory. path is only
// recorded (for Save and messages); data must be its content. The daemon
// uses it to decrypt exactly the bytes whose fingerprint it computed.
func OpenBytes(data []byte, path, password, rootGroup string) (*Vault, error) {
	db := gokeepasslib.NewDatabase()
	db.Credentials = gokeepasslib.NewPasswordCredentials(password)

	decodeErr := gokeepasslib.NewDecoder(bytes.NewReader(data)).Decode(db)

	// The KDBX library only implements Argon2d: with an Argon2id database it
	// silently derives the wrong key and decoding fails as if the password
	// were wrong. The header is parsed before the key derivation, so the
	// real cause can be detected and reported, whatever Decode returned.
	if usesArgon2id(db) {
		return nil, ErrUnsupportedKDF
	}

	if decodeErr != nil {
		return nil, fmt.Errorf("%w (%v)", ErrWrongPassword, decodeErr)
	}

	// Inside the decrypted XML, password fields are additionally encrypted
	// with a stream cipher; they must be unlocked to read their value. They
	// stay unlocked in memory and are locked again only while saving.
	if err := db.UnlockProtectedEntries(); err != nil {
		return nil, fmt.Errorf("cannot unlock protected fields: %w", err)
	}

	return &Vault{
		path:         path,
		rootGroup:    rootGroup,
		db:           db,
		original:     data,
		originalHash: sha256.Sum256(data),
	}, nil
}

// zzhGroup locates <root group>/<name> ("Hosts" or "Keys").
//
// A KDBX file always has a single top-level group (KeePassXC calls it
// "Root" or names it after the database); the zzh root group is searched
// among its children.
func (v *Vault) zzhGroup(name string) (*gokeepasslib.Group, error) {
	if v.db.Content == nil || v.db.Content.Root == nil || len(v.db.Content.Root.Groups) == 0 {
		return nil, errors.New("the database has no groups")
	}

	databaseRoot := &v.db.Content.Root.Groups[0]

	zzhRoot := childGroup(databaseRoot, v.rootGroup)
	if zzhRoot == nil {
		return nil, fmt.Errorf("group %q not found at the top of the database (create it in KeePassXC or change \"root_group\")", v.rootGroup)
	}

	group := childGroup(zzhRoot, name)
	if group == nil {
		return nil, fmt.Errorf("group %q not found inside %q", name, v.rootGroup)
	}

	return group, nil
}

// childGroup returns the direct child group with the given name, or nil.
func childGroup(parent *gokeepasslib.Group, name string) *gokeepasslib.Group {
	for index := range parent.Groups {
		if parent.Groups[index].Name == name {
			return &parent.Groups[index]
		}
	}
	return nil
}

// locatedEntry is a KeePass entry together with its folder, the
// slash-separated path relative to the Hosts or Keys group.
//
// The entry pointer refers into the group slices: it is only valid until
// entries or groups are added to the tree, so it must not be kept across
// modifications.
type locatedEntry struct {
	entry  *gokeepasslib.Entry
	folder string
}

// collectEntries walks a group tree depth-first and returns every entry.
func collectEntries(group *gokeepasslib.Group, folder string, result *[]locatedEntry) {
	for index := range group.Entries {
		*result = append(*result, locatedEntry{entry: &group.Entries[index], folder: folder})
	}

	for index := range group.Groups {
		child := &group.Groups[index]

		childFolder := child.Name
		if folder != "" {
			childFolder = folder + "/" + child.Name
		}

		collectEntries(child, childFolder, result)
	}
}

// entriesIn returns all entries under <root group>/<name>.
func (v *Vault) entriesIn(name string) ([]locatedEntry, error) {
	group, err := v.zzhGroup(name)
	if err != nil {
		return nil, err
	}

	var result []locatedEntry
	collectEntries(group, "", &result)
	return result, nil
}

// findUnique returns the single entry with the given title among entries.
// kind ("host" or "key") is only used in error messages.
//
// The title is the only thing the user types, so it must identify a single
// entry: duplicates are reported with their folders so that the user knows
// which entries to rename in KeePassXC.
func findUnique(entries []locatedEntry, title, kind, where string) (locatedEntry, error) {
	var matches []locatedEntry
	for _, located := range entries {
		if entryTitle(located.entry) == title {
			matches = append(matches, located)
		}
	}

	switch len(matches) {
	case 0:
		return locatedEntry{}, fmt.Errorf("%s %q %w in %s", kind, title, ErrNotFound, where)
	case 1:
		return matches[0], nil
	default:
		paths := make([]string, 0, len(matches))
		for _, match := range matches {
			paths = append(paths, joinPath(match.folder, title))
		}
		return locatedEntry{}, fmt.Errorf("%s name %q is used by more than one entry: %v", kind, title, paths)
	}
}

func joinPath(folder, name string) string {
	if folder == "" {
		return name
	}
	return folder + "/" + name
}
