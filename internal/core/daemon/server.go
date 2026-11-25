package daemon

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net"
	"os"
	"sort"
	"sync"
	"time"

	"zzh/internal/core/localipc"
	"zzh/internal/core/vault"
)

// How often expired passwords are purged and open databases checked for
// changes even if nobody asks, and how long a freshly started daemon waits
// for its first password before giving up (it is started by a command that
// then sends one).
const (
	checkInterval  = 5 * time.Second
	startupTimeout = 30 * time.Second

	// wrongPasswordChecks is how many times the same file content must
	// refuse the kept password before the daemon forgets it. A file being
	// written by a sync client cannot be decrypted either, and the KDBX
	// format cannot tell "corrupted" from "wrong password"; only a content
	// that stays the same over several checks while still refusing the
	// password means the master password really changed.
	wrongPasswordChecks = 3
)

// entry is one password kept in memory, with the open database when
// keep_open is enabled.
type entry struct {
	password   string
	unlockedAt time.Time
	lastUsed   time.Time
	policy     Policy

	keepOpen bool

	// snapshot and snapshotKey are the database ready to be handed to a
	// command (see vault.ExportSnapshot); fileHash is the SHA-256 of the
	// file content they were made from.
	snapshot    []byte
	snapshotKey string
	fileHash    [sha256.Size]byte

	// unsupported is set for databases that cannot be kept open (KDBX 3):
	// the daemon then only keeps the password, without retrying each time.
	unsupported bool

	// stale explains why the current file could not be loaded; the last
	// good snapshot is still served meanwhile.
	stale string

	// failedHash and failures count the checks in which the same content
	// refused the password (see wrongPasswordChecks).
	failedHash [sha256.Size]byte
	failures   int
}

func (e entry) expired(now time.Time) bool {
	if e.policy.IdleTimeout > 0 && now.Sub(e.lastUsed) >= e.policy.IdleTimeout {
		return true
	}
	return e.policy.MaxLifetime > 0 && now.Sub(e.unlockedAt) >= e.policy.MaxLifetime
}

// server is the daemon state. All access goes through the mutex: requests
// are served concurrently.
type server struct {
	mutex     sync.Mutex
	passwords map[string]*entry
	everUsed  bool
	now       func() time.Time
	done      chan struct{}
	closeOnce sync.Once
}

func newServer(now func() time.Time) *server {
	return &server{passwords: map[string]*entry{}, now: now, done: make(chan struct{})}
}

// Run is the body of "zzh daemon run": it listens, publishes the endpoint
// file in dir and serves until there is nothing left to keep.
//
// If another daemon already answers, Run returns at once: two commands may
// try to start the daemon at the same moment, and one is enough.
func Run(dir string, pid int) error {
	if _, err := NewClient(dir).Status(); err == nil {
		return nil
	}

	endpoint, err := localipc.Listen("daemon")
	if err != nil {
		return err
	}
	defer endpoint.Close()

	if err := writeEndpoint(dir, endpointInfo{Endpoint: endpoint.Path, PID: pid}); err != nil {
		return err
	}
	defer removeEndpoint(dir, endpoint.Path)

	s := newServer(time.Now)
	go s.purgeLoop()
	go s.serve(endpoint.Listener)

	<-s.done
	return nil
}

func (s *server) stop() {
	s.closeOnce.Do(func() { close(s.done) })
}

func (s *server) serve(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			s.stop()
			return
		}
		go s.handle(conn)
	}
}

// purgeLoop forgets expired passwords even when no command arrives, keeps
// the open databases in sync with their files (so that a change made in
// KeePassXC or by a sync client is loaded before the next command needs
// it), and stops the daemon when nothing is left (or when the command that
// started it never sent a password).
func (s *server) purgeLoop() {
	started := s.now()
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.mutex.Lock()
			s.purgeLocked()
			for database, kept := range s.passwords {
				if kept.keepOpen {
					s.refreshLocked(database, kept)
				}
			}
			idle := len(s.passwords) == 0 && (s.everUsed || s.now().Sub(started) > startupTimeout)
			s.mutex.Unlock()
			if idle {
				s.stop()
				return
			}
		}
	}
}

// purgeLocked removes expired passwords; the mutex must be held.
func (s *server) purgeLocked() {
	now := s.now()
	for database, kept := range s.passwords {
		if kept.expired(now) {
			delete(s.passwords, database)
		}
	}
}

func (s *server) handle(conn net.Conn) {
	defer conn.Close()

	var req request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}

	resp, stopAfter := s.process(req)

	// If the client is already gone there is nobody to answer.
	_ = json.NewEncoder(conn).Encode(resp)

	if stopAfter {
		s.stop()
	}
}

// process executes one request. It returns true when the daemon must stop
// afterwards (explicit stop, or nothing left to keep after a lock).
func (s *server) process(req request) (response, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.purgeLocked()
	now := s.now()

	switch req.Op {
	case opGet:
		kept, found := s.passwords[req.Database]
		if !found {
			return response{}, false
		}
		// The current policy from the client replaces the stored one, so a
		// changed timeout applies at once; the password may then turn out
		// to be already expired under the new rules.
		kept.policy = req.Policy
		if kept.expired(now) {
			delete(s.passwords, req.Database)
			return response{}, false
		}
		kept.lastUsed = now
		resp := response{Found: true, Password: kept.password}

		// keep_open travels with the request like the policy, so turning it
		// off in the config drops the open copy at the next command.
		kept.keepOpen = req.KeepOpen
		if !kept.keepOpen {
			kept.snapshot, kept.snapshotKey = nil, ""
			return resp, false
		}

		// Make sure the copy matches the file right now: a command must
		// never see an older version than what is on disk.
		s.refreshLocked(req.Database, kept)
		if _, stillKept := s.passwords[req.Database]; !stillKept {
			// The password was forgotten because it no longer opens the file.
			return response{}, false
		}
		resp.Snapshot, resp.SnapshotKey, resp.Stale = kept.snapshot, kept.snapshotKey, kept.stale
		return resp, false

	case opPut:
		kept := &entry{password: req.Password, unlockedAt: now, lastUsed: now, policy: req.Policy, keepOpen: req.KeepOpen}
		s.passwords[req.Database] = kept
		s.everUsed = true
		if kept.keepOpen {
			// Opening takes about a second: done in the background, so the
			// command that just unlocked is not slowed down.
			go func() {
				s.mutex.Lock()
				defer s.mutex.Unlock()
				if s.passwords[req.Database] == kept {
					s.refreshLocked(req.Database, kept)
				}
			}()
		}
		return response{}, false

	case opLock:
		if req.Database == "" {
			s.passwords = map[string]*entry{}
		} else {
			delete(s.passwords, req.Database)
		}
		return response{}, len(s.passwords) == 0

	case opStatus:
		return response{Unlocked: s.statusLocked()}, false

	case opStop:
		s.passwords = map[string]*entry{}
		return response{}, true

	default:
		return response{Error: "unknown operation " + req.Op}, false
	}
}

// refreshLocked brings the open copy of a database in line with its file;
// the mutex must be held (D-034).
//
// The file content is read and fingerprinted on every call: reading a few
// hundred KB and hashing them takes about a millisecond, and unlike the
// modification time the content cannot lie (sync clients rewrite times).
// Only when the content changed is the file decrypted again, which costs
// the real key derivation.
func (s *server) refreshLocked(database string, kept *entry) {
	data, err := os.ReadFile(database)
	if err != nil {
		kept.stale = "the database file cannot be read: " + err.Error()
		return
	}

	hash := sha256.Sum256(data)
	if hash == kept.fileHash && (kept.snapshot != nil || kept.unsupported) {
		kept.stale = ""
		return
	}

	v, err := vault.OpenBytes(data, database, kept.password, "")
	if err != nil {
		if errors.Is(err, vault.ErrWrongPassword) {
			if hash == kept.failedHash {
				kept.failures++
			} else {
				kept.failedHash, kept.failures = hash, 1
			}
			if kept.failures >= wrongPasswordChecks {
				delete(s.passwords, database)
				return
			}
		}
		// Keep serving the last good copy: the file may be half written.
		kept.stale = "the database file cannot be opened right now (sync in progress?): " + err.Error()
		return
	}

	kept.failures = 0
	snapshot, key, err := v.ExportSnapshot()
	if errors.Is(err, vault.ErrSnapshotUnsupported) {
		kept.unsupported, kept.fileHash, kept.stale = true, hash, ""
		kept.snapshot, kept.snapshotKey = nil, ""
		return
	}
	if err != nil {
		kept.stale = "cannot prepare the database copy: " + err.Error()
		return
	}

	kept.snapshot, kept.snapshotKey, kept.fileHash, kept.stale = snapshot, key, hash, ""
}

func (s *server) statusLocked() []Unlocked {
	list := make([]Unlocked, 0, len(s.passwords))
	for database, kept := range s.passwords {
		list = append(list, Unlocked{Database: database, UnlockedAt: kept.unlockedAt, LastUsed: kept.lastUsed, Policy: kept.policy,
			Open: kept.snapshot != nil, Stale: kept.stale})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Database < list[j].Database })
	return list
}
