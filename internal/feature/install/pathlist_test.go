package install

import (
	"strings"
	"testing"
)

// fakeExpand resolves the variables used in the default Windows PATH.
func fakeExpand(entry string) string {
	return strings.NewReplacer("%SystemRoot%", `C:\Windows`, "%SYSTEMROOT%", `C:\Windows`).Replace(entry)
}

func isOpenSSH(entry string) bool {
	return sameEntry(entry, `C:\Windows\System32\OpenSSH`, fakeExpand)
}

// The default system PATH of Windows, as stored in the registry.
const systemPath = `%SystemRoot%\system32;%SystemRoot%;%SystemRoot%\System32\Wbem;%SYSTEMROOT%\System32\OpenSSH\;C:\Program Files\Git\cmd`

const shimFolder = `C:\Program Files\zzh\shim`

func TestInsertBeforeOpenSSH(t *testing.T) {
	got, changed := insertBefore(systemPath, shimFolder, isOpenSSH, fakeExpand)
	expected := `%SystemRoot%\system32;%SystemRoot%;%SystemRoot%\System32\Wbem;C:\Program Files\zzh\shim;%SYSTEMROOT%\System32\OpenSSH\;C:\Program Files\Git\cmd`
	if !changed || got != expected {
		t.Fatalf("got %q (changed %v), expected %q", got, changed, expected)
	}

	// Running install again changes nothing.
	again, changed := insertBefore(got, shimFolder, isOpenSSH, fakeExpand)
	if changed || again != got {
		t.Fatalf("a second insert must be a no-op, got %q", again)
	}
}

// A shim folder found after OpenSSH (moved by someone, or an old install)
// is moved before it.
func TestInsertBeforeMovesMisplacedEntry(t *testing.T) {
	value := systemPath + `;C:\Program Files\zzh\shim\`
	got, changed := insertBefore(value, shimFolder, isOpenSSH, fakeExpand)
	if !changed || strings.Count(strings.ToLower(got), `zzh\shim`) != 1 ||
		strings.Index(got, `zzh\shim`) > strings.Index(got, `OpenSSH`) {
		t.Fatalf("the shim folder must appear once, before OpenSSH: %q", got)
	}
}

func TestInsertBeforeWithoutOpenSSH(t *testing.T) {
	got, _ := insertBefore(`C:\a;C:\b`, shimFolder, isOpenSSH, fakeExpand)
	if got != shimFolder+`;C:\a;C:\b` {
		t.Fatalf("without OpenSSH in PATH the shim folder goes first, got %q", got)
	}
}

func TestAppendEntry(t *testing.T) {
	got, changed := appendEntry(`C:\a;;C:\b;`, `C:\Program Files\zzh`, fakeExpand)
	if !changed || got != `C:\a;C:\b;C:\Program Files\zzh` {
		t.Fatalf("got %q (changed %v)", got, changed)
	}
	if _, changed := appendEntry(got, `c:\program files\ZZH\`, fakeExpand); changed {
		t.Fatalf("the same folder with other case and a final backslash must be recognized")
	}
}

func TestRemoveEntry(t *testing.T) {
	value := `C:\a;C:\Program Files\zzh\shim\;C:\b;c:\program files\zzh\shim`
	got, changed := removeEntry(value, shimFolder, fakeExpand)
	if !changed || got != `C:\a;C:\b` {
		t.Fatalf("got %q (changed %v)", got, changed)
	}
	if same, changed := removeEntry(`C:\a`, shimFolder, fakeExpand); changed || same != `C:\a` {
		t.Fatalf("removing a missing entry must change nothing")
	}
}
