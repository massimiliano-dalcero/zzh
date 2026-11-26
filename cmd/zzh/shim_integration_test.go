package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"zzh/internal/core/config"
	"zzh/internal/core/vault"
	"zzh/internal/core/vault/vaulttest"
)

// These tests run the real zzh binary through real symbolic links named ssh
// and scp, placed first in PATH as "zzh install --shim" does (D-055). zzh is
// built in a folder whose name contains a space, like C:\Program Files\zzh,
// because that path is handed to scp with -S.

// shimSetup builds zzh, links the given program names to it and returns the
// folder of the links and an environment with that folder first in PATH.
func shimSetup(t *testing.T, programs ...string) (string, []string) {
	t.Helper()
	realSSH(t) // skips when no usable ssh client is installed

	zzhFolder := filepath.Join(t.TempDir(), "with space")
	if err := os.MkdirAll(zzhFolder, 0o755); err != nil {
		t.Fatalf("cannot create folder: %v", err)
	}
	built := buildZzh(t)
	zzh := filepath.Join(zzhFolder, filepath.Base(built))
	if err := os.Rename(built, zzh); err != nil {
		t.Fatalf("cannot move zzh: %v", err)
	}

	shimFolder := t.TempDir()
	for _, program := range programs {
		name := program
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if err := os.Symlink(zzh, filepath.Join(shimFolder, name)); err != nil {
			t.Skipf("symbolic links not available (on Windows they need administrator rights or Developer Mode): %v", err)
		}
	}

	env := append(os.Environ(),
		"PATH="+shimFolder+string(os.PathListSeparator)+os.Getenv("PATH"),
		"ZZH_PASSWORD="+vaulttest.Password,
		"SSH_AUTH_SOCK=",
	)
	return shimFolder, env
}

// writeConfig stores cfg in a temporary file and returns its path.
func writeConfig(t *testing.T, cfg config.Config) string {
	t.Helper()
	cfg.WarnEnvPassword = false
	path := filepath.Join(t.TempDir(), "config.json")
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("cannot encode config: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("cannot write config: %v", err)
	}
	return path
}

// runLink runs one of the links by its path, as a shell would after
// finding it in PATH.
func runLink(t *testing.T, shimFolder, program string, env []string, args ...string) string {
	t.Helper()
	name := program
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	command := exec.Command(filepath.Join(shimFolder, name), args...)
	command.Env = env
	output, _ := command.CombinedOutput() // the exit code is checked through the output
	return string(output)
}

func TestIntegrationShimSSH(t *testing.T) {
	shimFolder, env := shimSetup(t, "ssh")

	dbPath := vaulttest.NewDatabase(t)
	err := vaulttest.Modify(dbPath, func(v *vault.Vault) error {
		return v.AddHost(vault.NewHost{Name: "stored", Address: "192.0.2.20", Port: 2200, User: "stored-user"})
	})
	if err != nil {
		t.Fatalf("cannot prepare database: %v", err)
	}
	cfg := config.Defaults()
	cfg.Databases = map[string]config.Database{"test": {Path: dbPath}}
	withDatabase := append(env, "ZZH_CONFIG="+writeConfig(t, cfg))

	// "ssh -V" is no connection: the real ssh answers. This also proves
	// that zzh found the real ssh after skipping its own link in PATH.
	if output := runLink(t, shimFolder, "ssh", withDatabase, "-V"); !strings.Contains(output, "OpenSSH") {
		t.Errorf("ssh -V must reach the real ssh, got:\n%s", output)
	}

	// A host stored in KeePass gets its data from KeePass ("-G" prints the
	// configuration ssh would use and exits).
	output := runLink(t, shimFolder, "ssh", withDatabase, "-G", "stored")
	for _, expected := range []string{"hostname 192.0.2.20", "user stored-user", "port 2200"} {
		if !strings.Contains(output, expected) {
			t.Errorf("stored host: missing %q in:\n%s", expected, output)
		}
	}

	// An unknown host runs the plain ssh without the notice printed by
	// "zzh <host>": as a shim zzh must be invisible.
	output = runLink(t, shimFolder, "ssh", withDatabase, "-G", "someone@192.0.2.10")
	if !strings.Contains(output, "hostname 192.0.2.10") || strings.Contains(output, "zzh") {
		t.Errorf("unknown host: expected silent plain ssh, got:\n%s", output)
	}

	// "ssh ls" is the host "ls", never the zzh subcommand.
	output = runLink(t, shimFolder, "ssh", withDatabase, "-G", "ls")
	if !strings.Contains(output, "hostname ls") {
		t.Errorf("ssh ls must mean the host ls, got:\n%s", output)
	}

	// "ssh --zzh <args>" is zzh itself.
	output = runLink(t, shimFolder, "ssh", withDatabase, "--zzh", "ls", "--grep")
	if !strings.HasPrefix(output, "stored\t") || !strings.Contains(output, "192.0.2.20:2200") {
		t.Errorf("ssh --zzh ls must list the hosts as zzh ls, got:\n%s", output)
	}
	output = runLink(t, shimFolder, "ssh", withDatabase, "--zzh", "version")
	if !strings.HasPrefix(output, "zzh v") {
		t.Errorf("ssh --zzh version must print the zzh version, got:\n%s", output)
	}

	// A database that cannot be unlocked (here a wrong ZZH_PASSWORD) leaves
	// the plain ssh to do its job, with one notice.
	wrongPassword := append(withDatabase, "ZZH_PASSWORD=wrong")
	output = runLink(t, shimFolder, "ssh", wrongPassword, "-G", "stored")
	if !strings.Contains(output, "running plain ssh") || !strings.Contains(output, "hostname stored") {
		t.Errorf("wrong password: expected a notice and the plain ssh, got:\n%s", output)
	}

	// The three bypasses (D-060) run the real ssh at once: the stored host
	// is then just a name, with no data from KeePass and no word from zzh.
	disabled := cfg
	disabled.Shim.Enabled = false
	bypasses := map[string]struct {
		env  []string
		args []string
	}{
		"ssh - <args>":       {withDatabase, []string{"-", "-G", "stored"}},
		"ZZH_SHIM=off":       {append(withDatabase, "ZZH_SHIM=off"), []string{"-G", "stored"}},
		"shim.enabled false": {append(env, "ZZH_CONFIG="+writeConfig(t, disabled)), []string{"-G", "stored"}},
	}
	for name, bypass := range bypasses {
		output := runLink(t, shimFolder, "ssh", bypass.env, bypass.args...)
		if !strings.Contains(output, "hostname stored") || strings.Contains(output, "192.0.2.20") || strings.Contains(output, "zzh") {
			t.Errorf("%s: expected the plain ssh, got:\n%s", name, output)
		}
	}

	// A user who never set up zzh: plain ssh, not a word from zzh.
	withoutDatabase := append(env, "ZZH_CONFIG="+writeConfig(t, config.Defaults()))
	output = runLink(t, shimFolder, "ssh", withoutDatabase, "-G", "stored")
	if !strings.Contains(output, "hostname stored") || strings.Contains(output, "zzh") {
		t.Errorf("without database: expected silent plain ssh, got:\n%s", output)
	}
}

// scp through the shim runs the real scp with "-S <zzh>", and that zzh acts
// as ssh. The target is an unroutable documentation address, so the chain
// ends with the real ssh failing to connect, which is the proof that every
// step ran.
func TestIntegrationShimSCP(t *testing.T) {
	shimFolder, env := shimSetup(t, "scp")
	env = append(env, "ZZH_CONFIG="+writeConfig(t, config.Defaults()))

	output := runLink(t, shimFolder, "scp", env,
		"-v", "-o", "ConnectTimeout=2", "-o", "BatchMode=yes", "nobody@192.0.2.1:file", t.TempDir())

	if !strings.Contains(output, "Executing: program") || !strings.Contains(output, "with space") {
		t.Errorf("scp must run zzh (from the folder with a space) as its ssh, got:\n%s", output)
	}
	if !strings.Contains(output, "192.0.2.1") || !strings.Contains(strings.ToLower(output), "timed out") {
		t.Errorf("the real ssh started by zzh must have tried to connect, got:\n%s", output)
	}

	// With ZZH_SHIM=off the real scp runs alone, with its own ssh.
	output = runLink(t, shimFolder, "scp", append(env, "ZZH_SHIM=off"),
		"-v", "-o", "ConnectTimeout=2", "-o", "BatchMode=yes", "nobody@192.0.2.1:file", t.TempDir())
	if !strings.Contains(output, "Executing: program") || strings.Contains(output, "with space") {
		t.Errorf("with ZZH_SHIM=off scp must use its own ssh, got:\n%s", output)
	}
}
