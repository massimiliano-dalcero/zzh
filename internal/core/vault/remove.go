package vault

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tobischo/gokeepasslib/v3"
	"github.com/tobischo/gokeepasslib/v3/wrappers"
)

// Removal follows the KeePass conventions, so that KeePassXC shows the
// result the way the user expects (D-035):
//
//   - when the database has the recycle bin enabled, the entry is moved
//     there, with its history, and can be restored from KeePassXC;
//   - otherwise (or when it is already in the bin) it is deleted and
//     recorded among the "deleted objects", which KeePass uses when merging
//     two copies of a database, so that the entry does not come back from an
//     older copy.

// recycleBinName is the group KeePassXC creates for the bin; the name is
// only a label, the group is identified by the UUID stored in the metadata.
const recycleBinName = "Recycle Bin"

// recycleBinIcon is the KeePass standard icon of the bin.
const recycleBinIcon = 43

// RemoveHost removes the host reached by name (name or alias). It reports
// whether the entry went to the recycle bin.
func (v *Vault) RemoveHost(name string) (recycled bool, err error) {
	located, err := v.findHostEntry(name)
	if err != nil {
		return false, err
	}
	return v.removeEntry(located.entry.UUID)
}

// RemoveKey removes a key entry. A key still used by hosts is refused
// unless force is true: then the association is removed from those hosts
// (keeping their previous version in the history), so that no host points
// to a key that no longer exists. The names of those hosts are returned.
func (v *Vault) RemoveKey(name string, force bool) (recycled bool, detached []string, err error) {
	located, err := v.findKeyEntry(name)
	if err != nil {
		return false, nil, err
	}
	keyID := uuidString(located.entry.UUID)
	keyUUID := located.entry.UUID

	hostEntries, err := v.entriesIn(hostsGroupName)
	if err != nil {
		return false, nil, err
	}

	var users []*gokeepasslib.Entry
	for _, host := range hostEntries {
		if strings.TrimSpace(host.entry.GetContent(fieldKey)) == keyID {
			users = append(users, host.entry)
			detached = append(detached, entryTitle(host.entry))
		}
	}

	if len(users) > 0 && !force {
		return false, nil, fmt.Errorf("key %q is used by %s: remove the association first (zzh key use <host> --none) or use --force",
			name, strings.Join(detached, ", "))
	}

	for _, entry := range users {
		saveHistory(entry)
		removeValue(entry, fieldKey)
		touch(entry)
	}

	recycled, err = v.removeEntry(keyUUID)
	return recycled, detached, err
}

// removeEntry moves the entry to the recycle bin, or deletes it.
func (v *Vault) removeEntry(id gokeepasslib.UUID) (bool, error) {
	root := v.db.Content.Root

	// The bin is obtained first: creating it adds a group at the top of the
	// tree, which may move the top-level groups in memory. Searching the
	// parent afterwards guarantees that both pointers are valid; from here
	// on only Entries slices change, never Groups slices.
	bin := v.recycleBin()

	parent, index := findEntryParent(root.Groups, id)
	if parent == nil {
		return false, errors.New("entry not found in the database")
	}

	if bin != nil && bin != parent {
		moved := parent.Entries[index]
		now := wrappers.Now()
		moved.Times.LocationChanged = &now
		parent.Entries = append(parent.Entries[:index], parent.Entries[index+1:]...)
		bin.Entries = append(bin.Entries, moved)
		return true, nil
	}

	parent.Entries = append(parent.Entries[:index], parent.Entries[index+1:]...)
	now := wrappers.Now()
	root.DeletedObjects = append(root.DeletedObjects, gokeepasslib.DeletedObjectData{UUID: id, DeletionTime: &now})
	return false, nil
}

// recycleBin returns the recycle bin group, creating it at the top of the
// database like KeePassXC does, or nil when the bin is disabled.
func (v *Vault) recycleBin() *gokeepasslib.Group {
	meta := v.db.Content.Meta
	if !meta.RecycleBinEnabled.Bool {
		return nil
	}

	if !meta.RecycleBinUUID.IsZero() {
		if bin := findGroup(v.db.Content.Root.Groups, meta.RecycleBinUUID); bin != nil {
			return bin
		}
	}

	databaseRoot := &v.db.Content.Root.Groups[0]
	bin := gokeepasslib.NewGroup()
	bin.Name = recycleBinName
	bin.IconID = recycleBinIcon
	// The bin must not show up in searches or offer auto-type, as in KeePass.
	bin.EnableAutoType = wrappers.NewNullableBoolWrapper(false)
	bin.EnableSearching = wrappers.NewNullableBoolWrapper(false)
	databaseRoot.Groups = append(databaseRoot.Groups, bin)

	now := wrappers.Now()
	meta.RecycleBinUUID = bin.UUID
	meta.RecycleBinChanged = &now
	return &databaseRoot.Groups[len(databaseRoot.Groups)-1]
}

// findEntryParent walks the whole tree and returns the group holding the
// entry and its index, or nil.
func findEntryParent(groups []gokeepasslib.Group, id gokeepasslib.UUID) (*gokeepasslib.Group, int) {
	for groupIndex := range groups {
		group := &groups[groupIndex]
		for entryIndex := range group.Entries {
			if group.Entries[entryIndex].UUID.Compare(id) {
				return group, entryIndex
			}
		}
		if parent, index := findEntryParent(group.Groups, id); parent != nil {
			return parent, index
		}
	}
	return nil, -1
}

// findGroup walks the whole tree looking for a group by UUID.
func findGroup(groups []gokeepasslib.Group, id gokeepasslib.UUID) *gokeepasslib.Group {
	for index := range groups {
		if groups[index].UUID.Compare(id) {
			return &groups[index]
		}
		if found := findGroup(groups[index].Groups, id); found != nil {
			return found
		}
	}
	return nil
}
