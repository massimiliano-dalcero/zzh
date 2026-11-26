// Package caller finds out which program asked zzh to connect.
//
// When a program runs ssh through the shims, the process tree looks like
//
//	Code.exe → git.exe → zzh (started as "ssh")
//
// and for scp also contains zzh started as "scp" and the real scp. The
// confirmation dialog must name the program the user knows ("git.exe,
// started by Code.exe"), and authorizations are remembered per program, so
// the ancestors of zzh are walked upwards skipping the intermediaries that
// belong to the SSH machinery itself (zzh, ssh, scp, sftp).
package caller

import (
	"path/filepath"
	"strings"
)

// Caller is the program that started the connection.
type Caller struct {
	// Name is the file name of the program ("git.exe").
	Name string

	// Path is the full path of its executable when it can be read, "" when
	// the system does not tell (another user's process, a process that has
	// meanwhile exited). Authorizations are remembered by Path, or by Name
	// when the path is unknown.
	Path string

	// Parent is the name of the program that started it ("Code.exe"), ""
	// when unknown; it only helps the user recognize the request.
	Parent string
}

// Identity is the key under which an authorization is remembered.
func (c Caller) Identity() string {
	if c.Path != "" {
		return c.Path
	}
	return c.Name
}

// Describe returns the caller as shown to the user: "git.exe (started by
// Code.exe)", or "an unknown program".
func (c Caller) Describe() string {
	if c.Name == "" {
		return "an unknown program"
	}
	if c.Parent == "" {
		return c.Name
	}
	return c.Name + " (started by " + c.Parent + ")"
}

// process is one entry of the process tree as read from the system.
type process struct {
	pid    int
	parent int
	name   string
	path   string
}

// maxDepth bounds the walk: a few levels are enough to get past the SSH
// machinery, and the bound protects from cycles caused by reused PIDs.
const maxDepth = 12

// machinery lists the programs that are part of the connection itself and
// are therefore skipped: they are never what the user wants to authorize.
var machinery = map[string]bool{"zzh": true, "ssh": true, "scp": true, "sftp": true}

// isMachinery compares names without extension and case, so that "SSH.EXE"
// and "ssh" are the same program.
func isMachinery(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	return machinery[strings.TrimSuffix(base, ".exe")]
}

// Identify walks up from the parent of the current process and returns the
// first program that is not part of the SSH machinery, with its own parent.
// It never fails: when the tree cannot be read the result is an unknown
// caller, which the dialog presents as such.
func Identify(self int) Caller {
	return identify(self, lookup)
}

// identify is Identify with the process lookup as a parameter, for tests.
func identify(self int, find func(pid int) (process, bool)) Caller {
	current, ok := find(self)
	if !ok {
		return Caller{}
	}

	for depth := 0; depth < maxDepth; depth++ {
		parent, ok := find(current.parent)
		if !ok || parent.pid == current.pid {
			return Caller{}
		}
		if !isMachinery(parent.name) {
			caller := Caller{Name: parent.name, Path: parent.path}
			if grand, ok := find(parent.parent); ok && grand.pid != parent.pid {
				caller.Parent = grand.name
			}
			return caller
		}
		current = parent
	}
	return Caller{}
}
