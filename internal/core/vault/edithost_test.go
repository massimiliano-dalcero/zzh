package vault

import (
	"strings"
	"testing"
)

func ptr[T any](value T) *T { return &value }

func TestEditHostFields(t *testing.T) {
	v, path := openStandard(t)

	// web01 starts as: deploy@10.0.0.1, password s3cret, two options.
	err := v.EditHost("web01", HostChanges{
		User:    ptr("root"),
		Port:    ptr(2222),
		Tags:    ptr([]string{"proxmox", "lab"}),
		Options: ptr([]string{}),
	})
	if err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	if err := v.Save(false); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	reopened, err := Open(path, testPassword, "SSH")
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	host, err := reopened.FindHost("web01")
	if err != nil {
		t.Fatalf("host lost: %v", err)
	}
	if host.User != "root" || host.Port != 2222 || host.Address != "10.0.0.1" {
		t.Fatalf("fields not changed as requested: %+v", host)
	}
	// Fields not mentioned are untouched.
	if host.Password != "s3cret" {
		t.Fatalf("password must be kept: %q", host.Password)
	}
	if strings.Join(host.Tags, ",") != "proxmox,lab" || len(host.Options) != 0 {
		t.Fatalf("tags/options not replaced: %+v", host)
	}

	located, _ := reopened.findHostEntry("web01")
	if len(located.entry.Histories) != 1 || len(located.entry.Histories[0].Entries) != 1 {
		t.Fatalf("the previous version must be in the history")
	}
}

func TestEditHostRenameAndMove(t *testing.T) {
	v, path := openStandard(t)
	if err := v.AddHostAliases("web01", []string{"w1"}); err != nil {
		t.Fatalf("cannot add alias: %v", err)
	}

	// Reached by its alias, renamed and moved from prod to lab/new.
	if err := v.EditHost("w1", HostChanges{Name: ptr("front01"), Folder: ptr("lab/new")}); err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	if err := v.Save(false); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	reopened, err := Open(path, testPassword, "SSH")
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	host, err := reopened.FindHost("front01")
	if err != nil || host.Folder != "lab/new" || host.Password != "s3cret" {
		t.Fatalf("rename/move failed: %+v (%v)", host, err)
	}
	if host, err := reopened.FindHost("w1"); err != nil || host.Name != "front01" {
		t.Fatalf("aliases must follow the renamed host: %+v (%v)", host, err)
	}
	if _, err := reopened.FindHost("web01"); err == nil {
		t.Fatalf("the old name must not resolve any more")
	}

	// The other hosts, including those left in the old folder, are intact.
	hosts, _ := reopened.Hosts()
	if len(hosts) != 6 {
		t.Fatalf("no host must be lost or duplicated, got %d", len(hosts))
	}
}

func TestEditHostValidation(t *testing.T) {
	v, _ := openStandard(t)

	cases := map[string]error{
		"nothing to change":   v.EditHost("web01", HostChanges{}),
		"unknown host":        v.EditHost("nope", HostChanges{User: ptr("x")}),
		"name already used":   v.EditHost("web01", HostChanges{Name: ptr("bastion")}),
		"invalid name":        v.EditHost("web01", HostChanges{Name: ptr("bad name")}),
		"invalid port":        v.EditHost("web01", HostChanges{Port: ptr(70000)}),
		"invalid option":      v.EditHost("web01", HostChanges{Options: ptr([]string{"NoEquals"})}),
		"invalid folder":      v.EditHost("web01", HostChanges{Folder: ptr("a//b")}),
		"invalid new address": v.EditHost("web01", HostChanges{Address: ptr("https://x")}),
	}
	for name, err := range cases {
		if err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}

	// Failed edits left no trace.
	located, _ := v.findHostEntry("web01")
	if len(located.entry.Histories) != 0 {
		t.Fatalf("a rejected edit must not touch the entry")
	}
}
