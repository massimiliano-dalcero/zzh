package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"zzh/internal/core/localipc"
)

// Client talks to the daemon whose endpoint file is in dir.
type Client struct {
	dir string
}

// NewClient returns a client for the daemon of dir (see DefaultDir).
func NewClient(dir string) *Client {
	return &Client{dir: dir}
}

// call sends one request and waits for the answer. Any failure to find or
// reach the daemon is ErrNotRunning: a stale endpoint file left by a daemon
// that died is just "not running".
func (c *Client) call(req request) (response, error) {
	info, err := readEndpoint(c.dir)
	if err != nil {
		return response{}, ErrNotRunning
	}

	conn, err := localipc.Dial(info.Endpoint, 2*time.Second)
	if err != nil {
		return response{}, ErrNotRunning
	}
	defer conn.Close()

	// A deadline protects the command from a daemon that accepted the
	// connection but hangs: requests are answered in microseconds.
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return response{}, fmt.Errorf("cannot talk to the zzh daemon: %w", err)
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return response{}, fmt.Errorf("cannot talk to the zzh daemon: %w", err)
	}
	var resp response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return response{}, fmt.Errorf("no answer from the zzh daemon: %w", err)
	}
	if resp.Error != "" {
		return response{}, errors.New("zzh daemon: " + resp.Error)
	}
	return resp, nil
}

// Kept is what the daemon holds for a database.
type Kept struct {
	Password string

	// Snapshot and SnapshotKey are the open database, present only with
	// keep_open (see vault.OpenSnapshot); Stale, when not empty, explains
	// why the file on disk could not be loaded and this copy is older.
	Snapshot    []byte
	SnapshotKey string
	Stale       string
}

// Get returns what the daemon keeps for database. found is false when the
// daemon is not running or the database is locked (never unlocked or
// expired); err is set only for unexpected failures. keepOpen asks for the
// open database too.
func (c *Client) Get(database string, policy Policy, keepOpen bool) (kept Kept, found bool, err error) {
	resp, err := c.call(request{Op: opGet, Database: database, Policy: policy, KeepOpen: keepOpen})
	if errors.Is(err, ErrNotRunning) {
		return Kept{}, false, nil
	}
	if err != nil {
		return Kept{}, false, err
	}
	return Kept{Password: resp.Password, Snapshot: resp.Snapshot, SnapshotKey: resp.SnapshotKey, Stale: resp.Stale}, resp.Found, nil
}

// Put hands a verified password to the daemon; with keepOpen the daemon also
// opens the database, in the background.
func (c *Client) Put(database, password string, policy Policy, keepOpen bool) error {
	_, err := c.call(request{Op: opPut, Database: database, Password: password, Policy: policy, KeepOpen: keepOpen})
	return err
}

// Lock forgets the password of database, or of every database when
// database is empty. A daemon that is not running has nothing to forget.
func (c *Client) Lock(database string) error {
	_, err := c.call(request{Op: opLock, Database: database})
	if errors.Is(err, ErrNotRunning) {
		return nil
	}
	return err
}

// Status lists the unlocked databases; ErrNotRunning when there is no daemon.
func (c *Client) Status() ([]Unlocked, error) {
	resp, err := c.call(request{Op: opStatus})
	return resp.Unlocked, err
}

// Stop ends the daemon, forgetting every password.
func (c *Client) Stop() error {
	_, err := c.call(request{Op: opStop})
	if errors.Is(err, ErrNotRunning) {
		return nil
	}
	return err
}

// EnsureRunning starts "<executable> daemon run" in the background unless a
// daemon already answers, and waits until it is ready.
//
// The process is detached from the terminal (see detach): closing the
// terminal does not stop it, and it holds no console or window.
func (c *Client) EnsureRunning(executable string) error {
	if _, err := c.Status(); err == nil {
		return nil
	}

	command := exec.Command(executable, "daemon", "run")
	command.Env = append(os.Environ(), EnvDir+"="+c.dir)
	detach(command)
	if err := command.Start(); err != nil {
		return fmt.Errorf("cannot start the zzh daemon: %w", err)
	}
	// The daemon lives on its own; this process must not wait for it.
	if err := command.Process.Release(); err != nil {
		return fmt.Errorf("cannot detach the zzh daemon: %w", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := c.Status(); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("the zzh daemon did not start in time")
}
