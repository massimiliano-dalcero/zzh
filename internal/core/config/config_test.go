package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("cannot write test config: %v", err)
	}
	return path
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, exists, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Fatalf("a missing file must be reported as not existing")
	}
	if cfg.Lock.IdleTimeout.Std() != 15*time.Minute {
		t.Fatalf("expected default idle timeout, got %v", cfg.Lock.IdleTimeout.Std())
	}
}

func TestLoadPartialFileKeepsDefaults(t *testing.T) {
	path := writeFile(t, `{"lock": {"idle_timeout": "5m"}}`)

	cfg, exists, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatalf("file should be reported as existing")
	}
	if cfg.Lock.IdleTimeout.Std() != 5*time.Minute {
		t.Fatalf("idle timeout not applied: %v", cfg.Lock.IdleTimeout.Std())
	}
	// Values absent from the file must keep their defaults.
	if cfg.Lock.MaxLifetime.Std() != 8*time.Hour {
		t.Fatalf("max lifetime lost its default: %v", cfg.Lock.MaxLifetime.Std())
	}
	if cfg.SSH.Binary != "ssh" {
		t.Fatalf("ssh binary lost its default: %q", cfg.SSH.Binary)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := writeFile(t, `{"lock": {"idle_timout": "5m"}}`)

	if _, _, err := Load(path); err == nil {
		t.Fatalf("a misspelled field must be rejected")
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := map[string]string{
		"bad duration":     `{"lock": {"idle_timeout": "soon"}}`,
		"numeric duration": `{"lock": {"idle_timeout": 300}}`,
		"bad key type":     `{"keys": {"default_type": "dsa"}}`,
		"small rsa":        `{"keys": {"rsa_bits": 1024}}`,
		"bad extra option": `{"ssh": {"extra_options": ["ServerAliveInterval 30"]}}`,
		"empty binary":     `{"ssh": {"binary": ""}}`,
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Load(writeFile(t, content)); err == nil {
				t.Fatalf("expected an error for %s", content)
			}
		})
	}
}

func TestResolveExpandsHomeInDatabasePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory available")
	}

	cfg, _, err := Load(writeFile(t, `{"databases": {"main": {"path": "~/vault/ssh.kdbx"}}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The stored value stays as written, so that rewriting the config file
	// does not replace "~" with an absolute path.
	if cfg.Databases["main"].Path != "~/vault/ssh.kdbx" {
		t.Fatalf("stored path was modified: %q", cfg.Databases["main"].Path)
	}

	resolved, err := cfg.Resolve("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join(home, "vault", "ssh.kdbx")
	if resolved.Path != expected {
		t.Fatalf("expected %q, got %q", expected, resolved.Path)
	}
}

func TestLoadRejectsInvalidBookmarks(t *testing.T) {
	cases := map[string]string{
		"dangling default": `{"databases": {"a": {"path": "a.kdbx"}}, "default_database": "b"}`,
		"empty path":       `{"databases": {"a": {"path": ""}}}`,
		"bad name":         `{"databases": {"my db": {"path": "a.kdbx"}}}`,
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Load(writeFile(t, content)); err == nil {
				t.Fatalf("expected an error for %s", content)
			}
		})
	}
}

func TestSaveThenLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")

	original := Defaults()
	original.Lock.IdleTimeout = Duration(42 * time.Second)

	if err := Save(path, original); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read saved file: %v", err)
	}
	// Durations must be stored in a human readable form.
	if !strings.Contains(string(data), `"42s"`) {
		t.Fatalf("duration not stored as string:\n%s", data)
	}

	loaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.Lock.IdleTimeout != original.Lock.IdleTimeout {
		t.Fatalf("round trip changed the value: %v", loaded.Lock.IdleTimeout.Std())
	}
}
