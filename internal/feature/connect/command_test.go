package connect

import (
	"reflect"
	"testing"

	"zzh/internal/core/config"
	"zzh/internal/core/sshcli"
	"zzh/internal/core/vault"
)

func parse(t *testing.T, args ...string) *sshcli.Invocation {
	t.Helper()
	invocation, err := sshcli.ParseInvocation(args)
	if err != nil {
		t.Fatalf("cannot parse %q: %v", args, err)
	}
	return invocation
}

var web01 = vault.Host{
	Name:    "web01",
	Address: "10.0.0.1",
	Port:    2222,
	User:    "deploy",
	Options: []string{"ServerAliveInterval=30"},
}

func TestBuildArgsFromEntry(t *testing.T) {
	sshConfig := config.Defaults().SSH
	sshConfig.ExtraOptions = []string{"ServerAliveInterval=60", "Compression=yes"}

	got := BuildArgs(sshConfig, web01, parse(t, "web01"), false)

	expected := []string{
		"-F", "none",
		"-o", "HostName=10.0.0.1",
		"-o", "User=deploy",
		"-o", "Port=2222",
		// The entry option wins over the config option with the same key.
		"-o", "ServerAliveInterval=30",
		"-o", "Compression=yes",
		"-o", "IdentityFile=none",
		"-o", "IdentityAgent=none",
		"--", "web01",
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("expected\n%q\ngot\n%q", expected, got)
	}
}

func TestBuildArgsUserOptionsWin(t *testing.T) {
	invocation := parse(t, "root@web01", "-p", "22", "-L", "8080:localhost:80", "-o", "serveraliveinterval=5", "uptime")

	got := BuildArgs(config.Defaults().SSH, web01, invocation, false)

	expected := []string{
		"-p", "22",
		"-L", "8080:localhost:80",
		"-o", "serveraliveinterval=5",
		"-F", "none",
		"-o", "HostName=10.0.0.1",
		// root@ beats the stored "deploy"; -p beats the stored port.
		"-o", "User=root",
		"-o", "IdentityFile=none",
		"-o", "IdentityAgent=none",
		"--", "web01", "uptime",
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("expected\n%q\ngot\n%q", expected, got)
	}
}

func TestBuildArgsLoginSwitchBeatsStoredUser(t *testing.T) {
	got := BuildArgs(config.Defaults().SSH, web01, parse(t, "-l", "admin", "web01"), false)

	for _, arg := range got {
		if arg == "User=deploy" {
			t.Fatalf("stored user must not be added when -l is given: %q", got)
		}
	}
}

func TestBuildArgsSystemConfig(t *testing.T) {
	sshConfig := config.Defaults().SSH
	sshConfig.UseSystemConfig = true

	got := BuildArgs(sshConfig, vault.Host{Name: "lab", Address: "lab.local"}, parse(t, "lab"), false)

	expected := []string{"-o", "HostName=lab.local", "-o", "IdentityFile=none", "-o", "IdentityAgent=none", "--", "lab"}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("expected %q, got %q", expected, got)
	}

	// An explicit -F from the user must not be combined with "-F none".
	got = BuildArgs(config.Defaults().SSH, vault.Host{Name: "lab", Address: "lab.local"}, parse(t, "-F", "my_config", "lab"), false)
	expected = []string{"-F", "my_config", "-o", "HostName=lab.local", "-o", "IdentityFile=none", "-o", "IdentityAgent=none", "--", "lab"}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestBuildArgsProtectsDashCommand(t *testing.T) {
	got := BuildArgs(config.Defaults().SSH, vault.Host{Name: "lab", Address: "lab.local"}, parse(t, "lab", "--", "-weird"), false)

	tail := got[len(got)-3:]
	if !reflect.DeepEqual(tail, []string{"--", "lab", "-weird"}) {
		t.Fatalf("remote command must follow \"--\" and the destination: %q", got)
	}
}

func TestBuildArgsSystemKeys(t *testing.T) {
	lab := vault.Host{Name: "lab", Address: "lab.local"}

	// Enabled: ssh keeps its normal key lookup.
	sshConfig := config.Defaults().SSH
	sshConfig.UseSystemKeys = true
	got := BuildArgs(sshConfig, lab, parse(t, "lab"), false)
	if !reflect.DeepEqual(got, []string{"-F", "none", "-o", "HostName=lab.local", "--", "lab"}) {
		t.Fatalf("system keys must not be excluded when enabled: %q", got)
	}

	// Disabled, but the user picks a key file and an agent for this
	// connection: zzh must not add the conflicting "none" values.
	got = BuildArgs(config.Defaults().SSH, lab, parse(t, "-i", "my_key", "-o", "IdentityAgent=SSH_AUTH_SOCK", "lab"), false)
	for _, arg := range got {
		if arg == "IdentityFile=none" || arg == "IdentityAgent=none" {
			t.Fatalf("user choices must win: %q", got)
		}
	}

	// Disabled, but the host entry enables the agent for itself.
	withAgent := vault.Host{Name: "lab", Address: "lab.local", Options: []string{"IdentityAgent=SSH_AUTH_SOCK"}}
	got = BuildArgs(config.Defaults().SSH, withAgent, parse(t, "lab"), false)
	for _, arg := range got {
		if arg == "IdentityAgent=none" {
			t.Fatalf("the host option must win over the global default: %q", got)
		}
	}
}
