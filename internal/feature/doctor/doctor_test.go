package doctor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zzh/internal/core/config"
)

func TestIsUsableBuild(t *testing.T) {
	cases := []struct {
		goos   string
		banner string
		usable bool
	}{
		{"windows", "OpenSSH_for_Windows_9.5p2, LibreSSL 3.8.2", true},
		// Git for Windows ships an MSYS build without the Windows marker.
		{"windows", "OpenSSH_9.5p1, OpenSSL 3.1.4 24 Oct 2023", false},
		{"linux", "OpenSSH_9.6p1 Ubuntu-3ubuntu13.5, OpenSSL 3.0.13", true},
		{"darwin", "OpenSSH_9.8p1, LibreSSL 3.3.6", true},
	}

	for _, tc := range cases {
		if got := isUsableBuild(tc.goos, tc.banner); got != tc.usable {
			t.Errorf("%s %q: expected %v, got %v", tc.goos, tc.banner, tc.usable, got)
		}
	}
}

func TestCheckDatabases(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "ssh.kdbx")
	if err := os.WriteFile(file, []byte("not really a database"), 0o600); err != nil {
		t.Fatalf("cannot create test file: %v", err)
	}

	cases := map[string]struct {
		databases       map[string]config.Database
		defaultDatabase string
		ok              bool
	}{
		"none bookmarked": {nil, "", false},
		"missing":         {map[string]config.Database{"a": {Path: filepath.Join(dir, "missing.kdbx")}}, "", false},
		"directory":       {map[string]config.Database{"a": {Path: dir}}, "", false},
		"single file":     {map[string]config.Database{"a": {Path: file}}, "", true},
		// Two valid files but no default: commands without --db would fail.
		"no default":   {map[string]config.Database{"a": {Path: file}, "b": {Path: file}}, "", false},
		"with default": {map[string]config.Database{"a": {Path: file}, "b": {Path: file}}, "b", true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Databases = tc.databases
			cfg.DefaultDatabase = tc.defaultDatabase

			var out bytes.Buffer
			r := &reporter{out: &out}
			checkDatabases(r, cfg)

			if r.failed == tc.ok {
				t.Fatalf("expected ok=%v, report: %s", tc.ok, out.String())
			}
		})
	}
}

func TestRunReportsEveryFailure(t *testing.T) {
	loaded := config.Loaded{Config: config.Defaults(), Path: "config.json"}
	loaded.Config.SSH.Binary = "zzh-no-such-ssh-binary"

	var out bytes.Buffer
	if err := Run(loaded, nil, &out); err == nil {
		t.Fatalf("doctor must fail when database and ssh are missing")
	}

	report := out.String()
	// Both problems must be listed in the same run.
	if !strings.Contains(report, "database: none bookmarked") || !strings.Contains(report, "zzh-no-such-ssh-binary") {
		t.Fatalf("not every failure reported:\n%s", report)
	}
}
