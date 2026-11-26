package dbcmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zzh/internal/core/config"
	"zzh/internal/core/vault"
	"zzh/internal/core/vault/vaulttest"
)

// fixture prepares an empty config path and two zzh databases.
func fixture(t *testing.T) (configPath, personal, work string) {
	t.Helper()
	return filepath.Join(t.TempDir(), "config.json"), vaulttest.NewDatabase(t), vaulttest.NewDatabase(t)
}

// testModify is session.Modify for the test databases: same password for
// all, no prompt, nothing written when the change says so.
func testModify(cfg config.Config, name string, change func(v *vault.Vault) error) error {
	resolved, err := cfg.Resolve(name)
	if err != nil {
		return err
	}
	v, err := vault.Open(resolved.Path, vaulttest.Password, resolved.RootGroup)
	if err != nil {
		return err
	}
	if err := change(v); err != nil {
		if errors.Is(err, vault.ErrNothingToSave) {
			return nil
		}
		return err
	}
	return v.Save(false)
}

// run reloads the config file before each command, like the real binary.
func run(t *testing.T, configPath string, args ...string) (string, error) {
	t.Helper()

	cfg, exists, err := config.Load(configPath)
	loaded := config.Loaded{Config: cfg, Path: configPath, Exists: exists}

	var out bytes.Buffer
	runErr := Run(args, loaded, err, testModify, &out)
	return out.String(), runErr
}

// A database that never saw zzh gets its groups; an existing zzh database
// is bookmarked without being rewritten.
func TestAddCreatesTheStructure(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	foreign := vaulttest.NewForeignDatabase(t, "Email")

	out, err := run(t, configPath, "add", "mixed", foreign)
	if err != nil || !strings.Contains(out, "Created the groups SSH/Hosts and SSH/Keys") {
		t.Fatalf("expected the groups to be created: %v\n%s", err, out)
	}
	v, err := vaulttest.Open(foreign)
	if err != nil || v.Structure() != vault.StructureComplete {
		t.Fatalf("the database must now have the zzh groups (%v)", err)
	}

	ready := vaulttest.NewDatabase(t)
	before, _ := os.ReadFile(ready)
	out, err = run(t, configPath, "add", "ready", ready)
	after, _ := os.ReadFile(ready)
	if err != nil || strings.Contains(out, "Created") || !bytes.Equal(before, after) {
		t.Fatalf("an existing zzh database must be bookmarked untouched: %v\n%s", err, out)
	}
}

// An "SSH" group without Hosts and Keys is not ours: stop, and do not
// bookmark the database.
func TestAddStopsOnForeignSSHGroup(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	path := vaulttest.NewForeignDatabase(t, "SSH")
	before, _ := os.ReadFile(path)

	_, err := run(t, configPath, "add", "other", path)
	if err == nil || !strings.Contains(err.Error(), `group "SSH"`) {
		t.Fatalf("expected a refusal naming the SSH group, got %v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatalf("the database must not be changed")
	}
	cfg, _, _ := config.Load(configPath)
	if len(cfg.Databases) != 0 {
		t.Fatalf("the bookmark must not be saved: %v", cfg.Databases)
	}
}

// A wrong password (or a file that is not a database) leaves no bookmark.
func TestAddRefusesUnreadableDatabase(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	notADatabase := filepath.Join(t.TempDir(), "notes.kdbx")
	os.WriteFile(notADatabase, []byte("x"), 0o600)

	if _, err := run(t, configPath, "add", "broken", notADatabase); err == nil {
		t.Fatalf("an unreadable file must be refused")
	}
	cfg, _, _ := config.Load(configPath)
	if len(cfg.Databases) != 0 {
		t.Fatalf("the bookmark must not be saved")
	}
}

func TestBookmarkLifecycle(t *testing.T) {
	configPath, personal, work := fixture(t)

	if _, err := run(t, configPath, "add", "personal", personal); err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if _, err := run(t, configPath, "add", "work", work); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	out, err := run(t, configPath, "ls")
	if err != nil {
		t.Fatalf("ls failed: %v", err)
	}
	// The first bookmark is the default and is marked with "*".
	if defaultName(out) != "personal" {
		t.Fatalf("default not marked:\n%s", out)
	}

	if _, err := run(t, configPath, "default", "work"); err != nil {
		t.Fatalf("default failed: %v", err)
	}
	cfg, _, _ := config.Load(configPath)
	if cfg.DefaultDatabase != "work" {
		t.Fatalf("default not saved: %q", cfg.DefaultDatabase)
	}

	out, err = run(t, configPath, "rm", "work")
	if err != nil {
		t.Fatalf("rm failed: %v", err)
	}
	if _, statErr := os.Stat(work); statErr != nil {
		t.Fatalf("rm must not delete the database file")
	}

	// With a single bookmark left it becomes the implicit default.
	out, _ = run(t, configPath, "ls")
	if defaultName(out) != "personal" {
		t.Fatalf("the only database must be shown as default:\n%s", out)
	}
}

func TestListFlagsMissingFiles(t *testing.T) {
	configPath, personal, _ := fixture(t)

	if _, err := run(t, configPath, "add", "personal", personal); err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if err := os.Remove(personal); err != nil {
		t.Fatalf("cannot remove test file: %v", err)
	}

	out, err := run(t, configPath, "ls")
	if err != nil || !strings.Contains(out, "MISSING") {
		t.Fatalf("a moved database must be flagged:\n%s (%v)", out, err)
	}
}

func TestErrors(t *testing.T) {
	configPath, personal, _ := fixture(t)

	cases := map[string][]string{
		"no action":        {},
		"unknown action":   {"edit"},
		"missing file":     {"add", "x", filepath.Join(filepath.Dir(personal), "nope.kdbx")},
		"directory":        {"add", "x", filepath.Dir(personal)},
		"unknown rm":       {"rm", "ghost"},
		"unknown default":  {"default", "ghost"},
		"too many params":  {"ls", "extra"},
		"missing add path": {"add", "x"},
	}

	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := run(t, configPath, args...); err == nil {
				t.Fatalf("expected an error for %q", args)
			}
		})
	}
}

// defaultName returns the name on the row marked with "*" in "db ls".
func defaultName(out string) string {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "*" {
			return fields[1]
		}
	}
	return ""
}
