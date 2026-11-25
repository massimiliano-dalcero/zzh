package vault

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/tobischo/gokeepasslib/v3"
)

// Custom attributes of key entries.
const (
	fieldPublicKey   = "zzh.public"
	fieldFingerprint = "zzh.fingerprint"
	fieldKeyType     = "zzh.type"
)

// Key is an SSH key pair stored in KeePass. The private key itself is not
// part of this struct: it is read only when needed, with PrivateKey.
type Key struct {
	// ID is the entry UUID in upper-case hex, the form KeePass uses in field
	// references ({REF:...}); host entries point to a key with this value.
	ID string

	// Name is the entry title, used on the command line.
	Name string

	// Folder is the path relative to the Keys group.
	Folder string

	Type        string
	PublicKey   string
	Fingerprint string

	// Attachment is the name of the private key attachment.
	Attachment string
}

// Keys returns all key entries under <root group>/Keys, sorted by name.
func (v *Vault) Keys() ([]Key, error) {
	entries, err := v.entriesIn(keysGroupName)
	if err != nil {
		return nil, err
	}

	keys := make([]Key, 0, len(entries))
	for _, located := range entries {
		keys = append(keys, keyFromEntry(located.entry, located.folder))
	}

	sort.Slice(keys, func(i, j int) bool { return keys[i].Name < keys[j].Name })
	return keys, nil
}

// FindKey returns the key with the given name.
func (v *Vault) FindKey(name string) (Key, error) {
	located, err := v.findKeyEntry(name)
	if err != nil {
		return Key{}, err
	}
	return keyFromEntry(located.entry, located.folder), nil
}

// PrivateKey returns the private key of the named key in OpenSSH PEM form.
func (v *Vault) PrivateKey(name string) ([]byte, error) {
	located, err := v.findKeyEntry(name)
	if err != nil {
		return nil, err
	}

	key := keyFromEntry(located.entry, located.folder)
	for _, reference := range located.entry.Binaries {
		if reference.Name != key.Attachment {
			continue
		}

		binary := reference.Find(v.db)
		if binary == nil {
			return nil, fmt.Errorf("key %q: attachment %q is referenced but missing", name, key.Attachment)
		}
		content, err := binary.GetContentBytes()
		if err != nil {
			return nil, fmt.Errorf("key %q: cannot read attachment %q: %w", name, key.Attachment, err)
		}
		return content, nil
	}

	return nil, fmt.Errorf("key %q has no attachment named %q", name, key.Attachment)
}

func (v *Vault) findKeyEntry(name string) (locatedEntry, error) {
	entries, err := v.entriesIn(keysGroupName)
	if err != nil {
		return locatedEntry{}, err
	}
	return findUnique(entries, name, "key", v.rootGroup+"/"+keysGroupName)
}

// keyNamesByID maps key UUIDs to key names, to show which key each host
// uses. A database without a Keys group simply has no keys: that is not an
// error for commands that only deal with hosts.
func (v *Vault) keyNamesByID() (map[string]string, error) {
	names := map[string]string{}

	if _, err := v.zzhGroup(keysGroupName); err != nil {
		return names, nil
	}

	keys, err := v.Keys()
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		names[key.ID] = key.Name
	}
	return names, nil
}

func keyFromEntry(entry *gokeepasslib.Entry, folder string) Key {
	keyType := strings.TrimSpace(entry.GetContent(fieldKeyType))

	return Key{
		ID:          uuidString(entry.UUID),
		Name:        entryTitle(entry),
		Folder:      folder,
		Type:        keyType,
		PublicKey:   strings.TrimSpace(entry.GetContent(fieldPublicKey)),
		Fingerprint: strings.TrimSpace(entry.GetContent(fieldFingerprint)),
		Attachment:  "id_" + keyType,
	}
}

// uuidString formats an entry UUID the way KeePass does in references.
func uuidString(id gokeepasslib.UUID) string {
	return strings.ToUpper(hex.EncodeToString(id[:]))
}
