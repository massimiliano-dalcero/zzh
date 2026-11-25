// Package daemon keeps master passwords in memory between zzh commands, so
// that the user types them once (D-004, D-033).
//
// The daemon is a "zzh daemon run" process started in the background. It
// listens on a private local endpoint (see localipc) whose random path is
// written to an endpoint file in the user's cache directory: clients read
// the file to find the daemon. Since the endpoint name is random and only
// the user can read the file, another account cannot pre-create the
// endpoint to intercept the passwords.
//
// The daemon stores only passwords, keyed by database path. Commands still
// open the database themselves, so all the KeePass logic stays in one place.
// Every password expires after the configured inactivity time and maximum
// lifetime (0 disables a limit); when the last one is gone the daemon exits.
package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// EnvDir overrides the directory of the endpoint file, like ZZH_CONFIG does
// for the config file; mainly for tests.
const EnvDir = "ZZH_DAEMON_DIR"

const endpointFileName = "daemon.json"

// ErrNotRunning is returned by the client when no daemon answers.
var ErrNotRunning = errors.New("the zzh daemon is not running")

// Policy is how long a password is kept. A zero duration disables the limit.
type Policy struct {
	IdleTimeout time.Duration `json:"idle_timeout"`
	MaxLifetime time.Duration `json:"max_lifetime"`
}

// Unlocked describes a database whose password the daemon holds.
type Unlocked struct {
	Database   string    `json:"database"`
	UnlockedAt time.Time `json:"unlocked_at"`
	LastUsed   time.Time `json:"last_used"`
	Policy     Policy    `json:"policy"`

	// Open is true when the database is kept decrypted (keep_open); Stale
	// explains why the file on disk currently cannot be loaded.
	Open  bool   `json:"open,omitempty"`
	Stale string `json:"stale,omitempty"`
}

// ExpiresAt returns when the password will be forgotten if not used again,
// and false when it never expires.
func (u Unlocked) ExpiresAt() (time.Time, bool) {
	var expiry time.Time
	if u.Policy.IdleTimeout > 0 {
		expiry = u.LastUsed.Add(u.Policy.IdleTimeout)
	}
	if u.Policy.MaxLifetime > 0 {
		limit := u.UnlockedAt.Add(u.Policy.MaxLifetime)
		if expiry.IsZero() || limit.Before(expiry) {
			expiry = limit
		}
	}
	return expiry, !expiry.IsZero()
}

// Operations understood by the daemon.
const (
	opGet    = "get"
	opPut    = "put"
	opLock   = "lock"
	opStatus = "status"
	opStop   = "stop"
)

// request is sent by the client, one per connection.
type request struct {
	Op       string `json:"op"`
	Database string `json:"database,omitempty"`
	Password string `json:"password,omitempty"`

	// Policy travels with get and put, so that a change of the timeouts in
	// the config file applies at the next command, without a new unlock.
	Policy Policy `json:"policy"`

	// KeepOpen asks the daemon to keep the database open and to return a
	// ready-to-use copy with the password (daemon.keep_open, D-034).
	KeepOpen bool `json:"keep_open,omitempty"`
}

type response struct {
	Error    string `json:"error,omitempty"`
	Found    bool   `json:"found,omitempty"`
	Password string `json:"password,omitempty"`

	// Snapshot and SnapshotKey are the open database (see
	// vault.ExportSnapshot); Stale explains why the file on disk could not
	// be loaded and an older copy is being served.
	Snapshot    []byte `json:"snapshot,omitempty"`
	SnapshotKey string `json:"snapshot_key,omitempty"`
	Stale       string `json:"stale,omitempty"`

	Unlocked []Unlocked `json:"unlocked,omitempty"`
}

// endpointInfo is the content of the endpoint file.
type endpointInfo struct {
	Endpoint string `json:"endpoint"`
	PID      int    `json:"pid"`
}

// DefaultDir returns where the endpoint file lives: ZZH_DAEMON_DIR, or the
// per-user cache directory (%LOCALAPPDATA%\zzh on Windows, ~/.cache/zzh on
// Linux, ~/Library/Caches/zzh on macOS), which only the user can read.
func DefaultDir() (string, error) {
	if dir := os.Getenv(EnvDir); dir != "" {
		return dir, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine the user cache directory: %w", err)
	}
	return filepath.Join(base, "zzh"), nil
}

func readEndpoint(dir string) (endpointInfo, error) {
	var info endpointInfo
	data, err := os.ReadFile(filepath.Join(dir, endpointFileName))
	if err != nil {
		return info, err
	}
	err = json.Unmarshal(data, &info)
	return info, err
}

// writeEndpoint publishes the endpoint atomically (temporary file + rename),
// so that a client never reads a half-written file.
func writeEndpoint(dir string, info endpointInfo) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}

	temporary, err := os.CreateTemp(dir, ".daemon-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot write the endpoint file: %w", err)
	}
	_, writeErr := temporary.Write(data)
	closeErr := temporary.Close()
	if writeErr == nil {
		writeErr = closeErr
	}
	if writeErr == nil {
		writeErr = os.Rename(temporary.Name(), filepath.Join(dir, endpointFileName))
	}
	if writeErr != nil {
		// Best effort: an unused temporary file is harmless.
		_ = os.Remove(temporary.Name())
		return fmt.Errorf("cannot write the endpoint file: %w", writeErr)
	}
	return nil
}

// removeEndpoint deletes the endpoint file, but only if it still names the
// given endpoint: a newer daemon may have replaced it in the meantime.
func removeEndpoint(dir, endpoint string) {
	info, err := readEndpoint(dir)
	if err != nil || info.Endpoint != endpoint {
		return
	}
	// Best effort: a stale file is detected by clients (connection fails).
	_ = os.Remove(filepath.Join(dir, endpointFileName))
}
