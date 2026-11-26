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

// programName is "ssh.exe" on Windows and "ssh" elsewhere.
func programName() string {
	if runtime.GOOS == "windows" {
		return "ssh.exe"
	}
	return "ssh"
}

// makeProgramFolder creates a folder with an executable ssh for any OS.
func makeProgramFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, programName()), []byte("x"), 0o755); err != nil {
		t.Fatalf("cannot create ssh: %v", err)
	}
	return dir
}

// pretendToBe makes ResolveBinary believe that path is the running zzh.
func pretendToBe(t *testing.T, path string) {
	t.Helper()
	original := selfExecutable
	selfExecutable = func() (string, error) { return path, nil }
	t.Cleanup(func() { selfExecutable = original })
}

// With the shims installed, the first "ssh" in PATH is zzh itself: it must
// be skipped, or zzh would start itself forever.
func TestResolveBinarySkipsItself(t *testing.T) {
	shimFolder := makeProgramFolder(t)
	realFolder := makeProgramFolder(t)
	pretendToBe(t, filepath.Join(shimFolder, programName()))
	t.Setenv("PATH", strings.Join([]string{shimFolder, realFolder}, string(os.PathListSeparator)))

	got, err := ResolveBinary("ssh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != filepath.Join(realFolder, programName()) {
		t.Fatalf("expected the real ssh after the shim, got %s", got)
	}
}

// The shim is a link: it must be recognized through the link too.
func TestResolveBinarySkipsItselfThroughLink(t *testing.T) {
	zzhFolder := t.TempDir()
	zzhFile := filepath.Join(zzhFolder, "zzh")
	if err := os.WriteFile(zzhFile, []byte("x"), 0o755); err != nil {
		t.Fatalf("cannot create zzh: %v", err)
	}
	shimFolder := t.TempDir()
	if err := os.Symlink(zzhFile, filepath.Join(shimFolder, programName())); err != nil {
		t.Skipf("symbolic links not available here: %v", err)
	}
	realFolder := makeProgramFolder(t)
	pretendToBe(t, zzhFile)
	t.Setenv("PATH", strings.Join([]string{shimFolder, realFolder}, string(os.PathListSeparator)))

	got, err := ResolveBinary("ssh")
	if err != nil || got != filepath.Join(realFolder, programName()) {
		t.Fatalf("expected the real ssh after the linked shim, got %s (%v)", got, err)
	}
}

// The shim of another zzh installation is skipped too, or two zzh would
// start each other in a loop.
func TestResolveBinarySkipsOtherZzhShims(t *testing.T) {
	otherZzh := filepath.Join(t.TempDir(), "zzh.exe")
	if err := os.WriteFile(otherZzh, []byte("another zzh"), 0o755); err != nil {
		t.Fatalf("cannot create zzh: %v", err)
	}
	otherShimFolder := t.TempDir()
	if err := os.Symlink(otherZzh, filepath.Join(otherShimFolder, programName())); err != nil {
		t.Skipf("symbolic links not available here: %v", err)
	}
	realFolder := makeProgramFolder(t)
	pretendToBe(t, filepath.Join(t.TempDir(), "this-zzh"))
	t.Setenv("PATH", strings.Join([]string{otherShimFolder, realFolder}, string(os.PathListSeparator)))

	got, err := ResolveBinary("ssh")
	if err != nil || got != filepath.Join(realFolder, programName()) {
		t.Fatalf("expected the real ssh after another zzh's shim, got %s (%v)", got, err)
	}
}

// On Unix a file without execute permission is not a program, as for
// exec.LookPath.
func TestResolveBinarySkipsNonExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("execute permission does not exist on Windows")
	}
	notExecutable := t.TempDir()
	if err := os.WriteFile(filepath.Join(notExecutable, "ssh"), []byte("x"), 0o644); err != nil {
		t.Fatalf("cannot create file: %v", err)
	}
	realFolder := makeProgramFolder(t)
	t.Setenv("PATH", strings.Join([]string{notExecutable, realFolder}, string(os.PathListSeparator)))

	got, err := ResolveBinary("ssh")
	if err != nil || got != filepath.Join(realFolder, "ssh") {
		t.Fatalf("expected the executable ssh, got %s (%v)", got, err)
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
