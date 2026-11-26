// Package session provides the unlocked vault to the commands that need it.
//
// The master password comes, in order, from ZZH_PASSWORD, from the zzh
// daemon (which keeps it in memory after the first use) or from the
// terminal; with daemon.autostart disabled a locked database is an error
// asking for "zzh unlock" (D-033).
package session

import (
	"errors"
	"fmt"
	"os"

	"zzh/internal/core/config"
	"zzh/internal/core/daemon"
	"zzh/internal/core/prompt"
	"zzh/internal/core/shim"
	"zzh/internal/core/style"
	"zzh/internal/core/vault"
)

// maxSaveAttempts bounds the retries when the database keeps changing on
// disk while zzh is trying to save (e.g. KeePassXC auto-saving repeatedly).
const maxSaveAttempts = 3

// ErrLocked is returned when the database is locked and zzh is not allowed
// to ask for the password (daemon.autostart is false).
var ErrLocked = errors.New("the database is locked")

// source tells where a master password came from, which decides what to do
// after using it (remember it, or explain a failure).
type source int

const (
	fromTerminal source = iota
	fromEnvironment
	fromDaemon
)

// credentials is a resolved database with its master password and, when
// the daemon keeps the database open, a ready-to-use copy of it.
type credentials struct {
	database config.ResolvedDatabase
	password string
	source   source
	kept     daemon.Kept
}

// OpenVault opens the selected database for reading.
//
// With daemon.keep_open the daemon provides the database already decrypted
// and in line with the file, so this takes milliseconds instead of a
// second; the returned vault is then read-only, which is fine because every
// write goes through Modify (D-034). Without the daemon, or if the copy
// cannot be used, the file is opened directly as before.
//
// databaseName is the value of the global "--db" flag; empty selects the
// default bookmark (see config.Resolve).
func OpenVault(cfg config.Config, databaseName string) (*vault.Vault, error) {
	creds, err := obtain(cfg, databaseName)
	if err != nil {
		return nil, err
	}

	if creds.kept.Snapshot != nil {
		if creds.kept.Stale != "" {
			style.PrintNotice(os.Stderr, "warning, showing the last readable version: "+creds.kept.Stale)
		}
		v, err := vault.OpenSnapshot(creds.kept.Snapshot, creds.kept.SnapshotKey, creds.database.Path, creds.database.RootGroup)
		if err == nil {
			return v, nil
		}
		// A copy that cannot be opened is not worth failing for: the file
		// is opened directly below, with the same password.
		style.PrintNotice(os.Stderr, err.Error())
	}

	v, err := vault.Open(creds.database.Path, creds.password, creds.database.RootGroup)
	return v, creds.afterUse(cfg, err)
}

// Modify opens the selected database, applies change and saves the result.
//
// If the file is modified by another program between opening and saving
// (vault.ErrModifiedExternally), the database is opened again and change is
// applied again on the fresh content, so that the other program's changes
// are preserved. change must therefore only depend on the vault it receives:
// anything expensive or random (like generating a key) must be prepared
// before calling Modify, so that every attempt stores the same data.
func Modify(cfg config.Config, databaseName string, change func(v *vault.Vault) error) error {
	creds, err := obtain(cfg, databaseName)
	if err != nil {
		return err
	}
	err = modifyUnlocked(creds.database, creds.password, cfg.BackupOnWrite, change)
	return creds.afterUse(cfg, err)
}

// modifyUnlocked is the open/change/save loop of Modify, separated from the
// password prompt so that it can be tested without a terminal.
func modifyUnlocked(database config.ResolvedDatabase, password string, backup bool, change func(v *vault.Vault) error) error {
	for attempt := 1; ; attempt++ {
		v, err := vault.Open(database.Path, password, database.RootGroup)
		if err != nil {
			return err
		}

		if err := change(v); err != nil {
			return err
		}

		err = v.Save(backup)
		if errors.Is(err, vault.ErrModifiedExternally) && attempt < maxSaveAttempts {
			continue
		}
		return err
	}
}

// obtain resolves the database and finds its master password.
func obtain(cfg config.Config, databaseName string) (credentials, error) {
	database, err := cfg.Resolve(databaseName)
	if err != nil {
		return credentials{}, err
	}

	if environmentPassword != "" {
		if cfg.WarnEnvPassword {
			style.PrintNotice(os.Stderr, fmt.Sprintf("using the master password from %s (disable this notice with \"zzh config set warn_env_password false\")", EnvPassword))
		}
		return credentials{database: database, password: environmentPassword, source: fromEnvironment}, nil
	}

	if client, err := daemonClient(); err == nil {
		kept, found, err := client.Get(database.Path, policy(cfg), cfg.Daemon.KeepOpen)
		if err != nil {
			return credentials{}, err
		}
		if found {
			return credentials{database: database, password: kept.Password, source: fromDaemon, kept: kept}, nil
		}
	}

	if !cfg.Daemon.Autostart {
		return credentials{}, fmt.Errorf("%w: run \"zzh unlock\" (automatic unlock is disabled by daemon.autostart)", ErrLocked)
	}

	password, err := askPassword(database)
	if errors.Is(err, prompt.ErrNoTerminal) {
		// Scripts, scheduled tasks, programs started without a visible
		// console (e.g. git run by an editor): nobody can type the password.
		// git run from a terminal is not this case: the question goes to
		// the controlling terminal (see the prompt package).
		return credentials{}, fmt.Errorf("%w and there is no terminal to ask the password: run \"zzh unlock\" first", ErrLocked)
	}
	if err != nil {
		return credentials{}, err
	}
	return credentials{database: database, password: password, source: fromTerminal}, nil
}

// afterUse runs once the password has been tried: a password typed on the
// terminal and accepted is handed to the daemon (starting it if needed), so
// the next commands do not ask again; a rejected password is explained
// according to where it came from.
func (c credentials) afterUse(cfg config.Config, err error) error {
	if err == nil {
		if c.source == fromTerminal && cfg.Daemon.Autostart {
			if rememberErr := remember(cfg, c.database.Path, c.password); rememberErr != nil {
				// The command itself succeeded: the daemon is a comfort, so
				// its failure is reported but does not fail the command.
				style.PrintNotice(os.Stderr, "the password will be asked again: "+rememberErr.Error())
			}
		}
		return nil
	}

	if !errors.Is(err, vault.ErrWrongPassword) {
		return err
	}

	switch c.source {
	case fromEnvironment:
		// A stale variable set long ago is an easy cause to overlook, and with
		// several bookmarked databases it may simply belong to another one.
		return fmt.Errorf("%w (the password was taken from %s)", err, EnvPassword)

	case fromDaemon:
		// The master password was changed (e.g. in KeePassXC) after the
		// unlock: the kept one is useless, forget it.
		if client, clientErr := daemonClient(); clientErr == nil {
			// Best effort: if the daemon is gone there is nothing to forget.
			_ = client.Lock(c.database.Path)
		}
		return fmt.Errorf("%w (the password kept by the zzh daemon is no longer valid: run \"zzh unlock\")", err)
	}
	return err
}

// Unlock asks the master password of the selected database, verifies it by
// opening the database and hands it to the daemon, starting it if needed.
// It works whatever daemon.autostart says: it is the explicit way to unlock.
func Unlock(cfg config.Config, databaseName string) (config.ResolvedDatabase, error) {
	database, err := cfg.Resolve(databaseName)
	if err != nil {
		return database, err
	}

	password := environmentPassword
	if password == "" {
		password, err = askPassword(database)
		if err != nil {
			return database, err
		}
	}

	if _, err := vault.Open(database.Path, password, database.RootGroup); err != nil {
		return database, err
	}
	return database, remember(cfg, database.Path, password)
}

// Lock makes the daemon forget the selected database, or every database
// when all is true.
func Lock(cfg config.Config, databaseName string, all bool) error {
	client, err := daemonClient()
	if err != nil {
		return err
	}
	if all {
		return client.Lock("")
	}
	database, err := cfg.Resolve(databaseName)
	if err != nil {
		return err
	}
	return client.Lock(database.Path)
}

// Status lists the databases the daemon keeps unlocked; daemon.ErrNotRunning
// when there is no daemon.
func Status() ([]daemon.Unlocked, error) {
	client, err := daemonClient()
	if err != nil {
		return nil, err
	}
	return client.Status()
}

// StopDaemon ends the daemon, forgetting every password.
func StopDaemon() error {
	client, err := daemonClient()
	if err != nil {
		return err
	}
	return client.Stop()
}

func remember(cfg config.Config, path, password string) error {
	client, err := daemonClient()
	if err != nil {
		return err
	}
	// The daemon is started from the zzh file itself, never from a shim
	// link: started as "ssh", it would read "daemon run" as a host name.
	executable, err := shim.Executable()
	if err != nil {
		return err
	}
	if err := client.EnsureRunning(executable); err != nil {
		return err
	}
	return client.Put(path, password, policy(cfg), cfg.Daemon.KeepOpen)
}

func daemonClient() (*daemon.Client, error) {
	dir, err := daemon.DefaultDir()
	if err != nil {
		return nil, err
	}
	return daemon.NewClient(dir), nil
}

func policy(cfg config.Config) daemon.Policy {
	return daemon.Policy{IdleTimeout: cfg.Lock.IdleTimeout.Std(), MaxLifetime: cfg.Lock.MaxLifetime.Std()}
}

// passwordPrompt asks the master password; a variable so that tests can
// simulate the absence of a terminal even when they run from a console,
// where the real prompt would find it and wait for an answer.
var passwordPrompt = prompt.Password

func askPassword(database config.ResolvedDatabase) (string, error) {
	return passwordPrompt(fmt.Sprintf("KeePass password for %q (%s): ", database.Name, database.Path))
}
