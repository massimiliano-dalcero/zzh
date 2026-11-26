// Package approval remembers, for a limited time, that the user allowed a
// program to connect to a host through zzh ("Allow for 15 minutes" in the
// confirmation dialog), so that the same program is not asked again at
// every connection.
//
// An authorization is the pair program + host: a program allowed for
// github.com is asked again before it uses the keys of another host (user's
// choice, see D-061).
//
// The list is kept in a small JSON file in the user's cache folder,
// readable only by the user. It holds no secret (program paths, host names,
// expiry times), so a file is good enough: the daemon may not be running,
// and code running as the user could click the dialog anyway. "zzh lock"
// clears it, together with the passwords.
package approval

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const fileName = "approvals.json"

// entry is one authorization.
type entry struct {
	Program string    `json:"program"`
	Host    string    `json:"host"`
	Until   time.Time `json:"until"`
}

// Store reads and writes the authorizations of one user.
type Store struct {
	dir string
}

// NewStore keeps the authorizations in dir (the zzh cache folder).
func NewStore(dir string) Store {
	return Store{dir: dir}
}

func (s Store) path() string {
	return filepath.Join(s.dir, fileName)
}

// Allowed reports whether program may connect to host at now.
func (s Store) Allowed(program, host string, now time.Time) bool {
	for _, e := range s.read() {
		if e.Program == program && e.Host == host && now.Before(e.Until) {
			return true
		}
	}
	return false
}

// Grant allows program to connect to host until now+duration, replacing an
// older authorization for the same pair and dropping the expired ones.
func (s Store) Grant(program, host string, now time.Time, duration time.Duration) error {
	var kept []entry
	for _, e := range s.read() {
		if now.Before(e.Until) && !(e.Program == program && e.Host == host) {
			kept = append(kept, e)
		}
	}
	kept = append(kept, entry{Program: program, Host: host, Until: now.Add(duration)})
	return s.write(kept)
}

// Clear forgets every authorization.
func (s Store) Clear() error {
	err := os.Remove(s.path())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cannot clear the authorizations: %w", err)
	}
	return nil
}

// read returns the stored authorizations. A missing or damaged file means
// "none": the worst outcome is one more question to the user.
func (s Store) read() []entry {
	data, err := os.ReadFile(s.path())
	if err != nil {
		return nil
	}
	var entries []entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil
	}
	return entries
}

// write replaces the file through a temporary one, so that a concurrent
// reader never sees half a file.
func (s Store) write(entries []entry) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("cannot create %s: %w", s.dir, err)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode the authorizations: %w", err)
	}
	temporary := s.path() + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return fmt.Errorf("cannot save the authorizations: %w", err)
	}
	if err := os.Rename(temporary, s.path()); err != nil {
		os.Remove(temporary)
		return fmt.Errorf("cannot save the authorizations: %w", err)
	}
	return nil
}
