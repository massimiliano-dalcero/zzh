package session

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zzh/internal/core/config"
	"zzh/internal/core/daemon"
	"zzh/internal/core/prompt"
	"zzh/internal/core/vault"
)

// TestMain points the daemon to a private directory: the tests must never
// reach the user's real daemon, nor start one in the user's profile.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "zzh-session-test")
	if err != nil {
		panic(err)
	}
	os.Setenv(daemon.EnvDir, dir)

	code := m.Run()

	// Stop a daemon a test may have started, then remove its directory.
	// Both are best effort: failures only leave temporary files behind.
	_ = daemon.NewClient(dir).Stop()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestOpenVaultWithoutDatabase(t *testing.T) {
	_, err := OpenVault(config.Defaults(), "")
	if err == nil || !strings.Contains(err.Error(), "no database configured") {
		t.Fatalf("expected a clear error, got %v", err)
	}
}

func TestOpenVaultUnknownBookmark(t *testing.T) {
	cfg := config.Defaults()
	cfg.Databases = map[string]config.Database{"personal": {Path: "p.kdbx"}}

	_, err := OpenVault(cfg, "work")
	if err == nil || !strings.Contains(err.Error(), "not bookmarked") {
		t.Fatalf("expected an unknown bookmark error, got %v", err)
	}
}

// With nobody to ask (a script, a scheduled task) the prompt must refuse to
// run instead of hanging, and the error must say what to do. The absence of
// a terminal is simulated: run from a console, the real prompt would find
// the controlling terminal and wait for an answer.
func TestOpenVaultRequiresTerminal(t *testing.T) {
	original := passwordPrompt
	passwordPrompt = func(string) (string, error) {
		return "", fmt.Errorf("cannot ask for the password: %w", prompt.ErrNoTerminal)
	}
	t.Cleanup(func() { passwordPrompt = original })

	cfg := config.Defaults()
	cfg.Databases = map[string]config.Database{"personal": {Path: "unused.kdbx"}}

	_, err := OpenVault(cfg, "")
	if !errors.Is(err, ErrLocked) || !strings.Contains(err.Error(), "no terminal") || !strings.Contains(err.Error(), "zzh unlock") {
		t.Fatalf("expected a locked/no-terminal error, got %v", err)
	}
}

// With automatic unlock disabled, a locked database is refused without
// even trying to ask.
func TestOpenVaultLockedWithoutAutostart(t *testing.T) {
	cfg := config.Defaults()
	cfg.Daemon.Autostart = false
	cfg.Databases = map[string]config.Database{"personal": {Path: "unused.kdbx"}}

	_, err := OpenVault(cfg, "")
	if !errors.Is(err, ErrLocked) || !strings.Contains(err.Error(), "daemon.autostart") {
		t.Fatalf("expected a locked error mentioning daemon.autostart, got %v", err)
	}
}

// OpenVault must take the password from the daemon without any prompt. The
// daemon runs inside the test process (starting a real one would launch the
// test binary instead of zzh); the real process is covered by the zzh
// integration tests.
func TestOpenVaultUsesDaemon(t *testing.T) {
	database := createDatabase(t)
	cfg := config.Defaults()
	cfg.Databases = map[string]config.Database{"s": {Path: database.Path}}

	dir := os.Getenv(daemon.EnvDir)
	finished := make(chan error, 1)
	go func() { finished <- daemon.Run(dir, os.Getpid()) }()
	client := daemon.NewClient(dir)
	waitForDaemon(t, client)

	if err := client.Put(database.Path, "pw", policy(cfg), false); err != nil {
		t.Fatalf("put failed: %v", err)
	}
	if _, err := OpenVault(cfg, ""); err != nil {
		t.Fatalf("the password kept by the daemon must be used: %v", err)
	}

	// A password that became wrong (master password changed) is reported
	// and forgotten.
	if err := client.Put(database.Path, "old-password", policy(cfg), false); err != nil {
		t.Fatalf("put failed: %v", err)
	}
	_, err := OpenVault(cfg, "")
	if !errors.Is(err, vault.ErrWrongPassword) || !strings.Contains(err.Error(), "zzh unlock") {
		t.Fatalf("expected a wrong password error pointing to unlock, got %v", err)
	}
	if _, found, _ := client.Get(database.Path, policy(cfg), false); found {
		t.Fatalf("the invalid password must be forgotten")
	}

	if err := client.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	<-finished
}

func waitForDaemon(t *testing.T, client *daemon.Client) {
	t.Helper()
	for i := 0; i < 250; i++ {
		if _, err := client.Status(); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("daemon did not start")
}

// createDatabase builds a real (small) database through the public API.
func createDatabase(t *testing.T) config.ResolvedDatabase {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.kdbx")
	if err := vault.Create(path, "pw", "s", "SSH"); err != nil {
		t.Fatalf("cannot create database: %v", err)
	}
	return config.ResolvedDatabase{Name: "s", Path: path, RootGroup: "SSH"}
}

// When another program saves the file while zzh is working, the change must
// be applied again on the fresh content instead of overwriting it.
func TestModifyRetriesAfterExternalChange(t *testing.T) {
	database := createDatabase(t)

	attempts := 0
	err := modifyUnlocked(database, "pw", false, func(v *vault.Vault) error {
		attempts++
		if attempts == 1 {
			// Simulate KeePassXC saving a new host during the first attempt.
			other, err := vault.Open(database.Path, "pw", "SSH")
			if err != nil {
				return err
			}
			if err := other.AddHost(vault.NewHost{Name: "from-keepassxc", Address: "a.local"}); err != nil {
				return err
			}
			if err := other.Save(false); err != nil {
				return err
			}
		}
		return v.AddHost(vault.NewHost{Name: "from-zzh", Address: "b.local"})
	})
	if err != nil {
		t.Fatalf("modify failed: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}

	v, err := vault.Open(database.Path, "pw", "SSH")
	if err != nil {
		t.Fatalf("cannot reopen: %v", err)
	}
	for _, name := range []string{"from-keepassxc", "from-zzh"} {
		if _, err := v.FindHost(name); err != nil {
			t.Fatalf("%s lost: %v", name, err)
		}
	}
}

func TestModifyStopsOnChangeError(t *testing.T) {
	database := createDatabase(t)
	before, err := os.ReadFile(database.Path)
	if err != nil {
		t.Fatalf("cannot read database: %v", err)
	}

	failure := errors.New("refused")
	err = modifyUnlocked(database, "pw", true, func(v *vault.Vault) error { return failure })
	if !errors.Is(err, failure) {
		t.Fatalf("expected the change error, got %v", err)
	}

	after, _ := os.ReadFile(database.Path)
	if !bytes.Equal(before, after) {
		t.Fatalf("a failed change must not write the database")
	}
}

// withEnvironmentPassword sets ZZH_PASSWORD, takes it like main does, and
// restores the package state at the end of the test.
func withEnvironmentPassword(t *testing.T, value string) {
	t.Helper()
	t.Setenv(EnvPassword, value)
	if err := TakePasswordFromEnvironment(); err != nil {
		t.Fatalf("cannot take the password: %v", err)
	}
	t.Cleanup(func() { environmentPassword = "" })
}

func TestTakePasswordRemovesVariable(t *testing.T) {
	withEnvironmentPassword(t, "pw")

	if _, present := os.LookupEnv(EnvPassword); present {
		t.Fatalf("%s must be removed so that child processes do not inherit it", EnvPassword)
	}
	if environmentPassword != "pw" {
		t.Fatalf("password not kept in memory")
	}
}

// With ZZH_PASSWORD set no prompt is needed, which is also why this test
// can open a database without a terminal.
func TestOpenVaultWithEnvironmentPassword(t *testing.T) {
	database := createDatabase(t)
	cfg := config.Defaults()
	cfg.WarnEnvPassword = false
	cfg.Databases = map[string]config.Database{"s": {Path: database.Path}}

	withEnvironmentPassword(t, "pw")
	if _, err := OpenVault(cfg, ""); err != nil {
		t.Fatalf("open with ZZH_PASSWORD failed: %v", err)
	}

	environmentPassword = "wrong"
	_, err := OpenVault(cfg, "")
	if !errors.Is(err, vault.ErrWrongPassword) || !strings.Contains(err.Error(), EnvPassword) {
		t.Fatalf("a wrong password must mention %s, got %v", EnvPassword, err)
	}
}

// With keep_open, reads get the daemon's copy (read-only) while writes go
// to the file; the next read must see the write.
func TestKeepOpenReadsCopyAndWritesFile(t *testing.T) {
	database := createDatabase(t)
	cfg := config.Defaults()
	cfg.Daemon.KeepOpen = true
	cfg.Databases = map[string]config.Database{"s": {Path: database.Path}}

	dir := os.Getenv(daemon.EnvDir)
	finished := make(chan error, 1)
	go func() { finished <- daemon.Run(dir, os.Getpid()) }()
	client := daemon.NewClient(dir)
	waitForDaemon(t, client)
	defer func() {
		client.Stop()
		<-finished
	}()

	if err := client.Put(database.Path, "pw", policy(cfg), true); err != nil {
		t.Fatalf("put failed: %v", err)
	}

	v, err := OpenVault(cfg, "")
	if err != nil {
		t.Fatalf("open through the daemon failed: %v", err)
	}
	if err := v.Save(false); !errors.Is(err, vault.ErrReadOnly) {
		t.Fatalf("a vault read from the daemon must be read-only, got %v", err)
	}

	err = Modify(cfg, "", func(v *vault.Vault) error {
		return v.AddHost(vault.NewHost{Name: "added", Address: "added.lan"})
	})
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	v, err = OpenVault(cfg, "")
	if err != nil {
		t.Fatalf("second read failed: %v", err)
	}
	if _, err := v.FindHost("added"); err != nil {
		t.Fatalf("the daemon copy must follow the write: %v", err)
	}
}

// Through the shim, Enter at the password question declines the unlock:
// the result is a "locked" error at once, without trying the empty
// password (which would cost the key derivation).
func TestEmptyPasswordDeclinesWhenAllowed(t *testing.T) {
	original := passwordPrompt
	var question string
	passwordPrompt = func(message string) (string, error) {
		question = message
		return "", nil
	}
	SkipWithEmptyPassword = true
	t.Cleanup(func() {
		passwordPrompt = original
		SkipWithEmptyPassword = false
	})

	cfg := config.Defaults()
	cfg.Databases = map[string]config.Database{"personal": {Path: "unused.kdbx"}}

	_, err := OpenVault(cfg, "")
	if !errors.Is(err, ErrLocked) || !strings.Contains(err.Error(), "no password typed") {
		t.Fatalf("expected a declined unlock, got %v", err)
	}
	if !strings.Contains(question, "[Enter: plain ssh]") {
		t.Fatalf("the question must say that Enter skips: %q", question)
	}
}
