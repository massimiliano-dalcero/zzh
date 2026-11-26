//go:build windows

package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// envKey is where Windows keeps the environment variables of one scope.
// The variables of new processes are built from these two keys: the system
// one first, then the user one, which is why a user PATH entry can never
// come before a system one.
type envKey struct {
	root registry.Key
	path string
}

var (
	systemEnvironment = envKey{registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`}
	userEnvironment   = envKey{registry.CURRENT_USER, `Environment`}
)

// gitSSHVariable makes Git for Windows use the shim: git puts its own MSYS
// folders first in PATH and would otherwise run its own ssh.exe (verified,
// see D-055). One system variable, read by every git, instead of editing
// the configuration of each git installation.
const gitSSHVariable = "GIT_SSH"

func environmentFor(system bool) envKey {
	if system {
		return systemEnvironment
	}
	return userEnvironment
}

// isElevated reports whether zzh runs with administrator rights, needed to
// write the system environment and Program Files.
func isElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// expandEntry expands the %VARIABLES% of a registry PATH entry.
func expandEntry(entry string) string {
	expanded, err := registry.ExpandString(entry)
	if err != nil {
		return entry
	}
	return expanded
}

// isOpenSSHFolder recognizes the folder of the Windows OpenSSH in PATH,
// normally written as %SystemRoot%\System32\OpenSSH\.
func isOpenSSHFolder(entry string) bool {
	native := filepath.Join(os.Getenv("SystemRoot"), "System32", "OpenSSH")
	return sameEntry(entry, native, expandEntry)
}

// registerPaths puts zzh, and with shims the shim folder and GIT_SSH, in the
// environment of new processes.
func registerPaths(l layout, system, shims bool, out io.Writer) error {
	key := environmentFor(system)
	changed := false

	pathChanged, err := editPath(key, func(value string) (string, bool) {
		value, appended := appendEntry(value, l.binDir, expandEntry)
		inserted := false
		if shims {
			value, inserted = insertBefore(value, l.shimDir, isOpenSSHFolder, expandEntry)
		}
		return value, appended || inserted
	})
	if err != nil {
		return err
	}
	changed = changed || pathChanged

	if shims {
		shimSSH := l.shimPath("ssh")
		current, found, err := getVariable(key, gitSSHVariable)
		if err != nil {
			return err
		}
		switch {
		case !found:
			if err := setVariable(key, gitSSHVariable, shimSSH); err != nil {
				return err
			}
			changed = true
		case !sameEntry(current, shimSSH, expandEntry):
			fmt.Fprintf(out, "%s is already set to %q: left unchanged, so git keeps using that program\n", gitSSHVariable, current)
		}
	}

	if changed {
		broadcastEnvironmentChange()
	}
	return nil
}

// unregisterPaths removes what registerPaths added: the shim folder and
// GIT_SSH (only if it still points to the shim) and, with all, the zzh
// folder.
func unregisterPaths(l layout, system, shims, all bool, out io.Writer) error {
	key := environmentFor(system)
	changed := false

	pathChanged, err := editPath(key, func(value string) (string, bool) {
		removedShim, removedBin := false, false
		if shims && l.shimDir != "" {
			value, removedShim = removeEntry(value, l.shimDir, expandEntry)
		}
		if all {
			value, removedBin = removeEntry(value, l.binDir, expandEntry)
		}
		return value, removedShim || removedBin
	})
	if err != nil {
		return err
	}
	changed = changed || pathChanged

	if shims && l.shimDir != "" {
		current, found, err := getVariable(key, gitSSHVariable)
		if err != nil {
			return err
		}
		if found && sameEntry(current, l.shimPath("ssh"), expandEntry) {
			if err := deleteVariable(key, gitSSHVariable); err != nil {
				return err
			}
			changed = true
		} else if found {
			fmt.Fprintf(out, "%s points to %q, not to the zzh shim: left unchanged\n", gitSSHVariable, current)
		}
	}

	if changed {
		broadcastEnvironmentChange()
	}
	return nil
}

// pathAdvice has nothing to add on Windows: the PATH is edited directly.
func pathAdvice(l layout, system, shims bool) string {
	return ""
}

// restartNotice reminds that only new processes see the new environment.
const restartNotice = "Programs already open (terminals, editors) keep the old PATH: restart them to use the change."

// editPath applies edit to the Path variable of key, keeping its registry
// type: the system Path is REG_EXPAND_SZ and must stay so, or entries like
// %SystemRoot%\system32 would stop working.
func editPath(key envKey, edit func(string) (string, bool)) (bool, error) {
	k, err := registry.OpenKey(key.root, key.path, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, fmt.Errorf("cannot open the environment in the registry: %w", err)
	}
	defer k.Close()

	value, valueType, err := k.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		value, valueType = "", registry.EXPAND_SZ
	} else if err != nil {
		return false, fmt.Errorf("cannot read Path from the registry: %w", err)
	}

	updated, changed := edit(value)
	if !changed {
		return false, nil
	}

	if valueType == registry.SZ {
		err = k.SetStringValue("Path", updated)
	} else {
		err = k.SetExpandStringValue("Path", updated)
	}
	if err != nil {
		return false, fmt.Errorf("cannot write Path to the registry: %w", err)
	}
	return true, nil
}

func getVariable(key envKey, name string) (string, bool, error) {
	k, err := registry.OpenKey(key.root, key.path, registry.QUERY_VALUE)
	if err != nil {
		return "", false, fmt.Errorf("cannot open the environment in the registry: %w", err)
	}
	defer k.Close()

	value, _, err := k.GetStringValue(name)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("cannot read %s from the registry: %w", name, err)
	}
	return value, true, nil
}

func setVariable(key envKey, name, value string) error {
	k, err := registry.OpenKey(key.root, key.path, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("cannot open the environment in the registry: %w", err)
	}
	defer k.Close()
	if err := k.SetStringValue(name, value); err != nil {
		return fmt.Errorf("cannot write %s to the registry: %w", name, err)
	}
	return nil
}

func deleteVariable(key envKey, name string) error {
	k, err := registry.OpenKey(key.root, key.path, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("cannot open the environment in the registry: %w", err)
	}
	defer k.Close()
	if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("cannot remove %s from the registry: %w", name, err)
	}
	return nil
}

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procSendMessageTimeoutW = user32.NewProc("SendMessageTimeoutW")
)

// broadcastEnvironmentChange tells running programs that the environment in
// the registry changed (WM_SETTINGCHANGE with "Environment"). Explorer
// listens and passes the new values to the programs it starts from then on;
// programs already running keep their own copy.
func broadcastEnvironmentChange() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001A
		smtoAbortIfHung = 0x0002
	)
	environment, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	var result uintptr
	// Best effort: the registry is already updated; if the broadcast fails,
	// the change is seen after the next logon instead of at once.
	_, _, _ = procSendMessageTimeoutW.Call(hwndBroadcast, wmSettingChange, 0,
		uintptr(unsafe.Pointer(environment)), smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)))
}

// removeExecutable deletes the installed zzh. Windows refuses to delete a
// running .exe, which is the usual case ("zzh uninstall" run from the
// installed zzh): the file is renamed (allowed while running) and a hidden
// cmd.exe deletes it, with its folder if empty, a moment after zzh exits.
func removeExecutable(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}

	pending := path + ".old"
	// Best effort: an older leftover may itself still be in use.
	_ = os.Remove(pending)
	if err := os.Rename(path, pending); err != nil {
		return fmt.Errorf("cannot remove %s: %w", path, err)
	}

	// cmd.exe parses its command line itself, so it is given verbatim
	// (Windows paths cannot contain double quotes, so quoting is safe).
	// "ping -n 3" is the classic way to wait about two seconds in cmd.
	commandLine := fmt.Sprintf(`cmd.exe /d /c ping -n 3 127.0.0.1 >nul & del /f /q "%s" & rmdir "%s"`,
		pending, filepath.Dir(path))
	cleaner := exec.Command("cmd.exe")
	cleaner.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       commandLine,
		CreationFlags: windows.CREATE_NO_WINDOW | windows.DETACHED_PROCESS,
	}
	if err := cleaner.Start(); err != nil {
		return fmt.Errorf("%s is in use and was renamed to %s; delete it after zzh exits (%v)", path, pending, err)
	}
	// The cleaner runs on its own; zzh does not wait for it.
	return cleaner.Process.Release()
}
