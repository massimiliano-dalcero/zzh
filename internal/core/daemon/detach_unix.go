//go:build !windows

package daemon

import (
	"os/exec"
	"syscall"
)

// detach makes the daemon independent from the terminal that started it: a
// new session (setsid) has no controlling terminal, so closing the terminal
// does not send it SIGHUP. Its standard streams are /dev/null because the
// exec.Cmd streams are left nil.
func detach(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
