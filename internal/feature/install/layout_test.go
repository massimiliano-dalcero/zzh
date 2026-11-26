package install

import (
	"path/filepath"
	"testing"
)

func TestLayoutFor(t *testing.T) {
	env := environment{
		home:         filepath.Join("home", "max"),
		localAppData: filepath.Join("C:", "Users", "max", "AppData", "Local"),
		programFiles: filepath.Join("C:", "Program Files"),
	}

	cases := []struct {
		name       string
		goos       string
		system     bool
		executable string
		shimDir    string
	}{
		{"windows user", "windows", false, filepath.Join(env.localAppData, "Programs", "zzh", "zzh.exe"), ""},
		{"windows system", "windows", true, filepath.Join(env.programFiles, "zzh", "zzh.exe"), filepath.Join(env.programFiles, "zzh", "shim")},
		{"linux user", "linux", false, filepath.Join(env.home, ".local", "bin", "zzh"), filepath.Join(env.home, ".local", "bin")},
		{"linux system", "linux", true, filepath.Join("/usr/local/bin", "zzh"), "/usr/local/bin"},
		{"macos system", "darwin", true, filepath.Join("/usr/local/bin", "zzh"), "/usr/local/bin"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env.goos = tc.goos
			l, err := layoutFor(env, tc.system)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if l.executable != tc.executable || l.shimDir != tc.shimDir {
				t.Fatalf("got executable %q shims %q, expected %q %q", l.executable, l.shimDir, tc.executable, tc.shimDir)
			}
		})
	}
}

func TestShimPath(t *testing.T) {
	l, _ := layoutFor(environment{goos: "windows", programFiles: filepath.Join("C:", "Program Files")}, true)
	if got := l.shimPath("scp"); got != filepath.Join("C:", "Program Files", "zzh", "shim", "scp.exe") {
		t.Fatalf("unexpected shim path %q", got)
	}
}
