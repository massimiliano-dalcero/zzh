// Package configcmd implements "zzh config path|show|init", the commands
// that let the user find, inspect and create the configuration file.
package configcmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"zzh/internal/core/config"
)

const usage = `usage:
  zzh config path | show | init
  zzh config get <setting>
  zzh config set <setting> <value>...
  zzh config unset <setting>
settings use dotted names, e.g. ssh.use_system_keys or lock.idle_timeout`

// runSetting reads or changes one setting by its dotted name.
//
// Changes are written immediately. They are refused when the file currently
// fails to load: rewriting it from the defaults would silently drop the
// user's other settings.
func runSetting(action, path string, values []string, loaded config.Loaded, loadErr error, out io.Writer) error {
	if loadErr != nil {
		return loadErr
	}

	var (
		updated config.Config
		err     error
	)

	switch action {
	case "get":
		if len(values) != 0 {
			return errors.New(usage)
		}
		value, err := config.Get(loaded.Config, path)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, value)
		return nil

	case "set":
		updated, err = config.Set(loaded.Config, path, values)

	case "unset":
		if len(values) != 0 {
			return errors.New(usage)
		}
		updated, err = config.Unset(loaded.Config, path)
	}
	if err != nil {
		return err
	}

	if err := config.Save(loaded.Path, updated); err != nil {
		return err
	}

	value, err := config.Get(updated, path)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s = %s\n", path, value)
	return nil
}

// Run executes the config subcommand.
//
// loaded and loadErr are the result of config.LoadDefault: they are passed
// in, instead of being loaded here, so that tests can point the command at a
// temporary file. Normal output goes to out, notes for the user to errOut,
// so that "zzh config show > file.json" produces a clean JSON file.
func Run(args []string, loaded config.Loaded, loadErr error, out, errOut io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}

	// get, set and unset take a setting name (and values); the other
	// actions take nothing.
	switch args[0] {
	case "get", "set", "unset":
		if len(args) < 2 {
			return errors.New(usage)
		}
		return runSetting(args[0], args[1], args[2:], loaded, loadErr, out)
	}
	if len(args) != 1 {
		return errors.New(usage)
	}

	switch args[0] {
	case "path":
		// The path is useful precisely when the file is broken, so it is
		// printed regardless of the load error.
		if loaded.Path == "" {
			return loadErr
		}
		fmt.Fprintln(out, loaded.Path)
		return nil

	case "show":
		if loadErr != nil {
			return loadErr
		}
		if !loaded.Exists {
			fmt.Fprintf(errOut, "# %s does not exist, showing defaults\n", loaded.Path)
		}
		data, err := json.MarshalIndent(loaded.Config, "", "  ")
		if err != nil {
			return fmt.Errorf("cannot encode configuration: %w", err)
		}
		fmt.Fprintln(out, string(data))
		return nil

	case "init":
		// Never overwrite an existing file, even a broken one: it may
		// contain the user's hand-made settings.
		if loaded.Exists {
			return fmt.Errorf("%s already exists", loaded.Path)
		}
		if loadErr != nil {
			return loadErr
		}
		if err := config.Save(loaded.Path, config.Defaults()); err != nil {
			return err
		}
		fmt.Fprintln(out, "created", loaded.Path)
		fmt.Fprintln(out, "set \"database\" to the path of your .kdbx file")
		return nil

	default:
		return fmt.Errorf("unknown config action %q (use path, show or init)", args[0])
	}
}
