package daemon

import (
	"os"
	"testing"
	"time"

	"zzh/internal/core/vault"
	"zzh/internal/core/vault/vaulttest"
)

// openDatabase returns a test database with one host and a server holding
// its password with keep_open.
func openDatabase(t *testing.T) (*server, string) {
	t.Helper()
	path := vaulttest.NewDatabase(t)
	addHost(t, path, "first")

	s := newServer(time.Now)
	s.process(request{Op: opPut, Database: path, Password: vaulttest.Password, KeepOpen: true})
	return s, path
}

func addHost(t *testing.T, path, name string) {
	t.Helper()
	err := vaulttest.Modify(path, func(v *vault.Vault) error {
		return v.AddHost(vault.NewHost{Name: name, Address: name + ".lan"})
	})
	if err != nil {
		t.Fatalf("cannot add host %s: %v", name, err)
	}
}

// getSnapshot asks the server like a command would and opens the copy.
func getSnapshot(t *testing.T, s *server, path string) (*vault.Vault, response) {
	t.Helper()
	resp, _ := s.process(request{Op: opGet, Database: path, KeepOpen: true})
	if !resp.Found {
		return nil, resp
	}
	if resp.Snapshot == nil {
		t.Fatalf("keep_open must return the open database")
	}
	v, err := vault.OpenSnapshot(resp.Snapshot, resp.SnapshotKey, path, "SSH")
	if err != nil {
		t.Fatalf("cannot open the snapshot: %v", err)
	}
	return v, resp
}

func hasHost(v *vault.Vault, name string) bool {
	_, err := v.FindHost(name)
	return err == nil
}

func TestKeepOpenFollowsTheFile(t *testing.T) {
	s, path := openDatabase(t)

	v, _ := getSnapshot(t, s, path)
	if !hasHost(v, "first") {
		t.Fatalf("the snapshot must contain the database content")
	}

	// Another program (KeePassXC, a sync client, zzh with ZZH_PASSWORD)
	// changes the file: the next command must see the change.
	addHost(t, path, "second")
	v, _ = getSnapshot(t, s, path)
	if !hasHost(v, "second") {
		t.Fatalf("a change of the file must be loaded before answering")
	}
}

func TestKeepOpenSurvivesUnreadableFile(t *testing.T) {
	s, path := openDatabase(t)
	getSnapshot(t, s, path)

	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read database: %v", err)
	}

	// A sync client is writing the file: it cannot be decrypted for now.
	if err := os.WriteFile(path, good[:len(good)/2], 0o600); err != nil {
		t.Fatalf("cannot truncate database: %v", err)
	}
	v, resp := getSnapshot(t, s, path)
	if v == nil || !hasHost(v, "first") {
		t.Fatalf("the last good copy must still be served")
	}
	if resp.Stale == "" {
		t.Fatalf("the response must say that the copy is older than the file")
	}

	// The write completes: everything is back to normal.
	if err := os.WriteFile(path, good, 0o600); err != nil {
		t.Fatalf("cannot restore database: %v", err)
	}
	if _, resp := getSnapshot(t, s, path); resp.Stale != "" {
		t.Fatalf("stale flag must be cleared once the file is readable: %s", resp.Stale)
	}
}

// Only the same content refusing the password over several checks means
// that the master password changed; then the password is forgotten.
func TestKeepOpenForgetsPasswordRefusedRepeatedly(t *testing.T) {
	s, path := openDatabase(t)
	getSnapshot(t, s, path)

	if err := os.WriteFile(path, []byte("not a database any more"), 0o600); err != nil {
		t.Fatalf("cannot overwrite database: %v", err)
	}

	for check := 1; check < wrongPasswordChecks; check++ {
		if v, _ := getSnapshot(t, s, path); v == nil {
			t.Fatalf("the password must survive check %d", check)
		}
	}
	if v, resp := getSnapshot(t, s, path); v != nil || resp.Found {
		t.Fatalf("after %d refusals of the same content the password must be forgotten", wrongPasswordChecks)
	}
}

func TestKeepOpenDisabledDropsCopy(t *testing.T) {
	s, path := openDatabase(t)
	getSnapshot(t, s, path)

	resp, _ := s.process(request{Op: opGet, Database: path, KeepOpen: false})
	if !resp.Found || resp.Password != vaulttest.Password || resp.Snapshot != nil {
		t.Fatalf("without keep_open only the password must be returned: %+v", resp.Found)
	}
	if s.passwords[path].snapshot != nil {
		t.Fatalf("turning keep_open off must drop the open copy")
	}
}
