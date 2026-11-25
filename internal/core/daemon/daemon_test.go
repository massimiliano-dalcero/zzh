package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeClock lets the tests move time forward without waiting.
type fakeClock struct{ current time.Time }

func (c *fakeClock) now() time.Time             { return c.current }
func (c *fakeClock) advance(step time.Duration) { c.current = c.current.Add(step) }

func newTestServer() (*server, *fakeClock) {
	clock := &fakeClock{current: time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)}
	return newServer(clock.now), clock
}

func get(s *server, database string, policy Policy) (string, bool) {
	resp, _ := s.process(request{Op: opGet, Database: database, Policy: policy})
	return resp.Password, resp.Found
}

func TestIdleTimeoutIsResetByUse(t *testing.T) {
	s, clock := newTestServer()
	policy := Policy{IdleTimeout: 15 * time.Minute}
	s.process(request{Op: opPut, Database: "a.kdbx", Password: "pw", Policy: policy})

	// Used every 10 minutes: never idle for 15.
	for i := 0; i < 5; i++ {
		clock.advance(10 * time.Minute)
		if password, found := get(s, "a.kdbx", policy); !found || password != "pw" {
			t.Fatalf("password lost after %d uses", i+1)
		}
	}

	clock.advance(15 * time.Minute)
	if _, found := get(s, "a.kdbx", policy); found {
		t.Fatalf("password must expire after 15 minutes of inactivity")
	}
}

func TestMaxLifetimeWinsOverActivity(t *testing.T) {
	s, clock := newTestServer()
	policy := Policy{IdleTimeout: time.Hour, MaxLifetime: 2 * time.Hour}
	s.process(request{Op: opPut, Database: "a.kdbx", Password: "pw", Policy: policy})

	for i := 0; i < 3; i++ {
		clock.advance(30 * time.Minute)
		get(s, "a.kdbx", policy)
	}
	clock.advance(31 * time.Minute)
	if _, found := get(s, "a.kdbx", policy); found {
		t.Fatalf("password must expire after the maximum lifetime, even if used")
	}
}

// Zero disables a limit; and the policy sent with each request replaces
// the stored one, so config changes apply at once.
func TestZeroDisablesAndPolicyUpdates(t *testing.T) {
	s, clock := newTestServer()
	s.process(request{Op: opPut, Database: "a.kdbx", Password: "pw", Policy: Policy{IdleTimeout: time.Minute}})

	never := Policy{}
	clock.advance(30 * time.Second)
	get(s, "a.kdbx", never)
	clock.advance(1000 * time.Hour)
	if _, found := get(s, "a.kdbx", never); !found {
		t.Fatalf("with both limits at 0 the password must never expire")
	}

	// Shortening the timeout below the time already elapsed expires it.
	clock.advance(10 * time.Minute)
	if _, found := get(s, "a.kdbx", Policy{IdleTimeout: 5 * time.Minute}); found {
		t.Fatalf("a shorter timeout must apply immediately")
	}
}

func TestLockAndStatus(t *testing.T) {
	s, _ := newTestServer()
	s.process(request{Op: opPut, Database: "a.kdbx", Password: "1"})
	s.process(request{Op: opPut, Database: "b.kdbx", Password: "2"})

	resp, _ := s.process(request{Op: opStatus})
	if len(resp.Unlocked) != 2 || resp.Unlocked[0].Database != "a.kdbx" {
		t.Fatalf("unexpected status: %+v", resp.Unlocked)
	}
	for _, unlocked := range resp.Unlocked {
		if unlocked.Database == "" || !unlocked.UnlockedAt.Equal(unlocked.LastUsed) {
			t.Fatalf("status must not expose passwords and must carry times: %+v", unlocked)
		}
	}

	if _, stop := s.process(request{Op: opLock, Database: "a.kdbx"}); stop {
		t.Fatalf("the daemon must keep running while a password is left")
	}
	if _, found := get(s, "a.kdbx", Policy{}); found {
		t.Fatalf("a.kdbx must be locked")
	}
	if _, stop := s.process(request{Op: opLock}); !stop {
		t.Fatalf("locking everything must stop the daemon")
	}
}

func TestExpiresAt(t *testing.T) {
	base := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	unlocked := Unlocked{UnlockedAt: base, LastUsed: base.Add(time.Hour), Policy: Policy{IdleTimeout: 15 * time.Minute, MaxLifetime: 8 * time.Hour}}

	expiry, expires := unlocked.ExpiresAt()
	if !expires || !expiry.Equal(base.Add(time.Hour+15*time.Minute)) {
		t.Fatalf("unexpected expiry %v", expiry)
	}
	if _, expires := (Unlocked{}).ExpiresAt(); expires {
		t.Fatalf("without limits there is no expiry")
	}
}

// The real daemon loop, served in-process, reached through the endpoint
// file like a separate process would be.
func TestRunAndClient(t *testing.T) {
	dir := t.TempDir()
	client := NewClient(dir)

	if _, err := client.Status(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("expected ErrNotRunning before start, got %v", err)
	}
	if _, found, err := client.Get("a.kdbx", Policy{}, false); found || err != nil {
		t.Fatalf("Get without daemon must simply find nothing: %v", err)
	}

	finished := make(chan error, 1)
	go func() { finished <- Run(dir, os.Getpid()) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := client.Status(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := client.Put("a.kdbx", "pw", Policy{}, false); err != nil {
		t.Fatalf("put failed: %v", err)
	}
	kept, found, err := client.Get("a.kdbx", Policy{}, false)
	if err != nil || !found || kept.Password != "pw" {
		t.Fatalf("get failed: %q %v %v", kept.Password, found, err)
	}

	// Locking the last database ends the daemon and removes its file.
	if err := client.Lock(""); err != nil {
		t.Fatalf("lock failed: %v", err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("daemon ended with error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("daemon did not stop after the last lock")
	}
	if _, err := os.Stat(filepath.Join(dir, endpointFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the endpoint file must be removed when the daemon stops")
	}
}

// A file left by a daemon that crashed must not confuse the clients.
func TestStaleEndpointFile(t *testing.T) {
	dir := t.TempDir()
	if err := writeEndpoint(dir, endpointInfo{Endpoint: `\\.\pipe\zzh-daemon-gone`, PID: 1}); err != nil {
		t.Fatalf("cannot write endpoint: %v", err)
	}
	if _, err := NewClient(dir).Status(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("a stale endpoint must read as not running, got %v", err)
	}
}
