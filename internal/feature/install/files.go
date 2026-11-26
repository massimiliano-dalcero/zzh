package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// copyExecutable installs the running zzh (source) at target.
//
// The new file is written next to the target first and then renamed over
// it, so that a failure halfway never leaves a truncated zzh in place. On
// Linux and macOS the rename is atomic and processes still running the old
// file (a daemon, a long ssh session) keep their copy. On Windows a running
// .exe cannot be overwritten or deleted, but it can be renamed: the old file
// is moved aside as "<target>.old" first, as build.ps1 does, and removed at
// the next install. It reports whether a file was copied (false when source
// already is the installed file).
func copyExecutable(source, target string) (bool, error) {
	if same, _ := sameFile(source, target); same {
		return false, nil
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return false, fmt.Errorf("cannot create %s: %w", filepath.Dir(target), err)
	}

	temporary := target + ".new"
	if err := copyFile(source, temporary); err != nil {
		return false, err
	}

	if runtime.GOOS == "windows" {
		if _, err := os.Stat(target); err == nil {
			old := target + ".old"
			// Best effort: a previous .old may still be in use by a process
			// started before the last upgrade; it is retried next time.
			_ = os.Remove(old)
			if err := os.Rename(target, old); err != nil {
				os.Remove(temporary)
				return false, fmt.Errorf("cannot move the installed zzh aside (%s): %w", target, err)
			}
		}
	}

	if err := os.Rename(temporary, target); err != nil {
		os.Remove(temporary)
		return false, fmt.Errorf("cannot install %s: %w", target, err)
	}
	return true, nil
}

// copyFile copies source to target with the execute permission.
func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", source, err)
	}
	defer in.Close()

	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", target, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(target)
		return fmt.Errorf("cannot write %s: %w", target, err)
	}
	if err := out.Close(); err != nil {
		os.Remove(target)
		return fmt.Errorf("cannot write %s: %w", target, err)
	}
	return nil
}

// sameFile reports whether a and b are the same file, following links.
func sameFile(a, b string) (bool, error) {
	infoA, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	infoB, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(infoA, infoB), nil
}

// linkState describes what is found where a shim should be.
type linkState int

const (
	linkMissing  linkState = iota // nothing there
	linkToZzh                     // a symbolic link to the installed zzh: ours
	linkDangling                  // a symbolic link to a file that no longer exists
	linkForeign                   // anything else: a real program, a link elsewhere
)

// inspectLink classifies the path where a shim goes.
func inspectLink(path, zzh string) linkState {
	info, err := os.Lstat(path)
	if err != nil {
		return linkMissing
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return linkForeign
	}
	if _, err := os.Stat(path); err != nil {
		return linkDangling
	}
	if same, _ := sameFile(path, zzh); same {
		return linkToZzh
	}
	return linkForeign
}

// errForeignFile is returned when a shim would replace something zzh did not
// create.
var errForeignFile = errors.New("it is not a link to zzh, so zzh leaves it alone")

// createLink makes path a symbolic link to zzh.
//
// Symbolic links, never copies: after an upgrade a link runs the new zzh,
// a copy would keep running the old one, and a hard link would detach when
// the Windows upgrade renames the old file aside (D-055). On Windows
// creating them needs administrator rights or Developer Mode, which
// "--system" already requires.
//
// A file that is not a link to zzh is never replaced: in /usr/local/bin
// there may be an ssh someone compiled on purpose. A dangling link is
// replaced, since it cannot run anything anyway (typically the link of an
// installation whose zzh was moved).
func createLink(path, zzh string) error {
	switch inspectLink(path, zzh) {
	case linkToZzh:
		return nil
	case linkForeign:
		return fmt.Errorf("%s already exists and %w; remove it yourself if the shim should take its place", path, errForeignFile)
	case linkDangling:
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("cannot replace the broken link %s: %w", path, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
	}
	if err := os.Symlink(zzh, path); err != nil {
		return fmt.Errorf("cannot create the link %s: %w", path, err)
	}
	return nil
}

// removeLink deletes path only if it is a link to zzh, or a dangling link,
// reporting whether something was removed. Anything else is left in place
// with an error, for the same reason createLink never replaces it.
func removeLink(path, zzh string) (bool, error) {
	switch inspectLink(path, zzh) {
	case linkMissing:
		return false, nil
	case linkForeign:
		return false, fmt.Errorf("%s %w", path, errForeignFile)
	}
	if err := os.Remove(path); err != nil {
		return false, fmt.Errorf("cannot remove %s: %w", path, err)
	}
	return true, nil
}

// removeEmptyDir removes a folder created by the installation once nothing
// is left in it.
func removeEmptyDir(dir string) {
	// Best effort: a folder that still holds files (a ".old" in use, files
	// of the user) is simply kept.
	_ = os.Remove(dir)
}
