// Package doctor implements "zzh doctor": a step-by-step check of every
// prerequisite zzh needs (configuration, KeePass database, ssh client),
// printed as a readable report.
package doctor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"zzh/internal/core/config"
	"zzh/internal/core/sshcli"
	"zzh/internal/core/style"
)

// reporter prints one line per check and remembers whether any failed.
// The checks keep going after a failure so that the user sees all the
// problems at once instead of fixing them one run at a time.
type reporter struct {
	out    io.Writer
	failed bool
}

// check prints one result. On a terminal the mark is green or red and the
// commands suggested in the message ("zzh init …") are highlighted.
func (r *reporter) check(ok bool, format string, values ...any) {
	s := style.For(r.out)
	mark := s.Success("ok  ")
	if !ok {
		mark = s.Error("FAIL")
		r.failed = true
	}
	fmt.Fprintf(r.out, "[%s] %s\n", mark, s.Commands(fmt.Sprintf(format, values...)))
}

// Run performs all checks and writes the report to out. It returns an error
// at the end if at least one check failed, so that scripts can rely on the
// exit code.
func Run(loaded config.Loaded, loadErr error, out io.Writer) error {
	r := &reporter{out: out}
	cfg := loaded.Config

	switch {
	case loadErr != nil:
		r.check(false, "config: %v", loadErr)
	case !loaded.Exists:
		r.check(true, "config: %s not found, using defaults (create it with \"zzh config init\")", loaded.Path)
	default:
		r.check(true, "config: %s", loaded.Path)
	}

	checkDatabases(r, cfg)
	checkSSH(r, cfg.SSH.Binary)

	if r.failed {
		return errors.New("some checks failed")
	}
	return nil
}

// checkDatabases verifies that every bookmarked .kdbx path points to a file
// and that zzh knows which one to use without "--db". It cannot verify the
// passwords: that would require asking for them, which a diagnostic command
// should not do.
func checkDatabases(r *reporter, cfg config.Config) {
	// No bookmark is the normal state right after installation, so the
	// message tells the user how to get started.
	if len(cfg.Databases) == 0 {
		r.check(false, "database: none bookmarked (use \"zzh init <name> <path>\" or \"zzh db add <name> <path>\")")
		return
	}

	for _, name := range cfg.DatabaseNames() {
		resolved, err := cfg.Resolve(name)
		if err != nil {
			r.check(false, "database %s: %v", name, err)
			continue
		}

		info, err := os.Stat(resolved.Path)
		switch {
		case err != nil:
			r.check(false, "database %s: %v", name, err)
		case info.IsDir():
			r.check(false, "database %s: %s is a directory", name, resolved.Path)
		default:
			r.check(true, "database %s: %s", name, resolved.Path)
		}
	}

	if resolved, err := cfg.Resolve(""); err != nil {
		r.check(false, "default database: %v", err)
	} else {
		r.check(true, "default database: %s", resolved.Name)
	}
}

// checkSSH locates the ssh binary and verifies that it supports every
// feature in sshcli.RequiredFeatures, reporting each one separately so the
// user understands what would not work with an old client.
func checkSSH(r *reporter, binary string) {
	// Same resolution as the connections, so the report shows the ssh that
	// zzh really runs.
	binaryPath, err := sshcli.ResolveBinary(binary)
	if err != nil {
		r.check(false, "ssh: %q not found: %v", binary, err)
		return
	}
	r.check(true, "ssh: %s", binaryPath)

	version, err := sshcli.DetectVersion(binaryPath)
	if err != nil {
		r.check(false, "ssh version: %v", err)
		return
	}
	r.check(true, "ssh version: OpenSSH %s", version)

	if !isUsableBuild(runtime.GOOS, version.Raw) {
		r.check(false, "  %s is not the native Windows OpenSSH: set \"ssh.binary\" to "+
			`C:\\Windows\\System32\\OpenSSH\\ssh.exe or install Microsoft.OpenSSH.Preview`, binaryPath)
	}

	for _, feature := range sshcli.RequiredFeatures {
		r.check(version.AtLeast(feature.Required),
			"  %s (needs %s): %s", feature.Name, feature.Required, feature.Purpose)
	}
}

// isUsableBuild reports whether the ssh build can work with zzh on the given
// operating system.
//
// On Windows several ssh.exe may be in PATH: the one shipped with Git for
// Windows is an MSYS build that only understands emulated Unix sockets, so
// it cannot talk to the named-pipe agent zzh provides. Only the native
// Win32-OpenSSH build identifies itself as "OpenSSH_for_Windows".
func isUsableBuild(goos, banner string) bool {
	if goos != "windows" {
		return true
	}
	return strings.Contains(banner, "OpenSSH_for_Windows")
}
