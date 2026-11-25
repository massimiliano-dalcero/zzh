// Package dbcmd implements "zzh db ls|add|rm|default": the management of
// the bookmarked KeePass databases.
package dbcmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"zzh/internal/core/config"
)

const usage = "usage: zzh db ls | add <name> <path> | rm <name> | default <name>"

// Run executes a db subcommand. Commands that change the bookmarks save the
// config file immediately.
func Run(args []string, loaded config.Loaded, loadErr error, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}

	// A broken config file must not be rewritten: the user's settings
	// would be lost. It must be fixed by hand first.
	if loadErr != nil {
		return loadErr
	}
	cfg := loaded.Config

	action, params := args[0], args[1:]

	switch {
	case action == "ls" && len(params) == 0:
		return list(cfg, out)

	case action == "add" && len(params) == 2:
		if err := checkDatabaseFile(params[1]); err != nil {
			return err
		}
		if err := cfg.AddDatabase(params[0], params[1]); err != nil {
			return err
		}
		if err := config.Save(loaded.Path, cfg); err != nil {
			return err
		}
		fmt.Fprintf(out, "Bookmarked %q -> %s\n", params[0], cfg.Databases[params[0]].Path)
		if cfg.DefaultDatabase == params[0] {
			fmt.Fprintln(out, "It is the default database.")
		}
		return nil

	case action == "rm" && len(params) == 1:
		wasDefault := cfg.DefaultDatabase == params[0]
		if err := cfg.RemoveDatabase(params[0]); err != nil {
			return err
		}
		if err := config.Save(loaded.Path, cfg); err != nil {
			return err
		}
		fmt.Fprintf(out, "Removed the bookmark %q (the file was not touched)\n", params[0])
		if wasDefault && len(cfg.Databases) > 1 {
			fmt.Fprintln(out, "It was the default: choose a new one with \"zzh db default <name>\".")
		}
		return nil

	case action == "default" && len(params) == 1:
		if err := cfg.SetDefaultDatabase(params[0]); err != nil {
			return err
		}
		if err := config.Save(loaded.Path, cfg); err != nil {
			return err
		}
		fmt.Fprintf(out, "%q is now the default database\n", params[0])
		return nil

	default:
		return errors.New(usage)
	}
}

// list prints the bookmarks, marking the one used without "--db" and
// flagging files that no longer exist (moved or deleted databases).
func list(cfg config.Config, out io.Writer) error {
	if len(cfg.Databases) == 0 {
		fmt.Fprintln(out, `No database bookmarked: use "zzh init <name> <path>" or "zzh db add <name> <path>".`)
		return nil
	}

	// The effective default may be implicit (single bookmark), so it is
	// computed with the same rule the other commands use.
	effectiveDefault := ""
	if resolved, err := cfg.Resolve(""); err == nil {
		effectiveDefault = resolved.Name
	}

	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "DEFAULT\tNAME\tPATH\tSTATUS")

	for _, name := range cfg.DatabaseNames() {
		resolved, err := cfg.Resolve(name)
		if err != nil {
			return err
		}

		marker := ""
		if name == effectiveDefault {
			marker = "*"
		}

		status := "ok"
		if err := checkDatabaseFile(resolved.Path); err != nil {
			status = "MISSING"
		}

		fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", marker, name, resolved.Path, status)
	}

	return table.Flush()
}

// checkDatabaseFile verifies that path is an existing regular file. The
// content is not checked: that would require the master password.
func checkDatabaseFile(path string) error {
	info, err := os.Stat(config.ExpandPath(path))
	if err != nil {
		return fmt.Errorf("cannot use %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory, not a .kdbx file", path)
	}
	return nil
}
