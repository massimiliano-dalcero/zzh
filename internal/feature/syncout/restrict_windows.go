//go:build windows

package syncout

import (
	"fmt"
	"os/exec"
	"os/user"
	"strings"
)

// restrictToOwner makes a file accessible only to the current user.
//
// On Windows the file mode passed to OpenFile is ignored and a new file
// inherits the permissions of its folder, which may include other accounts
// or groups. The Windows OpenSSH client then refuses the private key
// ("UNPROTECTED PRIVATE KEY FILE ... bad permissions"). icacls, part of
// every Windows installation, removes the inherited entries and grants full
// control to the current user only, identified by SID (the "*" prefix) so
// that localized or domain account names cannot be misread.
func restrictToOwner(path string) error {
	current, err := user.Current()
	if err != nil {
		return fmt.Errorf("cannot determine the current user: %w", err)
	}

	// On Windows user.Current().Uid is the account SID (S-1-5-21-...).
	output, err := exec.Command("icacls", path, "/inheritance:r", "/grant:r", "*"+current.Uid+":F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot restrict the permissions of %s: %v: %s", path, err, strings.TrimSpace(string(output)))
	}
	return nil
}
