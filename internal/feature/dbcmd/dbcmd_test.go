package dbcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zzh/internal/core/config"
)

// fixture prepares an empty config path and two database files.
func fixture(t *testing.T) (configPath, personal, work string) {
	t.Helper()
	dir := t.TempDir()

	personal = filepath.Join(dir, "personal.kdbx")
	work = filepath.Join(dir, "work.kdbx")
	for _, path := range []string{personal, work} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("cannot create test file: %v", err)
		}
	}

	return filepath.Join(dir, "config.json"), personal, work
}

// run reloads the config file before each command, like the real binary.
func run(t *testing.T, configPath string, args ...string) (string, error) {
	t.Helper()

	cfg, exists, err := config.Load(configPath)
	loaded := config.Loaded{Config: cfg, Path: configPath, Exists: exists}

	var out bytes.Buffer
	runErr := Run(args, loaded, err, &out)
	return out.String(), runErr
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
