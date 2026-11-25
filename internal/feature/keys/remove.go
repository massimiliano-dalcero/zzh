package keys

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"zzh/internal/core/cliflags"
	"zzh/internal/core/vault"
)

// removeKey handles "zzh key rm <name> [--force] [--yes]" (D-035).
//
// A key still associated with hosts is refused unless --force, which also
// removes those associations. The public key installed on remote servers is
// not touched: removing it from authorized_keys is a separate decision.
func removeKey(args []string, store Store, confirm Confirmer, out io.Writer) error {
	parsed, err := cliflags.Parse(args, cliflags.Spec{Switches: []string{"force", "yes"}})
	if err != nil {
		return err
	}
	if len(parsed.Positional) != 1 {
		return errors.New(usage)
	}
	name := parsed.Positional[0]

	if !parsed.Switch("yes") {
		question := fmt.Sprintf("Remove the key %q?", name)
		if parsed.Switch("force") {
			question = fmt.Sprintf("Remove the key %q and its association with every host?", name)
		}
		confirmed, err := confirm(question)
		if err != nil {
			return fmt.Errorf("%w (use --yes to remove without asking)", err)
		}
		if !confirmed {
			fmt.Fprintln(out, "Nothing removed")
			return nil
		}
	}

	var recycled bool
	var detached []string
	err = store.Modify(func(v *vault.Vault) error {
		var removeErr error
		recycled, detached, removeErr = v.RemoveKey(name, parsed.Switch("force"))
		return removeErr
	})
	if err != nil {
		return err
	}

	if recycled {
		fmt.Fprintf(out, "Key %q moved to the KeePass recycle bin (restore it from KeePassXC)\n", name)
	} else {
		fmt.Fprintf(out, "Key %q deleted (the database has no recycle bin; the previous file is kept as .bak)\n", name)
	}
	if len(detached) > 0 {
		fmt.Fprintf(out, "No longer used by: %s\n", strings.Join(detached, ", "))
	}
	return nil
}
