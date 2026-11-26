// Package dbcmd implements "zzh db ls|add|rm|default": the management of
// the bookmarked KeePass databases.
package dbcmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"zzh/internal/core/config"
	"zzh/internal/core/style"
	"zzh/internal/core/table"
	"zzh/internal/core/vault"
)

const usage = "usage: zzh db ls [--grep] | add <name> <path> | rm <name> | default <name>"

// Modifier opens the database bookmarked as name in cfg (asking the master
// password as usual), applies change and saves it; session.Modify in the
// real program. change may return vault.ErrNothingToSave to leave the file
// untouched.
type Modifier func(cfg config.Config, name string, change func(v *vault.Vault) error) error

// Run executes a db subcommand. Commands that change the bookmarks save the
// config file immediately.
func Run(args []string, loaded config.Loaded, loadErr error, modify Modifier, out io.Writer) error {
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
	case action == "ls":
		grep, err := table.ParseGrepOnly(params, usage)
		if err != nil {
			return err
		}
		return list(cfg, grep, out)

	case action == "add" && len(params) == 2:
		if err := checkDatabaseFile(params[1]); err != nil {
			return err
		}
		if err := cfg.AddDatabase(params[0], params[1]); err != nil {
			return err
		}
		created, err := prepareStructure(cfg, params[0], modify)
		if err != nil {
			// Nothing is saved: a bookmark to a database zzh cannot use
			// would only fail later, at the first command.
			return err
		}
		if err := config.Save(loaded.Path, cfg); err != nil {
			return err
		}
		fmt.Fprintf(out, "Bookmarked %q -> %s\n", params[0], cfg.Databases[params[0]].Path)
		if created {
			resolved, _ := cfg.Resolve(params[0]) // just added: cannot fail
			fmt.Fprintf(out, "Created the groups %[1]s/Hosts and %[1]s/Keys, where zzh keeps hosts and keys.\n", resolved.RootGroup)
		}
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
// prepareStructure makes sure the newly bookmarked database has the groups
// zzh works in (D-065), and reports whether it created them:
//
//   - no root group ("SSH" by default): the database was not used by zzh
//     yet; root, Hosts and Keys are created next to the existing groups,
//     with the usual safe write (backup, verification);
//   - root group with Hosts and Keys: already a zzh database, the file is
//     not rewritten;
//   - root group without Hosts or Keys: a group of the same name made by
//     someone else, or changed by hand; zzh stops instead of adding groups
//     inside something it does not own.
//
// Opening the database also checks the master password and the format
// (e.g. an unsupported Argon2id) before the bookmark exists.
func prepareStructure(cfg config.Config, name string, modify Modifier) (bool, error) {
	created := false
	err := modify(cfg, name, func(v *vault.Vault) error {
		switch v.Structure() {
		case vault.StructureComplete:
			return vault.ErrNothingToSave
		case vault.StructureMissing:
			created = true
			return v.CreateStructure()
		}
		root, _ := cfg.Resolve(name)
		return fmt.Errorf("the database already has a group %q without the \"Hosts\" and \"Keys\" groups zzh needs; "+
			"zzh does not change a group it did not create: add Hosts and Keys inside it with KeePassXC, "+
			"or use another group name with \"zzh config set root_group <name>\" before \"zzh db add\"", root.RootGroup)
	})
	return created, err
}

func list(cfg config.Config, grep bool, out io.Writer) error {
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

	// The default database is marked in green, a missing file in red.
	s := style.For(out)
	listing := table.Table{Headers: []string{"DEFAULT", "NAME", "PATH", "STATUS"}}

	for _, name := range cfg.DatabaseNames() {
		resolved, err := cfg.Resolve(name)
		if err != nil {
			return err
		}

		marker := ""
		if name == effectiveDefault {
			marker = "*"
		}

		status := table.Cell{Text: "ok", Paint: s.Success}
		if err := checkDatabaseFile(resolved.Path); err != nil {
			status = table.Cell{Text: "MISSING", Paint: s.Error}
		}

		listing.Add(
			table.Cell{Text: marker, Paint: s.Success},
			table.Cell{Text: name, Paint: s.Name},
			table.Cell{Text: resolved.Path, Paint: s.Dim},
			status,
		)
	}

	return listing.Render(out, grep)
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
