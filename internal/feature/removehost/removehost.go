// Package removehost implements "zzh rm <host>": it removes a host entry,
// moving it to the KeePass recycle bin when the database uses one (D-035).
package removehost

import (
	"errors"
	"fmt"
	"io"

	"zzh/internal/core/cliflags"
	"zzh/internal/core/vault"
)

const usage = "usage: zzh rm <host> [--yes]"

// Modifier opens the database, applies a change and saves it
// (see session.Modify).
type Modifier func(change func(v *vault.Vault) error) error

// Confirmer asks a yes/no question (prompt.Confirm).
type Confirmer func(message string) (bool, error)

// Run asks for confirmation (unless --yes) and removes the host.
func Run(args []string, modify Modifier, confirm Confirmer, out io.Writer) error {
	parsed, err := cliflags.Parse(args, cliflags.Spec{Switches: []string{"yes"}})
	if err != nil {
		return err
	}
	if len(parsed.Positional) != 1 {
		return errors.New(usage)
	}
	name := parsed.Positional[0]

	if !parsed.Switch("yes") {
		confirmed, err := confirm(fmt.Sprintf("Remove the host %q?", name))
		if err != nil {
			return fmt.Errorf("%w (use --yes to remove without asking)", err)
		}
		if !confirmed {
			fmt.Fprintln(out, "Nothing removed")
			return nil
		}
	}

	var recycled bool
	err = modify(func(v *vault.Vault) error {
		var removeErr error
		recycled, removeErr = v.RemoveHost(name)
		return removeErr
	})
	if err != nil {
		return err
	}

	fmt.Fprintln(out, removalMessage("Host", name, recycled))
	return nil
}

// removalMessage tells where the entry went, since the two outcomes are
// recovered in different ways.
func removalMessage(kind, name string, recycled bool) string {
	if recycled {
		return fmt.Sprintf("%s %q moved to the KeePass recycle bin (restore it from KeePassXC)", kind, name)
	}
	return fmt.Sprintf("%s %q deleted (the database has no recycle bin; the previous file is kept as .bak)", kind, name)
}
