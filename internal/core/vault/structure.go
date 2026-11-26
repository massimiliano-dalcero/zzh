package vault

import (
	"errors"

	"github.com/tobischo/gokeepasslib/v3"
)

// Structure describes whether a database has the groups zzh works in:
// <root group>/Hosts and <root group>/Keys, directly under the database's
// top-level group.
type Structure int

const (
	// StructureComplete: the root group exists with Hosts and Keys.
	StructureComplete Structure = iota

	// StructureMissing: there is no root group at all; zzh can create the
	// whole structure without touching anything that exists.
	StructureMissing

	// StructureIncomplete: a group with the root group's name exists but
	// lacks Hosts or Keys. It was not created by zzh (or was changed by
	// hand), so zzh leaves it alone rather than guess what it is for.
	StructureIncomplete
)

// ErrNothingToSave can be returned by a change given to session.Modify (or
// vaulttest.Modify) to say that the database was not modified and must not
// be written: saving would only produce a new file with the same content.
var ErrNothingToSave = errors.New("nothing to save")

// Structure reports the state of the zzh groups in this database.
func (v *Vault) Structure() Structure {
	if v.db.Content == nil || v.db.Content.Root == nil || len(v.db.Content.Root.Groups) == 0 {
		return StructureMissing
	}
	zzhRoot := childGroup(&v.db.Content.Root.Groups[0], v.rootGroup)
	switch {
	case zzhRoot == nil:
		return StructureMissing
	case childGroup(zzhRoot, hostsGroupName) != nil && childGroup(zzhRoot, keysGroupName) != nil:
		return StructureComplete
	}
	return StructureIncomplete
}

// CreateStructure adds <root group> with Hosts and Keys under the top-level
// group, the layout "zzh init" creates for new databases. It refuses unless
// the structure is missing entirely: it never adds groups inside a group
// that already exists.
func (v *Vault) CreateStructure() error {
	if v.Structure() != StructureMissing {
		return errors.New("the zzh groups can only be created where none exists")
	}
	if v.db.Content == nil {
		// Never the case for a file that was decoded: it always has a body.
		return errors.New("the database has no content")
	}

	if v.db.Content.Root == nil {
		v.db.Content.Root = &gokeepasslib.RootData{}
	}
	// A KDBX file normally has one top-level group (KeePassXC's "Root");
	// an empty one gets it, named like KeePassXC does.
	if len(v.db.Content.Root.Groups) == 0 {
		v.db.Content.Root.Groups = []gokeepasslib.Group{newGroup("Root")}
	}

	top := &v.db.Content.Root.Groups[0]
	top.Groups = append(top.Groups, newGroup(v.rootGroup, newGroup(hostsGroupName), newGroup(keysGroupName)))
	return nil
}
