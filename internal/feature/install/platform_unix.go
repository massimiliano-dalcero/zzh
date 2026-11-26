//go:build !windows

package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// isElevated reports whether zzh runs as root, needed to write
// /usr/local/bin.
func isElevated() bool {
	return os.Geteuid() == 0
}

// registerPaths has nothing to write on Linux and macOS: /usr/local/bin is
// already in PATH before /usr/bin, and for ~/.local/bin the shell profile
// is the user's business (see pathAdvice).
func registerPaths(l layout, system, shims bool, out io.Writer) error {
	return nil
}

// unregisterPaths has nothing to undo, symmetrically.
func unregisterPaths(l layout, system, shims, all bool, out io.Writer) error {
	return nil
}

// restartNotice is not needed: no environment was changed.
const restartNotice = ""

// pathAdvice checks the PATH of the current shell and explains what to add
// to the shell profile when the installation folder is missing from it or,
// for the shims, comes after /usr/bin (where the real ssh is). zzh does not
// edit profiles itself: which file is read depends on the shell and on how
// it is started, and the user knows their setup better.
func pathAdvice(l layout, system, shims bool) string {
	entries := filepath.SplitList(os.Getenv("PATH"))
	position := func(dir string) int {
		for i, entry := range entries {
			if filepath.Clean(entry) == filepath.Clean(dir) {
				return i
			}
		}
		return -1
	}

	binPosition := position(l.binDir)
	realSSH := position("/usr/bin")

	missing := binPosition < 0
	tooLate := shims && binPosition >= 0 && realSSH >= 0 && binPosition > realSSH
	if !missing && !tooLate {
		return ""
	}

	if system {
		return fmt.Sprintf("%s is not before /usr/bin in this PATH: fix the system PATH, or the shims will not be used", l.binDir)
	}

	profile := "~/.profile (bash) or ~/.zprofile (zsh)"
	line := fmt.Sprintf(`export PATH="%s:$PATH"`, strings.Replace(l.binDir, os.Getenv("HOME"), "$HOME", 1))
	if missing {
		return fmt.Sprintf("%s is not in PATH: add this line to %s, then open a new terminal:\n  %s", l.binDir, profile, line)
	}
	return fmt.Sprintf("%s comes after /usr/bin in PATH, so the real ssh would be found first: add this line to %s, then open a new terminal:\n  %s",
		l.binDir, profile, line)
}

// removeExecutable deletes the installed zzh. On Linux and macOS a running
// program can be deleted: processes using it keep running.
func removeExecutable(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cannot remove %s: %w", path, err)
	}
	return nil
}
