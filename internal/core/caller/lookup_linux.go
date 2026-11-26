//go:build linux

package caller

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// lookup reads a process from /proc: the parent from /proc/<pid>/stat and
// the executable from the /proc/<pid>/exe link (readable for the user's own
// processes; the name from "comm" is used when it is not).
func lookup(pid int) (process, bool) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return process{}, false
	}

	// The format is "pid (comm) state ppid ...": comm may contain spaces
	// and parentheses, so the fields are taken after the last ")".
	text := string(stat)
	closing := strings.LastIndexByte(text, ')')
	opening := strings.IndexByte(text, '(')
	if closing < 0 || opening < 0 || opening > closing {
		return process{}, false
	}
	fields := strings.Fields(text[closing+1:])
	if len(fields) < 2 {
		return process{}, false
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return process{}, false
	}

	p := process{pid: pid, parent: parent, name: text[opening+1 : closing]}
	if path, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil {
		p.path = path
		p.name = filepath.Base(path)
	}
	return p, true
}
