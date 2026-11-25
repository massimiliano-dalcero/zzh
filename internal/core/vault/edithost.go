package vault

import (
	"errors"
	"strings"

	"github.com/tobischo/gokeepasslib/v3"
	"github.com/tobischo/gokeepasslib/v3/wrappers"
)

// HostChanges lists the fields of a host to change. A nil field is left as
// it is; a non-nil one replaces the current value, and an empty value clears
// it (no user, no password, default port, no tags...). Key and aliases have
// their own operations (SetHostKey, AddHostAliases, RemoveHostAliases).
type HostChanges struct {
	Name     *string
	Address  *string
	Port     *int
	User     *string
	Password *string
	Folder   *string
	Tags     *[]string
	Options  *[]string
}

// IsEmpty reports whether nothing would change.
func (c HostChanges) IsEmpty() bool {
	return c.Name == nil && c.Address == nil && c.Port == nil && c.User == nil &&
		c.Password == nil && c.Folder == nil && c.Tags == nil && c.Options == nil
}

// EditHost applies changes to the host reached by name (name or alias).
//
// Everything is validated before the entry is touched. The previous state
// of the entry is kept in its KeePass history, so every edit can be undone
// from KeePassXC (D-032).
func (v *Vault) EditHost(name string, changes HostChanges) error {
	if changes.IsEmpty() {
		return errors.New("nothing to change")
	}

	located, err := v.findHostEntry(name)
	if err != nil {
		return err
	}
	current := hostFromEntry(located.entry, located.folder)

	// Validation, without modifying anything.

	if changes.Name != nil && *changes.Name != current.Name {
		existing, err := v.entriesIn(hostsGroupName)
		if err != nil {
			return err
		}
		if err := checkNewHostWords(existing, []string{*changes.Name}, located.entry); err != nil {
			return err
		}
	}

	newURL := ""
	if changes.Address != nil || changes.Port != nil {
		address, port := current.Address, current.Port
		if changes.Address != nil {
			address = *changes.Address
		}
		if changes.Port != nil {
			port = *changes.Port
		}
		newURL = formatURL(address, port)
		if _, _, _, err := ParseAddress(newURL); err != nil {
			return err
		}
	}

	if changes.Options != nil {
		if _, err := parseOptions(strings.Join(*changes.Options, "\n")); err != nil {
			return err
		}
	}

	var folderParts []string
	if changes.Folder != nil {
		folderParts, err = splitFolder(*changes.Folder)
		if err != nil {
			return err
		}
	}

	// Changes.

	saveHistory(located.entry)

	if changes.Name != nil {
		setValue(located.entry, fieldTitle, *changes.Name, false)
	}
	if newURL != "" {
		setValue(located.entry, fieldURL, newURL, false)
	}
	if changes.User != nil {
		setValue(located.entry, fieldUserName, *changes.User, false)
	}
	if changes.Password != nil {
		setValue(located.entry, fieldPassword, *changes.Password, true)
	}
	if changes.Tags != nil {
		located.entry.Tags = strings.Join(*changes.Tags, ";")
	}
	if changes.Options != nil {
		if len(*changes.Options) == 0 {
			removeValue(located.entry, fieldOptions)
		} else {
			setValue(located.entry, fieldOptions, strings.Join(*changes.Options, "\n"), false)
		}
	}
	touch(located.entry)

	// Moving comes last: it copies the entry into another group and removes
	// it from the old one, after which located.entry no longer points to it.
	if changes.Folder != nil && strings.Join(folderParts, "/") != located.folder {
		return v.moveHostEntry(located, folderParts)
	}
	return nil
}

// moveHostEntry moves an entry to another folder below Hosts, creating the
// folder if needed. The entry keeps its UUID, so references to it (and its
// history) are preserved.
func (v *Vault) moveHostEntry(located locatedEntry, folderParts []string) error {
	hostsGroup, err := v.zzhGroup(hostsGroupName)
	if err != nil {
		return err
	}

	var oldParts []string
	if located.folder != "" {
		oldParts = strings.Split(located.folder, "/")
	}
	oldGroup := findFolder(hostsGroup, oldParts)
	if oldGroup == nil {
		return errors.New("cannot find the current folder of the host")
	}

	moved := *located.entry
	now := wrappers.Now()
	moved.Times.LocationChanged = &now

	removed := false
	for index := range oldGroup.Entries {
		if oldGroup.Entries[index].UUID.Compare(moved.UUID) {
			oldGroup.Entries = append(oldGroup.Entries[:index], oldGroup.Entries[index+1:]...)
			removed = true
			break
		}
	}
	if !removed {
		return errors.New("cannot remove the host from its current folder")
	}

	// Only the Entries slice of the old group changed above; hostsGroup
	// still points to the right group, and ensureFolder only touches the
	// Groups slices below it.
	target := ensureFolder(hostsGroup, folderParts)
	target.Entries = append(target.Entries, moved)
	return nil
}

// findFolder returns the existing group at the given path below parent, or
// nil.
func findFolder(parent *gokeepasslib.Group, parts []string) *gokeepasslib.Group {
	current := parent
	for _, part := range parts {
		current = childGroup(current, part)
		if current == nil {
			return nil
		}
	}
	return current
}
