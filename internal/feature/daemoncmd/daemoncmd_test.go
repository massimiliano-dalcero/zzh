package daemoncmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zzh/internal/core/config"
	"zzh/internal/core/daemon"
)

func TestUnlockReportsPolicy(t *testing.T) {
	cfg := config.Defaults()
	ops := Operations{Unlock: func(string) (config.ResolvedDatabase, error) {
		return config.ResolvedDatabase{Name: "default"}, nil
	}}

	cases := map[string]config.LockConfig{
		"15m0s without use, at most after 8h0m0s": cfg.Lock,
		"no timeout configured":                   {},
		"locks after 2h0m0s without use":          {IdleTimeout: config.Duration(2 * time.Hour)},
	}
	for expected, lock := range cases {
		cfg.Lock = lock
		var out bytes.Buffer
		if err := Unlock(nil, "", cfg, ops, &out); err != nil {
			t.Fatalf("unlock failed: %v", err)
		}
		if !strings.Contains(out.String(), expected) {
			t.Errorf("expected %q in %q", expected, out.String())
		}
	}
}

func TestLockAllOrOne(t *testing.T) {
	var gotName string
	var gotAll bool
	ops := Operations{Lock: func(name string, all bool) error {
		gotName, gotAll = name, all
		return nil
	}}

	var out bytes.Buffer
	if err := Lock(nil, "", ops, &out); err != nil || !gotAll {
		t.Fatalf("\"zzh lock\" must lock everything: %v", err)
	}
	if err := Lock(nil, "work", ops, &out); err != nil || gotAll || gotName != "work" {
		t.Fatalf("\"zzh --db work lock\" must lock only work: %v", err)
	}
}

func TestStatus(t *testing.T) {
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	cfg := config.Defaults()
	path := filepath.Join(t.TempDir(), "ssh.kdbx")
	cfg.Databases = map[string]config.Database{"default": {Path: path}}

	ops := Operations{Status: func() ([]daemon.Unlocked, error) {
		return []daemon.Unlocked{
			{Database: path, UnlockedAt: now.Add(-time.Hour), LastUsed: now.Add(-5 * time.Minute),
				Policy: daemon.Policy{IdleTimeout: 15 * time.Minute}},
			{Database: "/other.kdbx", UnlockedAt: now, LastUsed: now, Open: true, Stale: "sync in progress"},
		}, nil
	}}

	var out bytes.Buffer
	if err := Status(nil, cfg, ops, now, &out); err != nil {
		t.Fatalf("status failed: %v", err)
	}
	text := out.String()
	for _, expected := range []string{"default", "in 10m0s", "never", "password", "open (file unreadable)", "warning: /other.kdbx: sync in progress"} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q in:\n%s", expected, text)
		}
	}

	ops.Status = func() ([]daemon.Unlocked, error) { return nil, daemon.ErrNotRunning }
	out.Reset()
	if err := Status(nil, cfg, ops, now, &out); err != nil || !strings.Contains(out.String(), "No database") {
		t.Fatalf("no daemon must read as nothing unlocked: %v %q", err, out.String())
	}
}

func TestDaemonSubcommands(t *testing.T) {
	ran, stopped := false, false
	ops := Operations{
		Run:  func() error { ran = true; return nil },
		Stop: func() error { stopped = true; return nil },
	}

	var out bytes.Buffer
	if err := Daemon([]string{"run"}, ops, &out); err != nil || !ran {
		t.Fatalf("run not dispatched: %v", err)
	}
	if err := Daemon([]string{"stop"}, ops, &out); err != nil || !stopped {
		t.Fatalf("stop not dispatched: %v", err)
	}
	for _, args := range [][]string{nil, {"start"}, {"run", "x"}} {
		if err := Daemon(args, ops, &out); err == nil {
			t.Errorf("%q: expected an error", args)
		}
	}
}
