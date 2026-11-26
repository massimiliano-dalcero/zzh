package approval

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGrantAllowedAndExpiry(t *testing.T) {
	store := NewStore(t.TempDir())
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

	if store.Allowed("git.exe", "github", now) {
		t.Fatalf("nothing is allowed before a grant")
	}
	if err := store.Grant("git.exe", "github", now, 15*time.Minute); err != nil {
		t.Fatalf("grant failed: %v", err)
	}
	if !store.Allowed("git.exe", "github", now.Add(14*time.Minute)) {
		t.Fatalf("the grant must hold for its duration")
	}
	if store.Allowed("git.exe", "github", now.Add(15*time.Minute)) {
		t.Fatalf("the grant must expire")
	}
	// Program + host: another host, or another program, is asked again.
	if store.Allowed("git.exe", "prod-db", now) || store.Allowed("curl.exe", "github", now) {
		t.Fatalf("a grant covers only its program and host")
	}
}

func TestGrantDropsExpiredAndReplaces(t *testing.T) {
	store := NewStore(t.TempDir())
	now := time.Now()
	store.Grant("a", "h1", now.Add(-time.Hour), time.Minute) // already expired
	store.Grant("b", "h2", now, time.Minute)
	store.Grant("b", "h2", now, time.Hour) // replaces the previous one

	entries := store.read()
	if len(entries) != 1 || entries[0].Program != "b" || !entries[0].Until.Equal(now.Add(time.Hour)) {
		t.Fatalf("unexpected entries %+v", entries)
	}
}

func TestClearAndDamagedFile(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	now := time.Now()
	store.Grant("a", "h", now, time.Hour)
	if err := store.Clear(); err != nil || store.Allowed("a", "h", now) {
		t.Fatalf("clear must forget everything: %v", err)
	}
	if err := store.Clear(); err != nil {
		t.Fatalf("clearing twice must not fail: %v", err)
	}

	os.WriteFile(filepath.Join(dir, fileName), []byte("{broken"), 0o600)
	if store.Allowed("a", "h", now) {
		t.Fatalf("a damaged file means no authorization")
	}
	if err := store.Grant("a", "h", now, time.Hour); err != nil || !store.Allowed("a", "h", now) {
		t.Fatalf("a damaged file must be replaced by a grant: %v", err)
	}
}
