//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW: the child gets a console without a
// window, the way editors start git in the background.
const createNoWindow = 0x08000000

// detachFromTerminal starts the command with a hidden console: zzh must
// recognize that nobody can see it and refuse to ask instead of waiting.
func detachFromTerminal(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
}
