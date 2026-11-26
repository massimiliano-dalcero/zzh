//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detachFromTerminal starts the command in a new session, which has no
// controlling terminal: /dev/tty cannot be opened, as under cron.
func detachFromTerminal(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
