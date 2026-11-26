package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain gives the whole test run a private daemon folder. The zzh
// processes started by the integration tests inherit it, so that neither
// the daemon nor the confirmation authorizations (approvals.json, kept in
// the same folder) ever reach the user's real cache folder.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "zzh-cmd-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot create the test daemon folder:", err)
		os.Exit(1)
	}
	os.Setenv("ZZH_DAEMON_DIR", dir)
	os.Setenv("PATH", withoutInstalledShims(os.Getenv("PATH")))

	code := m.Run()

	// Best effort: a leftover temporary folder is harmless.
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// withoutInstalledShims drops from PATH the folders holding an ssh that is a
// link to an installed zzh ("zzh install --shim" on the developer's
// machine): the tests must run as if no zzh were installed, and must never
// start the installed one, which reads the user's configuration and may
// show dialogs.
func withoutInstalledShims(path string) string {
	var kept []string
	for _, dir := range filepath.SplitList(path) {
		if dir != "" && isZzhShimFolder(dir) {
			continue
		}
		kept = append(kept, dir)
	}
	return strings.Join(kept, string(os.PathListSeparator))
}

func isZzhShimFolder(dir string) bool {
	for _, name := range []string{"ssh", "ssh.exe"} {
		link := filepath.Join(dir, name)
		info, err := os.Lstat(link)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		target, err := filepath.EvalSymlinks(link)
		if err != nil {
			continue
		}
		base := strings.ToLower(filepath.Base(target))
		if base == "zzh" || base == "zzh.exe" {
			return true
		}
	}
	return false
}
