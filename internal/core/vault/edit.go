package vault

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/tobischo/gokeepasslib/v3"
	"github.com/tobischo/gokeepasslib/v3/wrappers"
)

// The edit operations below only change the database in memory; nothing is
// written until Save is called. They validate their input fully before
// touching the tree, so that a failed operation leaves the vault unchanged.

// NewKey describes a key pair to store.
type NewKey struct {
	Name        string
	Folder      string
	Type        string
	PublicKey   string
	Fingerprint string
	PrivatePEM  []byte
}

// NewHost describes a host to store.
type NewHost struct {
	Name     string
	Folder   string
	Address  string
	Port     int
	User     string
	Password string
	KeyName  string
	Tags     []string
	Options  []string
	Aliases  []string
}

// ValidateEntryName checks a host or key name chosen by the user.
//
// Names are typed on the command line, so they must survive the shell and
// the zzh argument parser: no spaces, no leading "-" (it would be read as an
// option) and no "@" (it separates "user@host").
func ValidateEntryName(name string) error {
	switch {
	case name == "":
		return errors.New("the name cannot be empty")
	case strings.HasPrefix(name, "-"):
		return fmt.Errorf("invalid name %q: it cannot start with '-'", name)
	case strings.Contains(name, "@"):
		return fmt.Errorf("invalid name %q: it cannot contain '@'", name)
	case strings.IndexFunc(name, unicode.IsSpace) >= 0:
		return fmt.Errorf("invalid name %q: it cannot contain spaces", name)
	}
	return nil
}

// AddKey stores a new key pair as an entry with the private key attached.
func (v *Vault) AddKey(key NewKey) error {
	if err := ValidateEntryName(key.Name); err != nil {
		return err
	}
	folderParts, err := splitFolder(key.Folder)
	if err != nil {
		return err
	}

	existing, err := v.entriesIn(keysGroupName)
	if err != nil {
		return err
	}
	if err := ensureUnusedTitle(existing, key.Name, "key"); err != nil {
		return err
	}

	keysGroup, err := v.zzhGroup(keysGroupName)
	if err != nil {
		return err
	}

	entry := gokeepasslib.NewEntry()
	setValue(&entry, fieldTitle, key.Name, false)
	setValue(&entry, fieldKeyType, key.Type, false)
	setValue(&entry, fieldPublicKey, key.PublicKey, false)
	setValue(&entry, fieldFingerprint, key.Fingerprint, false)

	// The private key is stored as an attachment, the way KeePassXC users
	// usually keep SSH keys, and flagged for in-memory protection so that
	// KeePassXC treats it as sensitive data.
	binary := v.db.AddBinary(key.PrivatePEM)
	binary.MemoryProtection = 1
	entry.Binaries = append(entry.Binaries, binary.CreateReference("id_"+key.Type))

	target := ensureFolder(keysGroup, folderParts)
	target.Entries = append(target.Entries, entry)
	return nil
}

// AddHost stores a new host entry.
func (v *Vault) AddHost(host NewHost) error {
	if err := ValidateEntryName(host.Name); err != nil {
		return err
	}
	folderParts, err := splitFolder(host.Folder)
	if err != nil {
		return err
	}

	// The address is validated with the same parser used when reading, so
	// that a host accepted here is always usable afterwards.
	url := formatURL(host.Address, host.Port)
	if _, _, _, err := ParseAddress(url); err != nil {
		return err
	}
	if _, err := parseOptions(strings.Join(host.Options, "\n")); err != nil {
		return err
	}

	existing, err := v.entriesIn(hostsGroupName)
	if err != nil {
		return err
	}
	words := append([]string{host.Name}, host.Aliases...)
	if err := checkNewHostWords(existing, words, nil); err != nil {
		return err
	}

	keyID := ""
	if host.KeyName != "" {
		key, err := v.FindKey(host.KeyName)
		if err != nil {
			return err
		}
		keyID = key.ID
	}

	hostsGroup, err := v.zzhGroup(hostsGroupName)
	if err != nil {
		return err
	}

	entry := gokeepasslib.NewEntry()
	setValue(&entry, fieldTitle, host.Name, false)
	setValue(&entry, fieldUserName, host.User, false)
	setValue(&entry, fieldPassword, host.Password, true)
	setValue(&entry, fieldURL, url, false)
	if keyID != "" {
		setValue(&entry, fieldKey, keyID, false)
	}
	if len(host.Options) > 0 {
		setValue(&entry, fieldOptions, strings.Join(host.Options, "\n"), false)
	}
	if len(host.Aliases) > 0 {
		setValue(&entry, fieldAliases, strings.Join(host.Aliases, "\n"), false)
	}
	entry.Tags = strings.Join(host.Tags, ";")

	target := ensureFolder(hostsGroup, folderParts)
	target.Entries = append(target.Entries, entry)
	return nil
}

// SetHostKey associates a key with a host; an empty keyName removes the
// association. The previous state of the entry is kept in its history, as
// KeePassXC does, so the change can be reverted from the KeePassXC UI.
func (v *Vault) SetHostKey(hostName, keyName string) error {
	keyID := ""
	if keyName != "" {
		key, err := v.FindKey(keyName)
		if err != nil {
			return err
		}
		keyID = key.ID
	}

	located, err := v.findHostEntry(hostName)
	if err != nil {
		return err
	}

	if strings.TrimSpace(located.entry.GetContent(fieldKey)) == keyID {
		return nil
	}

	saveHistory(located.entry)
	if keyID == "" {
		removeValue(located.entry, fieldKey)
	} else {
		setValue(located.entry, fieldKey, keyID, false)
	}
	touch(located.entry)
	return nil
}

// AddHostAliases adds aliases to an existing host (D-031). Aliases the host
// already has are ignored, so the command can be repeated safely.
func (v *Vault) AddHostAliases(hostName string, aliases []string) error {
	located, err := v.findHostEntry(hostName)
	if err != nil {
		return err
	}

	current := parseAliases(located.entry.GetContent(fieldAliases))
	var added []string
	for _, alias := range aliases {
		if !slices.Contains(current, alias) && !slices.Contains(added, alias) && entryTitle(located.entry) != alias {
			added = append(added, alias)
		}
	}
	if len(added) == 0 {
		return nil
	}

	existing, err := v.entriesIn(hostsGroupName)
	if err != nil {
		return err
	}
	if err := checkNewHostWords(existing, added, located.entry); err != nil {
		return err
	}

	saveHistory(located.entry)
	setValue(located.entry, fieldAliases, strings.Join(append(current, added...), "\n"), false)
	touch(located.entry)
	return nil
}

// RemoveHostAliases removes aliases from whichever host has them. The host
// does not need to be named: an alias identifies a single host.
func (v *Vault) RemoveHostAliases(aliases []string) error {
	existing, err := v.entriesIn(hostsGroupName)
	if err != nil {
		return err
	}

	// Everything is checked before changing anything, so that a typo in the
	// last alias does not leave the first ones half removed.
	owners := map[*gokeepasslib.Entry][]string{}
	for _, alias := range aliases {
		var owner *gokeepasslib.Entry
		for _, located := range existing {
			if slices.Contains(parseAliases(located.entry.GetContent(fieldAliases)), alias) {
				owner = located.entry
				break
			}
			if entryTitle(located.entry) == alias {
				return fmt.Errorf("%q is the name of a host, not an alias", alias)
			}
		}
		if owner == nil {
			return fmt.Errorf("alias %q %w", alias, ErrNotFound)
		}
		owners[owner] = append(owners[owner], alias)
	}

	for entry, removed := range owners {
		remaining := slices.DeleteFunc(parseAliases(entry.GetContent(fieldAliases)), func(alias string) bool {
			return slices.Contains(removed, alias)
		})

		saveHistory(entry)
		if len(remaining) == 0 {
			removeValue(entry, fieldAliases)
		} else {
			setValue(entry, fieldAliases, strings.Join(remaining, "\n"), false)
		}
		touch(entry)
	}
	return nil
}

// checkNewHostWords validates words (a new name and/or aliases) and makes
// sure none of them already reaches another host, by name or by alias:
// "zzh <word>" must always identify a single host. owner, when not nil, is
// the entry receiving the words; its own words do not count as conflicts.
func checkNewHostWords(existing []locatedEntry, words []string, owner *gokeepasslib.Entry) error {
	seen := map[string]bool{}
	for _, word := range words {
		if err := ValidateEntryName(word); err != nil {
			return err
		}
		if seen[word] {
			return fmt.Errorf("%q is given more than once", word)
		}
		seen[word] = true
	}

	for _, located := range existing {
		if located.entry == owner {
			continue
		}
		taken := hostWords(located.entry)
		for _, word := range words {
			if taken[word] {
				return fmt.Errorf("%q is already used by the host %s", word, joinPath(located.folder, entryTitle(located.entry)))
			}
		}
	}
	return nil
}

// ensureUnusedTitle refuses a name already used by another entry of the
// same kind, anywhere in the tree: names must be unique to be usable.
func ensureUnusedTitle(entries []locatedEntry, title, kind string) error {
	for _, located := range entries {
		if entryTitle(located.entry) == title {
			return fmt.Errorf("a %s named %q already exists (%s)", kind, title, joinPath(located.folder, title))
		}
	}
	return nil
}

// splitFolder turns "prod/eu" into its parts, rejecting empty segments
// ("prod//eu", "/prod") which would create unnamed groups.
func splitFolder(folder string) ([]string, error) {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return nil, nil
	}

	parts := strings.Split(folder, "/")
	for index, part := range parts {
		parts[index] = strings.TrimSpace(part)
		if parts[index] == "" {
			return nil, fmt.Errorf("invalid folder %q: empty path segment", folder)
		}
	}
	return parts, nil
}

// ensureFolder returns the group at the given path below parent, creating
// the missing groups.
func ensureFolder(parent *gokeepasslib.Group, parts []string) *gokeepasslib.Group {
	current := parent
	for _, part := range parts {
		child := childGroup(current, part)
		if child == nil {
			group := gokeepasslib.NewGroup()
			group.Name = part
			current.Groups = append(current.Groups, group)
			child = &current.Groups[len(current.Groups)-1]
		}
		current = child
	}
	return current
}

// setValue sets a field, adding it if missing. Protected fields (passwords)
// are encrypted with the inner stream cipher when the database is saved.
func setValue(entry *gokeepasslib.Entry, key, value string, protected bool) {
	if existing := entry.Get(key); existing != nil {
		existing.Value.Content = value
		return
	}

	data := gokeepasslib.ValueData{Key: key, Value: gokeepasslib.V{Content: value}}
	if protected {
		data.Value.Protected = wrappers.NewBoolWrapper(true)
	}
	entry.Values = append(entry.Values, data)
}

func removeValue(entry *gokeepasslib.Entry, key string) {
	if index := entry.GetIndex(key); index >= 0 {
		entry.Values = append(entry.Values[:index], entry.Values[index+1:]...)
	}
}

// saveHistory appends a copy of the entry, as it is now, to its history.
// History items keep the UUID of the entry they belong to and carry no
// history of their own, as in KeePass.
func saveHistory(entry *gokeepasslib.Entry) {
	snapshot := entry.Clone()
	snapshot.UUID = entry.UUID
	snapshot.Histories = nil

	if len(entry.Histories) == 0 {
		entry.Histories = []gokeepasslib.History{{}}
	}
	entry.Histories[0].Entries = append(entry.Histories[0].Entries, snapshot)
}

// touch updates the modification time, which KeePassXC uses for display
// and for merging databases.
func touch(entry *gokeepasslib.Entry) {
	now := wrappers.Now()
	entry.Times.LastModificationTime = &now
}
