// Package initdb implements "zzh init <name> <path>": it creates a new
// KeePass database with the zzh layout and bookmarks it in the config file.
package initdb

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"zzh/internal/core/config"
	"zzh/internal/core/vault"
)

// PasswordReader asks the user for a new master password (with
// confirmation). It is a parameter so that tests can run without a terminal.
type PasswordReader func(message string) (string, error)

// Existing handles a path that already holds a file (D-066): instead of
// refusing, init offers to use it as "zzh db add" does. Both functions are
// provided by the caller: Confirm asks a yes/no question (prompt.Confirm),
// Add bookmarks the existing database and creates the zzh groups if they
// are missing ("zzh db add"; feature modules do not call each other).
type Existing struct {
	Confirm func(question string) (bool, error)
	Add     func(name, path string) error
}

// Run creates the database and the bookmark.
//
// Everything that can be checked cheaply (arguments, config file, name and
// path availability) is checked before asking for the password, so that the
// user does not type a new master password twice only to hit a trivial error.
func Run(args []string, loaded config.Loaded, loadErr error, askPassword PasswordReader, existing Existing, out io.Writer) error {
	if len(args) != 2 {
		return errors.New("usage: zzh init <name> <path.kdbx>")
	}
	name, rawPath := args[0], args[1]

	// A broken config file must not be overwritten with the new bookmark:
	// the user's other settings would be lost.
	if loadErr != nil {
		return loadErr
	}
	cfg := loaded.Config

	if err := config.ValidateDatabaseName(name); err != nil {
		return err
	}
	if _, exists := cfg.Databases[name]; exists {
		return fmt.Errorf("database %q is already bookmarked (see \"zzh db ls\")", name)
	}

	path, err := filepath.Abs(config.ExpandPath(rawPath))
	if err != nil {
		return fmt.Errorf("invalid path %q: %w", rawPath, err)
	}
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return fmt.Errorf("%s is a folder, not a database file", path)
		}
		// The file is never overwritten. With the user's consent it is used
		// as it is: bookmarked, and given the zzh groups if it has none.
		allowed, err := existing.Confirm(fmt.Sprintf("%s already exists. Use it as %q, adding the zzh groups (SSH/Hosts, SSH/Keys) if they are missing?", path, name))
		if err != nil {
			return err
		}
		if !allowed {
			fmt.Fprintln(out, "Nothing done: the existing file was left as it is.")
			return nil
		}
		return existing.Add(name, path)
	}

	password, err := askPassword(fmt.Sprintf("New master password for %s: ", path))
	if err != nil {
		return err
	}

	rootGroup := cfg.RootGroup
	fmt.Fprintln(out, "Creating the database (the key derivation is tuned to take about one second)...")

	if err := vault.Create(path, password, name, rootGroup); err != nil {
		return err
	}

	if err := cfg.AddDatabase(name, path); err != nil {
		return fmt.Errorf("database created at %s but not bookmarked: %w", path, err)
	}
	if err := config.Save(loaded.Path, cfg); err != nil {
		return fmt.Errorf("database created at %s but the config file could not be updated: %w", path, err)
	}

	fmt.Fprintf(out, "Created %s with the groups %s/Hosts and %s/Keys\n", path, rootGroup, rootGroup)
	fmt.Fprintf(out, "Bookmarked as %q in %s\n", name, loaded.Path)
	if cfg.DefaultDatabase == name {
		fmt.Fprintln(out, "It is the default database.")
	} else {
		fmt.Fprintf(out, "Use it with \"zzh --db %s ...\" or make it the default with \"zzh db default %s\".\n", name, name)
	}
	return nil
}
