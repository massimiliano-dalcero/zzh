package sshcli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// posixRuntimeLibraries mark a folder whose programs are built for a POSIX
// emulation layer on Windows: MSYS2 (Git for Windows, MSYS2) or Cygwin.
// Their ssh.exe only understands emulated Unix sockets and cannot reach the
// named pipe of the zzh agent.
var posixRuntimeLibraries = []string{"msys-2.0.dll", "cygwin1.dll"}

// ResolveBinary returns the path of the ssh client to run.
//
// An explicit path (anything containing a path separator) is used as given:
// it is the user's choice. A bare name such as the default "ssh" is looked up
// in PATH, but on Windows the copies belonging to an MSYS2 or Cygwin
// environment are skipped, falling back to the Windows OpenSSH in
// System32\OpenSSH.
//
// This matters because git for Windows runs core.sshCommand through its own
// shell, which puts its MSYS folders first in PATH: without this rule, zzh
// started by git would pick Git's ssh.exe, which cannot use the zzh agent,
// and the server would refuse the login ("Permission denied (publickey)")
// although the same command works from PowerShell (D-036).
func ResolveBinary(binary string) (string, error) {
	if strings.ContainsAny(binary, `/\`) || runtime.GOOS != "windows" {
		return exec.LookPath(binary)
	}

	name := binary
	if !strings.HasSuffix(strings.ToLower(name), ".exe") {
		name += ".exe"
	}

	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err != nil || info.IsDir() {
			continue
		}
		if isPosixEmulationFolder(dir) {
			continue
		}
		return candidate, nil
	}

	native := filepath.Join(os.Getenv("SystemRoot"), "System32", "OpenSSH", name)
	if _, err := os.Stat(native); err == nil {
		return native, nil
	}

	return "", fmt.Errorf("no native %s found in PATH nor in %s: %w", name, filepath.Dir(native), errors.ErrUnsupported)
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
