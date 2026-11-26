package install

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	return string(data)
}

func TestCopyExecutable(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "new-zzh")
	target := filepath.Join(dir, "bin", "zzh")
	writeFile(t, source, "v2")

	copied, err := copyExecutable(source, target)
	if err != nil || !copied || readFile(t, target) != "v2" {
		t.Fatalf("first install failed: copied %v, %v", copied, err)
	}

	// Upgrade over an existing file.
	writeFile(t, source, "v3")
	if _, err := copyExecutable(source, target); err != nil || readFile(t, target) != "v3" {
		t.Fatalf("upgrade failed: %v", err)
	}
	if runtime.GOOS == "windows" && readFile(t, target+".old") != "v2" {
		t.Fatalf("on Windows the previous version must be moved aside as .old")
	}
	if _, err := os.Stat(target + ".new"); err == nil {
		t.Fatalf("the temporary file must not be left behind")
	}

	// Installing from the installed file itself copies nothing.
	if copied, err := copyExecutable(target, target); err != nil || copied {
		t.Fatalf("installing over itself must be a no-op: %v %v", copied, err)
	}
}

// symlinkOrSkip creates a link, skipping the test where links need
// privileges the test runner does not have.
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links not available here: %v", err)
	}
}

func TestCreateAndRemoveLink(t *testing.T) {
	dir := t.TempDir()
	zzh := filepath.Join(dir, "zzh")
	writeFile(t, zzh, "zzh")
	link := filepath.Join(dir, "shim", "ssh")

	if err := createLink(link, zzh); err != nil {
		t.Skipf("symbolic links not available here: %v", err)
	}
	if inspectLink(link, zzh) != linkToZzh {
		t.Fatalf("the created link must point to zzh")
	}
	if err := createLink(link, zzh); err != nil {
		t.Fatalf("creating an existing link again must be a no-op: %v", err)
	}

	removed, err := removeLink(link, zzh)
	if err != nil || !removed {
		t.Fatalf("our link must be removed: %v %v", removed, err)
	}
	if removed, err := removeLink(link, zzh); err != nil || removed {
		t.Fatalf("removing a missing link must be a no-op: %v %v", removed, err)
	}
}

// A real program where the shim would go is never touched.
func TestForeignFilesAreLeftAlone(t *testing.T) {
	dir := t.TempDir()
	zzh := filepath.Join(dir, "zzh")
	writeFile(t, zzh, "zzh")

	realSSH := filepath.Join(dir, "ssh")
	writeFile(t, realSSH, "someone's ssh")
	if err := createLink(realSSH, zzh); !errors.Is(err, errForeignFile) {
		t.Fatalf("a real file must not be replaced, got %v", err)
	}
	if _, err := removeLink(realSSH, zzh); !errors.Is(err, errForeignFile) {
		t.Fatalf("a real file must not be removed, got %v", err)
	}
	if readFile(t, realSSH) != "someone's ssh" {
		t.Fatalf("the real file was modified")
	}

	// A link to another program is not ours either.
	other := filepath.Join(dir, "other")
	writeFile(t, other, "other")
	linkElsewhere := filepath.Join(dir, "scp")
	symlinkOrSkip(t, other, linkElsewhere)
	if err := createLink(linkElsewhere, zzh); !errors.Is(err, errForeignFile) {
		t.Fatalf("a link to another program must not be replaced, got %v", err)
	}
}

// A dangling link (zzh moved away) is replaced and can be removed.
func TestDanglingLinkIsReplaced(t *testing.T) {
	dir := t.TempDir()
	zzh := filepath.Join(dir, "zzh")
	writeFile(t, zzh, "zzh")
	link := filepath.Join(dir, "sftp")
	symlinkOrSkip(t, filepath.Join(dir, "gone"), link)

	if err := createLink(link, zzh); err != nil || inspectLink(link, zzh) != linkToZzh {
		t.Fatalf("a dangling link must be replaced by a link to zzh: %v", err)
	}
}
