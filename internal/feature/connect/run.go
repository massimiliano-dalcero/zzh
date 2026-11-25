package connect

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"

	"zzh/internal/core/sshcli"
)

// ExitStatus is returned when ssh ran but ended with a non-zero code.
// The caller should exit with the same code and without printing anything:
// ssh has already reported the problem, and scripts like
// "zzh web01 test -f /etc/foo" rely on the remote exit code.
type ExitStatus struct {
	Code int
}

func (e ExitStatus) Error() string {
	return fmt.Sprintf("ssh exited with status %d", e.Code)
}

// Run starts ssh with the terminal attached and waits for it to finish.
//
// ssh runs as a child process instead of replacing zzh (exec), because zzh
// must stay alive during the session to serve keys and passwords to ssh.
//
// extraEnv is added to the inherited environment; for a variable that is
// already set (e.g. a system SSH_AUTH_SOCK) the value in extraEnv wins,
// because exec keeps the last occurrence of a duplicated name.
func Run(binary string, args []string, extraEnv []string) error {
	// The ssh client is resolved here rather than by exec, so that on
	// Windows the ssh of an MSYS environment (Git for Windows puts it first
	// in PATH when it runs zzh) is never picked (D-036).
	path, err := sshcli.ResolveBinary(binary)
	if err != nil {
		return fmt.Errorf("cannot find the ssh client %q: %w", binary, err)
	}
	command := exec.Command(path, args...)
	if len(extraEnv) > 0 {
		command.Env = append(os.Environ(), extraEnv...)
	}
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	// Ctrl+C in the terminal is delivered to both zzh and ssh. ssh decides
	// what to do with it (forward it to the remote side or terminate); zzh
	// must not die first, otherwise it could not wait for ssh and report
	// its exit code. Notify replaces the default "terminate" behaviour.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	err = command.Run()

	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return ExitStatus{Code: exitError.ExitCode()}
	}
	if err != nil {
		return fmt.Errorf("cannot run %s: %w", binary, err)
	}
	return nil
}
