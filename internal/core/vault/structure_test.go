package vault_test

import (
	"testing"

	"zzh/internal/core/vault"
	"zzh/internal/core/vault/vaulttest"
)

func TestStructureStates(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		expected vault.Structure
	}{
		{"zzh database", vaulttest.NewDatabase(t), vault.StructureComplete},
		{"other groups only", vaulttest.NewForeignDatabase(t, "Email", "Banking"), vault.StructureMissing},
		{"empty top group", vaulttest.NewForeignDatabase(t), vault.StructureMissing},
		{"SSH without Hosts and Keys", vaulttest.NewForeignDatabase(t, "SSH"), vault.StructureIncomplete},
	}
	for _, tc := range cases {
		v, err := vaulttest.Open(tc.path)
		if err != nil {
			t.Fatalf("%s: cannot open: %v", tc.name, err)
		}
		if got := v.Structure(); got != tc.expected {
			t.Errorf("%s: structure %v, expected %v", tc.name, got, tc.expected)
		}
	}
}

// The groups are created next to the existing ones, which are kept, and the
// result is a working zzh database.
func TestCreateStructure(t *testing.T) {
	path := vaulttest.NewForeignDatabase(t, "Email")
	if err := vaulttest.Modify(path, func(v *vault.Vault) error { return v.CreateStructure() }); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	v, err := vaulttest.Open(path)
	if err != nil {
		t.Fatalf("cannot reopen: %v", err)
	}
	if v.Structure() != vault.StructureComplete {
		t.Fatalf("the structure must be complete after creation")
	}
	if err := vaulttest.Modify(path, func(v *vault.Vault) error {
		return v.AddHost(vault.NewHost{Name: "web01", Address: "10.0.0.1"})
	}); err != nil {
		t.Fatalf("the new structure must accept hosts: %v", err)
	}
}

// An existing group with zzh's name is never completed.
func TestCreateStructureRefusesExistingGroup(t *testing.T) {
	v, err := vaulttest.Open(vaulttest.NewForeignDatabase(t, "SSH"))
	if err != nil {
		t.Fatalf("cannot open: %v", err)
	}
	if err := v.CreateStructure(); err == nil {
		t.Fatalf("an existing SSH group must not be completed")
	}
}
