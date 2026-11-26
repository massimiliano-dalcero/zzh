// Package shim lets zzh stand in for ssh, scp and sftp.
//
// "zzh install --shim" (D-055) places links named ssh, scp and sftp that
// point to the zzh executable in a folder that comes before the real OpenSSH
// in PATH. Any program that runs "ssh" then starts zzh, which recognizes the
// name it was started with and behaves as ssh: hosts stored in KeePass get
// their data, keys and passwords from it, everything else runs the real ssh
// unchanged.
//
// This package holds the small pieces every part of zzh needs to cooperate
// with the shims: which name zzh was started with, where the real zzh
// executable is (not the link), and a guard against endless loops.
package shim

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Names zzh can be started as. ProgramSSH runs the connect logic; scp and
// sftp run the real program, which in turn runs zzh as its ssh.
const (
	ProgramSSH  = "ssh"
	ProgramSCP  = "scp"
	ProgramSFTP = "sftp"
)

// ZzhPrefix, as the first argument of zzh started as ssh, switches back to
// the zzh command line: "ssh --zzh ls" behaves as "zzh ls".
const ZzhPrefix = "--zzh"

// EnvAsSSH tells a zzh process to behave as ssh although it was not started
// with that name. scp and sftp are given the zzh executable itself as their
// ssh program (-S), because on Linux and macOS the path of the link is not
// known reliably; this variable carries the information instead. zzh
// removes it from its own environment at once, so that it never reaches
// the programs zzh starts.
const EnvAsSSH = "ZZH_AS_SSH"

// EnvDepth counts how many zzh processes acting as ssh are nested. A real
// chain is short (ssh started by scp, a jump host started by ssh); a long
// one means that the "real" ssh found in PATH is again zzh, for example a
// copy that os.SameFile cannot recognize, and zzh stops instead of starting
// itself forever.
const EnvDepth = "ZZH_SHIM_DEPTH"

// maxDepth is far above any legitimate nesting and low enough to stop a
// loop in a fraction of a second.
const maxDepth = 8

// InvokedAs returns the program zzh is standing in for: "ssh", "scp" or
// "sftp", or "" when it was started under its own name.
//
// The name comes from argv[0], reduced to its base name without extension
// and lower-cased. Callers differ: PowerShell passes the full path
// (C:\Program Files\zzh\shim\ssh.exe), cmd, Python and Node pass just "ssh"
// (verified on Windows, see D-055); Windows file names are case-insensitive.
// os.Executable cannot be used for this: on Linux it already resolves the
// link to the zzh file.
//
// EnvAsSSH, set by a zzh acting as scp or sftp, also selects ssh mode, and
// is consumed here.
func InvokedAs(argv0 string) string {
	if os.Getenv(EnvAsSSH) != "" {
		// Best effort: Unsetenv only fails for invalid names.
		_ = os.Unsetenv(EnvAsSSH)
		return ProgramSSH
	}

	name := strings.ToLower(filepath.Base(argv0))
	name = strings.TrimSuffix(name, ".exe")

	switch name {
	case ProgramSSH, ProgramSCP, ProgramSFTP:
		return name
	}
	return ""
}

// Executable returns the path of the zzh file itself, following links.
//
// zzh starts itself in two situations: as askpass program of ssh and to
// launch its daemon. Through a shim on Windows os.Executable returns the
// link (…\shim\ssh.exe): started with that name, the new process would act
// as ssh and read "daemon run" as a host name. Resolving the link always
// starts zzh under its own name.
func Executable() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot determine the zzh executable path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the zzh executable path %s: %w", executable, err)
	}
	return resolved, nil
}

// EnterNested records one more zzh acting as ssh in the environment that
// its children will inherit, and fails when the chain is too long (see
// EnvDepth).
func EnterNested() error {
	depth, _ := strconv.Atoi(os.Getenv(EnvDepth)) // missing or invalid: 0
	if depth >= maxDepth {
		return fmt.Errorf("zzh started itself %d times as ssh: the real ssh found in PATH is zzh again (a copy instead of a link?); check with \"zzh doctor\"", depth)
	}
	return os.Setenv(EnvDepth, strconv.Itoa(depth+1))
}
