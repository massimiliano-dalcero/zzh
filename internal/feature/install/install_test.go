package install

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// isolate makes the commands install into temporary folders and, on
// Windows, write a temporary registry key instead of the user's PATH (see
// isolateRegistry). It returns the fake environment.
func isolate(t *testing.T) environment {
	t.Helper()
	base := t.TempDir()
	env := environment{
		goos:         runtime.GOOS,
		home:         filepath.Join(base, "home"),
		localAppData: filepath.Join(base, "LocalAppData"),
		programFiles: filepath.Join(base, "Program Files"),
	}
	original := currentEnvironment
	currentEnvironment = func() environment { return env }
	t.Cleanup(func() { currentEnvironment = original })

	isolateRegistry(t)
	return env
}

// The per-user installation, the way a user without privileges runs it:
// copy, PATH, upgrade, removal.
func TestInstallAndUninstallForUser(t *testing.T) {
	env := isolate(t)
	l, err := layoutFor(env, false)
	if err != nil {
		t.Fatalf("layout: %v", err)
	}

	var out bytes.Buffer
	if err := RunInstall(nil, &out); err != nil {
		t.Fatalf("install failed: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(l.executable); err != nil {
		t.Fatalf("zzh was not copied to %s: %v", l.executable, err)
	}
	checkUserPath(t, l, true)

	// Running install again (an upgrade from the same binary) works.
	out.Reset()
	if err := RunInstall(nil, &out); err != nil {
		t.Fatalf("second install failed: %v\n%s", err, out.String())
	}

	out.Reset()
	if err := RunUninstall([]string{"--all"}, &out); err != nil {
		t.Fatalf("uninstall failed: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(l.executable); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("zzh must be removed, stat says %v", err)
	}
	checkUserPath(t, l, false)
}

// Per-user shims: refused on Windows, links next to zzh elsewhere.
func TestUserShims(t *testing.T) {
	env := isolate(t)
	l, _ := layoutFor(env, false)
	var out bytes.Buffer

	err := RunInstall([]string{"--shim"}, &out)
	if runtime.GOOS == "windows" {
		if !errors.Is(err, errNoUserShimOnWindows) {
			t.Fatalf("--shim without --system must be refused on Windows, got %v", err)
		}
		return
	}
	if err != nil {
		t.Skipf("cannot install per-user shims here: %v", err)
	}
	for _, program := range shimPrograms {
		if inspectLink(l.shimPath(program), l.executable) != linkToZzh {
			t.Fatalf("missing shim %s", program)
		}
	}

	// zzh cannot be removed while its shims exist.
	if err := RunUninstall([]string{"--all"}, &out); err == nil || !strings.Contains(err.Error(), "--shim --all") {
		t.Fatalf("removing zzh with its shims in place must be refused, got %v", err)
	}
	if err := RunUninstall([]string{"--shim", "--all"}, &out); err != nil {
		t.Fatalf("uninstall failed: %v", err)
	}
	for _, program := range shimPrograms {
		if inspectLink(l.shimPath(program), l.executable) != linkMissing {
			t.Fatalf("shim %s not removed", program)
		}
	}
}

func TestUninstallNeedsSomethingToDo(t *testing.T) {
	isolate(t)
	if err := RunUninstall(nil, &bytes.Buffer{}); err == nil {
		t.Fatalf("uninstall without --shim or --all must explain what to say")
	}
}
