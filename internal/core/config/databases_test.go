package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAddDatabaseFirstBecomesDefault(t *testing.T) {
	cfg := Defaults()

	if err := cfg.AddDatabase("personal", "personal.kdbx"); err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if err := cfg.AddDatabase("work", "work.kdbx"); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	if cfg.DefaultDatabase != "personal" {
		t.Fatalf("the first database must become the default, got %q", cfg.DefaultDatabase)
	}
	// Relative paths must be stored as absolute ones.
	if !filepath.IsAbs(cfg.Databases["work"].Path) {
		t.Fatalf("path not made absolute: %q", cfg.Databases["work"].Path)
	}

	if err := cfg.AddDatabase("work", "other.kdbx"); err == nil {
		t.Fatalf("a duplicate name must be rejected")
	}
	if err := cfg.AddDatabase("bad name", "x.kdbx"); err == nil {
		t.Fatalf("an invalid name must be rejected")
	}
}

func TestResolve(t *testing.T) {
	cfg := Defaults()
	cfg.RootGroup = "SSH"
	cfg.Databases = map[string]Database{
		"personal": {Path: "/p.kdbx"},
		"work":     {Path: "/w.kdbx", RootGroup: "Servers"},
	}

	// Two databases and no default: the user must choose.
	if _, err := cfg.Resolve(""); err == nil || !strings.Contains(err.Error(), "none is the default") {
		t.Fatalf("expected an ambiguity error, got %v", err)
	}

	cfg.DefaultDatabase = "personal"
	resolved, err := cfg.Resolve("")
	if err != nil || resolved.Name != "personal" || resolved.RootGroup != "SSH" {
		t.Fatalf("default not used: %+v, %v", resolved, err)
	}

	// The flag wins over the default, and the bookmark root group applies.
	resolved, err = cfg.Resolve("work")
	if err != nil || resolved.Name != "work" || resolved.RootGroup != "Servers" {
		t.Fatalf("requested database not used: %+v, %v", resolved, err)
	}

	if _, err := cfg.Resolve("missing"); err == nil {
		t.Fatalf("an unknown name must fail")
	}
}

func TestResolveSingleDatabaseWithoutDefault(t *testing.T) {
	cfg := Defaults()
	cfg.Databases = map[string]Database{"only": {Path: "/o.kdbx"}}

	resolved, err := cfg.Resolve("")
	if err != nil || resolved.Name != "only" {
		t.Fatalf("the only database must be used: %+v, %v", resolved, err)
	}
}

func TestResolveWithoutDatabases(t *testing.T) {
	_, err := Defaults().Resolve("")
	if err == nil || !strings.Contains(err.Error(), "zzh init") {
		t.Fatalf("expected a hint about zzh init, got %v", err)
	}
}

func TestRemoveAndSetDefault(t *testing.T) {
	cfg := Defaults()
	for _, name := range []string{"a", "b"} {
		if err := cfg.AddDatabase(name, name+".kdbx"); err != nil {
			t.Fatalf("add failed: %v", err)
		}
	}

	if err := cfg.SetDefaultDatabase("b"); err != nil || cfg.DefaultDatabase != "b" {
		t.Fatalf("set default failed: %v", err)
	}
	if err := cfg.SetDefaultDatabase("zzz"); err == nil {
		t.Fatalf("an unknown default must be rejected")
	}

	// Removing the default clears it instead of silently choosing another.
	if err := cfg.RemoveDatabase("b"); err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if cfg.DefaultDatabase != "" {
		t.Fatalf("default must be cleared, got %q", cfg.DefaultDatabase)
	}
	if err := cfg.RemoveDatabase("b"); err == nil {
		t.Fatalf("removing twice must fail")
	}
}
