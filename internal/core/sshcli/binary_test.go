package sshcli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// makeFolder creates a folder holding an ssh.exe and the given extra files.
func makeFolder(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range append([]string{"ssh.exe"}, files...) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o755); err != nil {
			t.Fatalf("cannot create %s: %v", name, err)
		}
	}
	return dir
}

// The PATH seen by zzh when git runs it: Git's MSYS folder first.
func TestResolveBinarySkipsMSYS(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the MSYS rule only applies on Windows")
	}

	gitUsrBin := makeFolder(t, "msys-2.0.dll")
	cygwinBin := makeFolder(t, "cygwin1.dll")
	native := makeFolder(t)
	t.Setenv("PATH", strings.Join([]string{gitUsrBin, cygwinBin, native}, string(os.PathListSeparator)))

	got, err := ResolveBinary("ssh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != filepath.Join(native, "ssh.exe") {
		t.Fatalf("expected the native ssh, got %s", got)
	}
}

func TestResolveBinaryFallsBackToSystem32(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the System32 fallback only applies on Windows")
	}
	system32 := filepath.Join(os.Getenv("SystemRoot"), "System32", "OpenSSH", "ssh.exe")
	if _, err := os.Stat(system32); err != nil {
		t.Skip("Windows OpenSSH not installed on this machine")
	}

	t.Setenv("PATH", makeFolder(t, "msys-2.0.dll"))

	got, err := ResolveBinary("ssh")
	if err != nil || !strings.EqualFold(got, system32) {
		t.Fatalf("expected %s, got %s (%v)", system32, got, err)
	}
}

// A path written explicitly in the configuration is the user's choice, even
// if it is an MSYS build.
func TestResolveBinaryKeepsExplicitPath(t *testing.T) {
	dir := makeFolder(t, "msys-2.0.dll")
	explicit := filepath.Join(dir, "ssh.exe")

	got, err := ResolveBinary(explicit)
	if err != nil || got != explicit {
		t.Fatalf("explicit path must be kept: %s (%v)", got, err)
	}
}
