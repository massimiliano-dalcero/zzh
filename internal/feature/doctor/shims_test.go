package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

// A link to a file named zzh is a shim even when it is not the running zzh
// (doctor started from a downloaded copy); a link to anything else is not.
func TestShimTarget(t *testing.T) {
	dir := t.TempDir()
	zzh := filepath.Join(dir, "zzh.exe")
	other := filepath.Join(dir, "real-ssh.exe")
	for _, file := range []string{zzh, other} {
		if err := os.WriteFile(file, []byte("x"), 0o755); err != nil {
			t.Fatalf("cannot create %s: %v", file, err)
		}
	}
	shimLink := filepath.Join(dir, "ssh-shim")
	otherLink := filepath.Join(dir, "ssh-other")
	if err := os.Symlink(zzh, shimLink); err != nil {
		t.Skipf("symbolic links not available here: %v", err)
	}
	if err := os.Symlink(other, otherLink); err != nil {
		t.Skipf("symbolic links not available here: %v", err)
	}

	if got := shimTarget(shimLink); got == "" || filepath.Base(got) != "zzh.exe" {
		t.Fatalf("a link to zzh must be recognized, got %q", got)
	}
	if got := shimTarget(otherLink); got != "" {
		t.Fatalf("a link to another program is not a shim, got %q", got)
	}
	if got := shimTarget(other); got != "" {
		t.Fatalf("a plain program is not a shim, got %q", got)
	}
}
