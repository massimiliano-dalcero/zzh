package caller

import (
	"os"
	"testing"
)

// tree builds a fake process table.
func tree(processes ...process) func(int) (process, bool) {
	table := map[int]process{}
	for _, p := range processes {
		table[p.pid] = p
	}
	return func(pid int) (process, bool) {
		p, ok := table[pid]
		return p, ok
	}
}

// git run by VS Code, through the ssh shim: the caller is git, started by
// Code.
func TestIdentifySkipsSSHMachinery(t *testing.T) {
	find := tree(
		process{pid: 1, parent: 0, name: "explorer.exe"},
		process{pid: 10, parent: 1, name: "Code.exe", path: `C:\Code\Code.exe`},
		process{pid: 20, parent: 10, name: "git.exe", path: `C:\Git\cmd\git.exe`},
		process{pid: 30, parent: 20, name: "ssh.exe"}, // the shim: zzh started as ssh
	)
	got := identify(30, find)
	if got.Name != "git.exe" || got.Path != `C:\Git\cmd\git.exe` || got.Parent != "Code.exe" {
		t.Fatalf("unexpected caller %+v", got)
	}
	if got.Describe() != "git.exe (started by Code.exe)" || got.Identity() != `C:\Git\cmd\git.exe` {
		t.Fatalf("unexpected description %q / identity %q", got.Describe(), got.Identity())
	}
}

// scp through the shims: zzh (ssh) ← real scp ← zzh (scp) ← script.
func TestIdentifyThroughSCP(t *testing.T) {
	find := tree(
		process{pid: 5, parent: 1, name: "bash", path: "/usr/bin/bash"},
		process{pid: 6, parent: 5, name: "backup.sh", path: "/home/max/backup.sh"},
		process{pid: 7, parent: 6, name: "scp"},
		process{pid: 8, parent: 7, name: "scp", path: "/usr/bin/scp"},
		process{pid: 9, parent: 8, name: "zzh", path: "/usr/local/bin/zzh"},
	)
	if got := identify(9, find); got.Name != "backup.sh" || got.Parent != "bash" {
		t.Fatalf("unexpected caller %+v", got)
	}
}

func TestIdentifyUnknown(t *testing.T) {
	if got := identify(99, tree()); got.Describe() != "an unknown program" {
		t.Fatalf("unexpected caller %+v", got)
	}
	// Without a path the identity falls back to the name.
	if (Caller{Name: "git"}).Identity() != "git" {
		t.Fatalf("identity must fall back to the name")
	}
}

// The real process table: the test binary's parent ("go" or the IDE) is
// found and is not part of the SSH machinery.
func TestIdentifyRealProcess(t *testing.T) {
	got := Identify(os.Getpid())
	if got.Name == "" {
		t.Skip("process tree not readable here")
	}
	t.Logf("caller of the test: %s, path %q", got.Describe(), got.Path)
	if isMachinery(got.Name) {
		t.Fatalf("machinery returned as caller: %+v", got)
	}
}
