package sshcli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// posixRuntimeLibraries mark a folder whose programs are built for a POSIX
// emulation layer on Windows: MSYS2 (Git for Windows, MSYS2) or Cygwin.
// Their ssh.exe only understands emulated Unix sockets and cannot reach the
// named pipe of the zzh agent.
var posixRuntimeLibraries = []string{"msys-2.0.dll", "cygwin1.dll"}

// selfExecutable is the running zzh; a variable so that tests can pretend
// that a file of theirs is zzh.
var selfExecutable = os.Executable

// ResolveBinary returns the path of the real program to run: the ssh client
// (or scp, sftp when zzh stands in for them).
//
// An explicit path (anything containing a path separator) is used as given:
// it is the user's choice. A bare name such as the default "ssh" is looked up
// in PATH with two rules:
//
//   - zzh itself is skipped. With the shims of "zzh install --shim" (D-055)
//     the first "ssh" in PATH is a link to zzh; picking it would make zzh
//     start itself forever. os.SameFile follows links, so the shim is
//     recognized whatever path it is reached through.
//   - On Windows the copies belonging to an MSYS2 or Cygwin environment are
//     skipped, falling back to the Windows OpenSSH in System32\OpenSSH. git
//     for Windows runs core.sshCommand through its own shell, which puts its
//     MSYS folders first in PATH: without this rule, zzh started by git would
//     pick Git's ssh.exe, which cannot use the zzh agent, and the server
//     would refuse the login ("Permission denied (publickey)") although the
//     same command works from PowerShell (D-036).
//
// An explicit path pointing to a shim would make zzh start itself; that
// case is stopped by shim.EnterNested with an explanation, not here.
func ResolveBinary(binary string) (string, error) {
	if strings.ContainsAny(binary, `/\`) {
		return binary, nil
	}

	self := ownFile()
	windows := runtime.GOOS == "windows"
	name := binary
	if windows && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		name += ".exe"
	}

	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		if !isProgram(candidate, windows) {
			continue
		}
		if self != nil && isSame(candidate, self) {
			continue
		}
		if isZzhLink(candidate) {
			continue
		}
		if windows && isPosixEmulationFolder(dir) {
			continue
		}
		return candidate, nil
	}

	if windows {
		native := filepath.Join(os.Getenv("SystemRoot"), "System32", "OpenSSH", name)
		if _, err := os.Stat(native); err == nil {
			return native, nil
		}
		return "", fmt.Errorf("no native %s found in PATH nor in %s: %w", name, filepath.Dir(native), errors.ErrUnsupported)
	}
	return "", fmt.Errorf("%s not found in PATH (zzh itself excluded)", name)
}

// ownFile describes the running zzh file, or nil when it cannot be read; in
// that case nothing is skipped, which is the behaviour before the shims.
func ownFile() os.FileInfo {
	path, err := selfExecutable()
	if err != nil {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	return info
}

// isZzhLink reports whether path is a link to a file named zzh: the shim of
// any zzh installation, not only of the running one. With two zzh on the
// same machine (system-wide and per user, or a downloaded copy run while
// another is installed) each would otherwise take the other's shim for the
// real ssh, and the two would start each other in a loop, every round
// possibly showing a dialog (seen on 2026-11-26).
func isZzhLink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	name := strings.ToLower(filepath.Base(target))
	return name == "zzh" || name == "zzh.exe"
}

// isSame reports whether path is the same file as self, following links.
func isSame(path string, self os.FileInfo) bool {
	info, err := os.Stat(path)
	return err == nil && os.SameFile(info, self)
}

// isProgram reports whether path is a file that can be run: on Unix the
// execute permission is required, as exec.LookPath does; on Windows the
// ".exe" name is enough.
func isProgram(path string, windows bool) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return windows || info.Mode()&0o111 != 0
}

// isPosixEmulationFolder reports whether the programs in dir need the MSYS2
// or Cygwin runtime, which always sits next to them.
func isPosixEmulationFolder(dir string) bool {
	for _, library := range posixRuntimeLibraries {
		if _, err := os.Stat(filepath.Join(dir, library)); err == nil {
			return true
		}
	}
	return false
}
