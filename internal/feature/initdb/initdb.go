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

// Run creates the database and the bookmark.
//
// Everything that can be checked cheaply (arguments, config file, name and
// path availability) is checked before asking for the password, so that the
// user does not type a new master password twice only to hit a trivial error.
func Run(args []string, loaded config.Loaded, loadErr error, askPassword PasswordReader, out io.Writer) error {
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
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists: to use it, run \"zzh db add %s %s\"", path, name, path)
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
