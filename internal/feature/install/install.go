// Package install implements "zzh install" and "zzh uninstall" (D-055).
//
// "zzh install" copies the running zzh to a fixed place and puts it in PATH,
// for the current user (no privileges) or with --system for the whole
// machine (sudo on Linux/macOS, an administrator terminal on Windows). Run
// again from a newer zzh it upgrades the installed one.
//
// With --shim it also installs links named ssh, scp and sftp pointing to
// zzh, placed before the real OpenSSH in PATH, so that every program
// running ssh goes through zzh (see the shim package).
//
// Configuration and databases live in each user's profile and are never
// touched by either command.
package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"zzh/internal/core/cliflags"
	"zzh/internal/core/shim"
)

// currentEnvironment reads the folders layoutFor needs from this machine; a
// variable so that tests install into temporary folders.
var currentEnvironment = readEnvironment

func readEnvironment() environment {
	home, _ := os.UserHomeDir() // "" is reported by layoutFor when needed
	return environment{
		goos:         runtime.GOOS,
		home:         home,
		localAppData: os.Getenv("LOCALAPPDATA"),
		programFiles: os.Getenv("ProgramFiles"),
	}
}

// privilegeError explains how to get the rights --system needs.
func privilegeError(command string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("--system needs administrator rights: run \"%s\" from a terminal opened with \"Run as administrator\"", command)
	}
	return fmt.Errorf("--system needs root: run \"sudo %s\"", command)
}

// RunInstall is "zzh install [--system] [--shim]".
func RunInstall(args []string, out io.Writer) error {
	parsed, err := cliflags.Parse(args, cliflags.Spec{Switches: []string{"system", "shim"}})
	if err != nil {
		return err
	}
	if len(parsed.Positional) > 0 {
		return fmt.Errorf("unexpected argument %q (usage: zzh install [--system] [--shim])", parsed.Positional[0])
	}
	system, shims := parsed.Switch("system"), parsed.Switch("shim")

	l, err := layoutFor(currentEnvironment(), system)
	if err != nil {
		return err
	}
	if shims && l.shimDir == "" {
		return errNoUserShimOnWindows
	}
	if system && !isElevated() {
		return privilegeError("zzh install " + strings.Join(args, " "))
	}

	// Everything that could stop the installation is checked before the
	// first change, so that a refusal never leaves half an installation.
	if shims {
		if err := checkShimPlaces(l); err != nil {
			return err
		}
	}

	source, err := shim.Executable()
	if err != nil {
		return err
	}
	copied, err := copyExecutable(source, l.executable)
	if err != nil {
		return err
	}
	if copied {
		fmt.Fprintf(out, "Installed %s\n", l.executable)
	} else {
		fmt.Fprintf(out, "%s is the zzh running now: nothing to copy\n", l.executable)
	}

	if shims {
		for _, program := range shimPrograms {
			if err := createLink(l.shimPath(program), l.executable); err != nil {
				return err
			}
		}
		fmt.Fprintf(out, "Shims ready in %s: ssh, scp and sftp now run through zzh\n", l.shimDir)
	}

	if err := registerPaths(l, system, shims, out); err != nil {
		return err
	}

	if advice := pathAdvice(l, system, shims); advice != "" {
		fmt.Fprintln(out, advice)
	}
	if restartNotice != "" {
		fmt.Fprintln(out, restartNotice)
	}
	return nil
}

// checkShimPlaces refuses the installation when a place for a shim holds
// something zzh did not create (see createLink).
func checkShimPlaces(l layout) error {
	var foreign []string
	for _, program := range shimPrograms {
		path := l.shimPath(program)
		if inspectLink(path, l.executable) == linkForeign {
			foreign = append(foreign, path)
		}
	}
	if len(foreign) > 0 {
		return fmt.Errorf("cannot install the shims: %s %w; remove them yourself if the shims should take their place",
			strings.Join(foreign, ", "), errForeignFile)
	}
	return nil
}

// RunUninstall is "zzh uninstall [--system] --shim [--all]".
//
// --shim removes the shims; --all also removes zzh. zzh is never removed
// while its shims are installed: they would point to a missing file and
// ssh would stop working, for every user with --system.
func RunUninstall(args []string, out io.Writer) error {
	parsed, err := cliflags.Parse(args, cliflags.Spec{Switches: []string{"system", "shim", "all"}})
	if err != nil {
		return err
	}
	if len(parsed.Positional) > 0 {
		return fmt.Errorf("unexpected argument %q (usage: zzh uninstall [--system] --shim [--all])", parsed.Positional[0])
	}
	system, shims, all := parsed.Switch("system"), parsed.Switch("shim"), parsed.Switch("all")
	if !shims && !all {
		return errors.New("say what to remove: --shim removes the shims, --shim --all also removes zzh")
	}

	l, err := layoutFor(currentEnvironment(), system)
	if err != nil {
		return err
	}
	if system && !isElevated() {
		return privilegeError("zzh uninstall " + strings.Join(args, " "))
	}

	if all && !shims && l.shimDir != "" {
		for _, program := range shimPrograms {
			if state := inspectLink(l.shimPath(program), l.executable); state == linkToZzh || state == linkDangling {
				return fmt.Errorf("the shims in %s are still installed and would stop ssh from working without zzh: use \"zzh uninstall%s --shim --all\"",
					l.shimDir, systemFlag(system))
			}
		}
	}

	var problems []string

	if shims && l.shimDir != "" {
		removedAny := false
		for _, program := range shimPrograms {
			removed, err := removeLink(l.shimPath(program), l.executable)
			if err != nil {
				problems = append(problems, err.Error())
			}
			removedAny = removedAny || removed
		}
		if l.shimDir != l.binDir {
			removeEmptyDir(l.shimDir)
		}
		if removedAny {
			fmt.Fprintf(out, "Removed the shims from %s\n", l.shimDir)
		} else {
			fmt.Fprintf(out, "No zzh shims in %s\n", l.shimDir)
		}
	}

	if err := unregisterPaths(l, system, shims, all, out); err != nil {
		return err
	}

	if all {
		if err := removeExecutable(l.executable); err != nil {
			return err
		}
		// Best effort: the copy moved aside by the last upgrade on Windows
		// may still be in use; it is only a leftover.
		_ = os.Remove(l.executable + ".old")
		if filepath.Base(l.binDir) == "zzh" {
			// Only a folder that belongs to zzh (Windows), never
			// /usr/local/bin or ~/.local/bin.
			removeEmptyDir(l.binDir)
		}
		fmt.Fprintf(out, "Removed %s (configuration and databases are kept)\n", l.executable)
	}

	if restartNotice != "" {
		fmt.Fprintln(out, restartNotice)
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func systemFlag(system bool) string {
	if system {
		return " --system"
	}
	return ""
}
