package vault

import (
	"errors"
	"testing"
)

func TestRemoveHostWithoutRecycleBin(t *testing.T) {
	v, path := openStandard(t)

	recycled, err := v.RemoveHost("web01")
	if err != nil || recycled {
		t.Fatalf("expected a permanent removal, got recycled=%v err=%v", recycled, err)
	}
	if err := v.Save(false); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	reopened, err := Open(path, testPassword, "SSH")
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	if _, err := reopened.FindHost("web01"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed host still found: %v", err)
	}
	// Recorded for KeePass merges.
	if len(reopened.db.Content.Root.DeletedObjects) != 1 {
		t.Fatalf("the deletion must be recorded among the deleted objects")
	}
	hosts, _ := reopened.Hosts()
	if len(hosts) != 5 {
		t.Fatalf("only one host must be removed, %d left", len(hosts))
	}
}

func TestRemoveHostToRecycleBin(t *testing.T) {
	v, path := openStandard(t)
	v.db.Content.Meta.RecycleBinEnabled.Bool = true

	// Reached through an alias, like every host command.
	if err := v.AddHostAliases("web01", []string{"w1"}); err != nil {
		t.Fatalf("cannot add alias: %v", err)
	}
	recycled, err := v.RemoveHost("w1")
	if err != nil || !recycled {
		t.Fatalf("expected the entry in the recycle bin: recycled=%v err=%v", recycled, err)
	}
	if err := v.Save(false); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	reopened, err := Open(path, testPassword, "SSH")
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	if _, err := reopened.FindHost("web01"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a recycled host must not be reachable: %v", err)
	}

	// The bin is a top-level group registered in the metadata, holding the
	// entry with its history, ready to be restored in KeePassXC.
	meta := reopened.db.Content.Meta
	bin := findGroup(reopened.db.Content.Root.Groups, meta.RecycleBinUUID)
	if bin == nil || len(bin.Entries) != 1 || entryTitle(&bin.Entries[0]) != "web01" {
		t.Fatalf("the host must be in the recycle bin")
	}
	if len(bin.Entries[0].Histories) == 0 {
		t.Fatalf("the history must move with the entry")
	}

	// A second removal reuses the same bin.
	if _, err := reopened.RemoveHost("bastion"); err != nil {
		t.Fatalf("second removal failed: %v", err)
	}
	if bin := findGroup(reopened.db.Content.Root.Groups, meta.RecycleBinUUID); len(bin.Entries) != 2 {
		t.Fatalf("the existing bin must be reused")
	}
}

func TestRemoveKey(t *testing.T) {
	v, path := openStandard(t)
	addTestKey(t, v, "k1")
	if err := v.SetHostKey("web01", "k1"); err != nil {
		t.Fatalf("cannot associate key: %v", err)
	}

	// Still used: refused, naming the host.
	if _, _, err := v.RemoveKey("k1", false); err == nil {
		t.Fatalf("a key in use must not be removed without force")
	}

	recycled, detached, err := v.RemoveKey("k1", true)
	if err != nil || recycled || len(detached) != 1 || detached[0] != "web01" {
		t.Fatalf("unexpected result: recycled=%v detached=%v err=%v", recycled, detached, err)
	}
	if err := v.Save(false); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	reopened, err := Open(path, testPassword, "SSH")
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	if _, err := reopened.FindKey("k1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed key still found: %v", err)
	}
	host, err := reopened.FindHost("web01")
	if err != nil || host.KeyID != "" {
		t.Fatalf("the host must no longer point to the removed key: %+v", host)
	}

	if _, _, err := reopened.RemoveKey("missing", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
