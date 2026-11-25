package configcmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zzh/internal/core/config"
)

// loadFrom mimics config.LoadDefault for a file inside a test directory.
func loadFrom(t *testing.T, path string) (config.Loaded, error) {
	t.Helper()
	cfg, exists, err := config.Load(path)
	return config.Loaded{Config: cfg, Path: path, Exists: exists}, err
}

func TestInitCreatesFileThenRefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zzh", "config.json")

	loaded, err := loadFrom(t, path)
	var out, errOut bytes.Buffer
	if err := Run([]string{"init"}, loaded, err, &out, &errOut); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("config file not created: %v", statErr)
	}

	// A second init must not touch the file the user may have edited.
	loaded, err = loadFrom(t, path)
	if err := Run([]string{"init"}, loaded, err, &out, &errOut); err == nil {
		t.Fatalf("init must refuse to overwrite an existing file")
	}
}

func TestShowPrintsDefaultsWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	loaded, err := loadFrom(t, path)
	var out, errOut bytes.Buffer
	if err := Run([]string{"show"}, loaded, err, &out, &errOut); err != nil {
		t.Fatalf("show failed: %v", err)
	}

	if !strings.Contains(out.String(), `"root_group": "SSH"`) {
		t.Fatalf("defaults not shown:\n%s", out.String())
	}
	// The note must stay out of stdout so that the output is valid JSON.
	if !strings.Contains(errOut.String(), "does not exist") || strings.Contains(out.String(), "#") {
		t.Fatalf("note about the missing file misplaced: out=%q err=%q", out.String(), errOut.String())
	}
}

func TestPathWorksWithBrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatalf("cannot write test file: %v", err)
	}

	loaded, err := loadFrom(t, path)
	if err == nil {
		t.Fatalf("expected the broken file to fail loading")
	}

	var out, errOut bytes.Buffer
	if err := Run([]string{"path"}, loaded, err, &out, &errOut); err != nil {
		t.Fatalf("path must work even with a broken file: %v", err)
	}
	if strings.TrimSpace(out.String()) != path {
		t.Fatalf("unexpected path %q", out.String())
	}

	if err := Run([]string{"show"}, loaded, err, &out, &errOut); err == nil {
		t.Fatalf("show must report the broken file")
	}
}

func TestUnknownAction(t *testing.T) {
	var out, errOut bytes.Buffer
	err := Run([]string{"edit"}, config.Loaded{}, nil, &out, &errOut)
	if err == nil {
		t.Fatalf("unknown action must fail")
	}

	err = Run(nil, config.Loaded{}, errors.New("unused"), &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("missing action must print usage, got %v", err)
	}
}

func TestSetGetUnsetPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	var out, errOut bytes.Buffer

	loaded, err := loadFrom(t, path)
	if err := Run([]string{"set", "ssh.use_system_keys", "true"}, loaded, err, &out, &errOut); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	if strings.TrimSpace(out.String()) != "ssh.use_system_keys = true" {
		t.Fatalf("unexpected confirmation %q", out.String())
	}

	// The change must be in the file, and a new load must see it.
	loaded, err = loadFrom(t, path)
	if err != nil || !loaded.Config.SSH.UseSystemKeys {
		t.Fatalf("setting not saved: %v", err)
	}

	out.Reset()
	if err := Run([]string{"get", "ssh.use_system_keys"}, loaded, nil, &out, &errOut); err != nil || strings.TrimSpace(out.String()) != "true" {
		t.Fatalf("get returned %q (%v)", out.String(), err)
	}

	if err := Run([]string{"unset", "ssh.use_system_keys"}, loaded, nil, &out, &errOut); err != nil {
		t.Fatalf("unset failed: %v", err)
	}
	loaded, err = loadFrom(t, path)
	if err != nil || loaded.Config.SSH.UseSystemKeys {
		t.Fatalf("default not restored: %v", err)
	}
}

func TestSetRefusesBrokenFileAndBadValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatalf("cannot write test file: %v", err)
	}
	loaded, err := loadFrom(t, path)

	var out, errOut bytes.Buffer
	if err := Run([]string{"set", "ssh.binary", "ssh"}, loaded, err, &out, &errOut); err == nil {
		t.Fatalf("a broken file must not be rewritten")
	}
	content, _ := os.ReadFile(path)
	if string(content) != "{broken" {
		t.Fatalf("the broken file was modified")
	}

	good := filepath.Join(t.TempDir(), "config.json")
	loaded, err = loadFrom(t, good)
	for _, args := range [][]string{
		{"set", "keys.rsa_bits", "1024"},
		{"set", "ssh.nope", "x"},
		{"get"},
		{"get", "ssh.binary", "extra"},
		{"unset", "lock.idle_timeout", "extra"},
	} {
		if err := Run(args, loaded, err, &out, &errOut); err == nil {
			t.Errorf("%q: expected an error", args)
		}
	}
	if _, statErr := os.Stat(good); statErr == nil {
		t.Fatalf("rejected values must not create the file")
	}
}
