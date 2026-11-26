//go:build !windows && !linux

package caller

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// lookup asks ps for the parent and the command of a process. macOS has no
// /proc; its ps prints the full executable path in "comm", which is what
// authorizations need.
func lookup(pid int) (process, bool) {
	output, err := exec.Command("ps", "-o", "ppid=,comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return process{}, false
	}
	line := strings.TrimSpace(string(output))
	parentText, command, found := strings.Cut(line, " ")
	if !found {
		return process{}, false
	}
	parent, err := strconv.Atoi(strings.TrimSpace(parentText))
	if err != nil {
		return process{}, false
	}
	command = strings.TrimSpace(command)

	p := process{pid: pid, parent: parent, name: filepath.Base(command)}
	if filepath.IsAbs(command) {
		p.path = command
	}
	return p, true
}
