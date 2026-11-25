//go:build windows

package daemon

import (
	"os/exec"
	"syscall"
)

// detachedProcess is DETACHED_PROCESS from the Windows API (not exported by
// the syscall package): the new process gets no console at all.
const detachedProcess = 0x00000008

// detach makes the daemon independent from the terminal that started it:
// no console (so no window and no Ctrl+C from that terminal) and its own
// process group.
func detach(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}
