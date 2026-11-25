// Package aliases implements "zzh alias add|rm": extra names for an
// existing host, so that one entry can be reached with several words
// without duplicating it (D-031).
package aliases

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"zzh/internal/core/vault"
)

const usage = `usage:
  zzh alias add <host> <alias>...
  zzh alias rm <alias>...`

// Modifier opens the database, applies a change and saves it
// (see session.Modify).
type Modifier func(change func(v *vault.Vault) error) error

// Run executes an alias subcommand.
//
// reserved lists the zzh subcommand names: an alias equal to one of them
// could never be used, because "zzh <subcommand>" runs the subcommand.
func Run(args []string, reserved []string, modify Modifier, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}

	switch {
	case args[0] == "add" && len(args) >= 3:
		host, newAliases := args[1], args[2:]
		for _, alias := range newAliases {
			if slices.Contains(reserved, alias) {
				return fmt.Errorf("%q is a zzh command and cannot be used as an alias", alias)
			}
		}
		if err := modify(func(v *vault.Vault) error { return v.AddHostAliases(host, newAliases) }); err != nil {
			return err
		}
		fmt.Fprintf(out, "Host %q can now also be reached as: %s\n", host, strings.Join(newAliases, " "))
		return nil

	case args[0] == "rm" && len(args) >= 2:
		removed := args[1:]
		if err := modify(func(v *vault.Vault) error { return v.RemoveHostAliases(removed) }); err != nil {
			return err
		}
		fmt.Fprintf(out, "Aliases removed: %s\n", strings.Join(removed, " "))
		return nil

	default:
		return errors.New(usage)
	}
}
