//go:build windows

package install

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// isolateRegistry points the user environment to a temporary key under
// HKCU\Software, removed at the end of the test: the commands write their
// PATH changes there instead of the real user environment.
func isolateRegistry(t *testing.T) {
	t.Helper()
	path := fmt.Sprintf(`Software\zzh-test-%d`, os.Getpid())
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
	if err != nil {
		t.Fatalf("cannot create test registry key: %v", err)
	}
	key.Close()

	original := userEnvironment
	userEnvironment = envKey{registry.CURRENT_USER, path}
	t.Cleanup(func() {
		userEnvironment = original
		// Best effort: a leftover test key is harmless.
		_ = registry.DeleteKey(registry.CURRENT_USER, path)
	})
}

// checkUserPath verifies whether the zzh folder is in the isolated user PATH.
func checkUserPath(t *testing.T, l layout, expected bool) {
	t.Helper()
	key, err := registry.OpenKey(userEnvironment.root, userEnvironment.path, registry.QUERY_VALUE)
	if err != nil {
		t.Fatalf("cannot open test key: %v", err)
	}
	defer key.Close()
	value, _, _ := key.GetStringValue("Path")

	found := false
	for _, entry := range strings.Split(value, ";") {
		if sameEntry(entry, l.binDir, expandEntry) {
			found = true
		}
	}
	if found != expected {
		t.Fatalf("zzh folder in PATH = %v, expected %v (Path = %q)", found, expected, value)
	}
}

// The system-wide changes, on a temporary key: shim folder before OpenSSH,
// GIT_SSH set, then both removed; a GIT_SSH chosen by the user is kept.
func TestRegisterSystemPathsOnTestKey(t *testing.T) {
	isolateRegistry(t)
	l, _ := layoutFor(environment{goos: "windows", programFiles: `C:\Program Files`}, true)

	// Simulate the default system PATH in the test key.
	if _, err := editPath(userEnvironment, func(string) (string, bool) {
		return `%SystemRoot%\system32;%SYSTEMROOT%\System32\OpenSSH\`, true
	}); err != nil {
		t.Fatalf("cannot prepare Path: %v", err)
	}

	var out strings.Builder
	if err := registerPaths(l, false, true, &out); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	value := readValue(t, "Path")
	shimAt, opensshAt := strings.Index(value, `zzh\shim`), strings.Index(value, "OpenSSH")
	if shimAt < 0 || shimAt > opensshAt || !strings.Contains(value, `Program Files\zzh;`) && !strings.HasSuffix(value, `Program Files\zzh`) {
		t.Fatalf("unexpected Path %q", value)
	}
	if got := readValue(t, gitSSHVariable); got != l.shimPath("ssh") {
		t.Fatalf("GIT_SSH = %q, expected the ssh shim", got)
	}

	if err := unregisterPaths(l, false, true, true, &out); err != nil {
		t.Fatalf("unregister failed: %v", err)
	}
	if value := readValue(t, "Path"); strings.Contains(value, "zzh") {
		t.Fatalf("zzh entries must be removed, Path = %q", value)
	}
	if got := readValue(t, gitSSHVariable); got != "" {
		t.Fatalf("GIT_SSH must be removed, got %q", got)
	}

	// A GIT_SSH set by the user is neither replaced nor removed.
	if err := setVariable(userEnvironment, gitSSHVariable, `C:\tools\plink.exe`); err != nil {
		t.Fatalf("cannot set GIT_SSH: %v", err)
	}
	if err := registerPaths(l, false, true, &out); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if err := unregisterPaths(l, false, true, true, &out); err != nil {
		t.Fatalf("unregister failed: %v", err)
	}
	if got := readValue(t, gitSSHVariable); got != `C:\tools\plink.exe` {
		t.Fatalf("the user's GIT_SSH must be kept, got %q", got)
	}
}

func readValue(t *testing.T, name string) string {
	t.Helper()
	value, _, err := getVariable(userEnvironment, name)
	if err != nil {
		t.Fatalf("cannot read %s: %v", name, err)
	}
	return value
}
