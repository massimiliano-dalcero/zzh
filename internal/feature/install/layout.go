package install

import (
	"errors"
	"path/filepath"
)

// shimPrograms are the names the shims take. ssh covers every program that
// looks for ssh in PATH; scp and sftp are needed because they do not: the
// Windows ones run the ssh.exe of their own folder (D-055).
var shimPrograms = []string{"ssh", "scp", "sftp"}

// layout says where an installation puts its files.
type layout struct {
	// binDir holds the zzh executable and is added to PATH when needed.
	binDir string

	// executable is the full path of the installed zzh.
	executable string

	// shimDir holds the links named ssh, scp and sftp; "" when shims are not
	// possible for this scope (per-user on Windows).
	shimDir string

	// exeSuffix is ".exe" on Windows, where every program needs it.
	exeSuffix string
}

// environment gives layoutFor what it needs from the system, so that the
// rules can be tested for every platform on any machine.
type environment struct {
	goos         string
	home         string // user home directory
	localAppData string // Windows %LOCALAPPDATA%
	programFiles string // Windows %ProgramFiles%
}

// errNoUserShimOnWindows explains why "--shim" without "--system" is refused
// on Windows.
var errNoUserShimOnWindows = errors.New("on Windows the ssh shim can only be installed with --system: " +
	"the PATH of a user always comes after the system PATH, where the Windows OpenSSH is, " +
	"so a shim in a user folder would never be found first (run \"zzh install --system --shim\" " +
	"from a terminal opened as administrator)")

// layoutFor returns the installation layout for the given scope (D-055):
//
//	                 user (no privileges)                 system (sudo / administrator)
//	Windows zzh      %LOCALAPPDATA%\Programs\zzh\zzh.exe   %ProgramFiles%\zzh\zzh.exe
//	Windows shims    not possible                          %ProgramFiles%\zzh\shim\
//	Linux/macOS      ~/.local/bin/zzh and its shims        /usr/local/bin/zzh and its shims
//
// On Windows the shims have their own folder because that folder, and only
// that one, must come before System32\OpenSSH in PATH. On Linux and macOS
// the shims sit next to zzh in a folder that already comes before /usr/bin,
// so no PATH change is needed (which also covers services and cron, that do
// not read shell profiles).
func layoutFor(env environment, system bool) (layout, error) {
	var l layout

	switch {
	case env.goos == "windows" && system:
		if env.programFiles == "" {
			return l, errors.New("cannot find the Program Files folder (%ProgramFiles% is not set)")
		}
		l.binDir = filepath.Join(env.programFiles, "zzh")
		l.shimDir = filepath.Join(l.binDir, "shim")
		l.exeSuffix = ".exe"

	case env.goos == "windows":
		if env.localAppData == "" {
			return l, errors.New("cannot find the user's local application data folder (%LOCALAPPDATA% is not set)")
		}
		l.binDir = filepath.Join(env.localAppData, "Programs", "zzh")
		l.exeSuffix = ".exe"

	case system:
		l.binDir = "/usr/local/bin"
		l.shimDir = l.binDir

	default:
		if env.home == "" {
			return l, errors.New("cannot find the home directory")
		}
		l.binDir = filepath.Join(env.home, ".local", "bin")
		l.shimDir = l.binDir
	}

	l.executable = filepath.Join(l.binDir, "zzh"+l.exeSuffix)
	return l, nil
}

// shimPath returns the path of the shim for one program.
func (l layout) shimPath(program string) string {
	return filepath.Join(l.shimDir, program+l.exeSuffix)
}
