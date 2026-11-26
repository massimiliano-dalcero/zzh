package shim

import (
	"strings"
	"testing"
)

func TestInvokedAs(t *testing.T) {
	t.Setenv(EnvAsSSH, "")

	cases := map[string]string{
		`C:\Program Files\zzh\shim\ssh.exe`: ProgramSSH, // PowerShell: full path
		"ssh":                               ProgramSSH, // cmd, Python, Node
		"SSH.EXE":                           ProgramSSH, // Windows names ignore case
		"/usr/local/bin/scp":                ProgramSCP,
		"sftp":                              ProgramSFTP,
		"zzh":                               "",
		`C:\tools\zzh.exe`:                  "",
		"ssh-keygen":                        "",
	}
	for argv0, expected := range cases {
		if got := InvokedAs(argv0); got != expected {
			t.Errorf("InvokedAs(%q) = %q, expected %q", argv0, got, expected)
		}
	}
}

// scp and sftp run zzh under its own name with EnvAsSSH set: ssh mode, and
// the variable must not survive for the programs zzh starts.
func TestInvokedAsFromEnvironment(t *testing.T) {
	t.Setenv(EnvAsSSH, "1")

	if got := InvokedAs("zzh"); got != ProgramSSH {
		t.Fatalf("with %s set, expected ssh mode, got %q", EnvAsSSH, got)
	}
	if got := InvokedAs("zzh"); got != "" {
		t.Fatalf("%s must be consumed by the first call, got %q", EnvAsSSH, got)
	}
}

func TestEnterNestedStopsLoops(t *testing.T) {
	t.Setenv(EnvDepth, "")

	for i := 0; i < maxDepth; i++ {
		if err := EnterNested(); err != nil {
			t.Fatalf("level %d must be accepted: %v", i+1, err)
		}
	}
	err := EnterNested()
	if err == nil || !strings.Contains(err.Error(), "real ssh found in PATH is zzh") {
		t.Fatalf("a loop must be stopped with a clear error, got %v", err)
	}
}
