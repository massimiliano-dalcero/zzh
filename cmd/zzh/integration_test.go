package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"zzh/internal/core/config"
	"zzh/internal/core/sshkey"
	"zzh/internal/core/vault"
	"zzh/internal/core/vault/vaulttest"
)

// This file tests the whole chain with real programs: the zzh binary built
// from this package, the system OpenSSH client, the KeePass key served by
// the zzh agent and the password typed by the zzh askpass. The ssh server
// is a minimal one written here, so the test never touches a real server
// or the accounts of the machine running it.

const testHostPassword = "stored-password"

// realSSH returns an OpenSSH client usable by zzh, or skips the test.
func realSSH(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test skipped in -short mode")
	}

	if runtime.GOOS == "windows" {
		// Only the native Windows build works with zzh (D-003); the ssh of
		// Git for Windows may come first in PATH.
		native := filepath.Join(os.Getenv("SystemRoot"), "System32", "OpenSSH", "ssh.exe")
		if _, err := os.Stat(native); err != nil {
			t.Skip("native Windows OpenSSH not installed")
		}
		return native
	}

	path, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("ssh not installed")
	}
	return path
}

// startTestServer runs an SSH server on 127.0.0.1 that accepts the given
// public key or the test password, and answers every command with the
// authentication method that was used ("publickey" or "password").
func startTestServer(t *testing.T, allowedKey ssh.PublicKey) int {
	t.Helper()

	_, hostPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("cannot generate host key: %v", err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPrivate)
	if err != nil {
		t.Fatalf("cannot create host signer: %v", err)
	}

	serverConfig := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) == string(allowedKey.Marshal()) {
				return &ssh.Permissions{Extensions: map[string]string{"method": "publickey"}}, nil
			}
			return nil, fmt.Errorf("unknown key")
		},
		PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if string(password) == testHostPassword {
				return &ssh.Permissions{Extensions: map[string]string{"method": "password"}}, nil
			}
			return nil, fmt.Errorf("wrong password")
		},
	}
	serverConfig.AddHostKey(hostSigner)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("cannot listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveTestConnection(conn, serverConfig)
		}
	}()

	return listener.Addr().(*net.TCPAddr).Port
}

func serveTestConnection(conn net.Conn, serverConfig *ssh.ServerConfig) {
	defer conn.Close()
	serverConn, channels, requests, err := ssh.NewServerConn(conn, serverConfig)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(requests)

	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "only sessions")
			continue
		}
		channel, channelRequests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		for request := range channelRequests {
			if request.Type != "exec" {
				request.Reply(false, nil)
				continue
			}
			request.Reply(true, nil)
			fmt.Fprintf(channel, "authenticated by %s\n", serverConn.Permissions.Extensions["method"])
			channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
			channel.Close()
			break
		}
	}
}

// buildZzh compiles this package into a temporary executable.
func buildZzh(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "zzh")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("cannot build zzh: %v\n%s", err, output)
	}
	return binary
}

func TestIntegrationKeyAndPasswordWithRealSSH(t *testing.T) {
	sshBinary := realSSH(t)

	pair, err := sshkey.Generate(sshkey.TypeEd25519, 0, "integration")
	if err != nil {
		t.Fatalf("cannot generate key: %v", err)
	}
	publicKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pair.PublicLine))
	if err != nil {
		t.Fatalf("cannot parse public key: %v", err)
	}
	port := startTestServer(t, publicKey)

	// A database with one host using the key and one using the password.
	dbPath := vaulttest.NewDatabase(t)
	err = vaulttest.Modify(dbPath, func(v *vault.Vault) error {
		if err := v.AddKey(vault.NewKey{Name: "integration", Type: pair.Type, PublicKey: pair.PublicLine,
			Fingerprint: pair.Fingerprint, PrivatePEM: pair.PrivatePEM}); err != nil {
			return err
		}
		if err := v.AddHost(vault.NewHost{Name: "with-key", Address: "127.0.0.1", Port: port, User: "tester", KeyName: "integration"}); err != nil {
			return err
		}
		return v.AddHost(vault.NewHost{Name: "with-password", Address: "127.0.0.1", Port: port, User: "tester", Password: testHostPassword})
	})
	if err != nil {
		t.Fatalf("cannot prepare database: %v", err)
	}

	// The test server has a new host key each run: accept it into a
	// throw-away known_hosts instead of the user's.
	cfg := config.Defaults()
	cfg.Databases = map[string]config.Database{"test": {Path: dbPath}}
	cfg.WarnEnvPassword = false
	cfg.SSH.Binary = sshBinary
	cfg.SSH.ExtraOptions = []string{
		"StrictHostKeyChecking=no",
		"UserKnownHostsFile=" + filepath.ToSlash(filepath.Join(t.TempDir(), "known_hosts")),
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("cannot encode config: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatalf("cannot write config: %v", err)
	}

	zzh := buildZzh(t)

	for host, method := range map[string]string{"with-key": "publickey", "with-password": "password"} {
		t.Run(host, func(t *testing.T) {
			command := exec.Command(zzh, host, "whoami")
			command.Env = append(os.Environ(),
				"ZZH_CONFIG="+configPath,
				"ZZH_PASSWORD="+vaulttest.Password,
				// A system agent must not be involved.
				"SSH_AUTH_SOCK=",
			)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("zzh %s failed: %v\n%s", host, err, output)
			}
			expected := "authenticated by " + method
			if !strings.Contains(string(output), expected) {
				t.Fatalf("expected %q, got:\n%s", expected, output)
			}
		})
	}

	// Sanity check of the test itself: the server really refuses a client
	// without key and password, so the successes above are meaningful.
	command := exec.Command(sshBinary, "-F", "none", "-o", "BatchMode=yes",
		"-o", "IdentityFile=none", "-o", "IdentityAgent=none",
		"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile="+filepath.ToSlash(filepath.Join(t.TempDir(), "kh")),
		"-p", strconv.Itoa(port), "tester@127.0.0.1", "whoami")
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("the test server accepted an unauthenticated client:\n%s", output)
	}
}

// A target that is not in KeePass is handed to the plain ssh as typed. The
// test uses an option that only ssh accepts in that position ("-G" prints
// the configuration and exits), so a success proves ssh received the
// arguments unchanged.
func TestIntegrationPassthroughUnknownHost(t *testing.T) {
	sshBinary := realSSH(t)

	cfg := config.Defaults()
	cfg.Databases = map[string]config.Database{"test": {Path: vaulttest.NewDatabase(t)}}
	cfg.WarnEnvPassword = false
	cfg.SSH.Binary = sshBinary
	configPath := filepath.Join(t.TempDir(), "config.json")
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("cannot encode config: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatalf("cannot write config: %v", err)
	}

	command := exec.Command(buildZzh(t), "-G", "-p", "2222", "someone@192.0.2.10")
	command.Env = append(os.Environ(), "ZZH_CONFIG="+configPath, "ZZH_PASSWORD="+vaulttest.Password)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("passthrough failed: %v\n%s", err, output)
	}

	text := string(output)
	for _, expected := range []string{"not in KeePass", "hostname 192.0.2.10", "user someone", "port 2222"} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q in:\n%s", expected, text)
		}
	}
}

// The real daemon: "zzh unlock" starts a detached "zzh daemon run" process,
// after which commands work without ZZH_PASSWORD and without a terminal
// (as under git); "zzh lock" makes them refuse again.
func TestIntegrationDaemonKeepsPassword(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in -short mode")
	}

	cfg := config.Defaults()
	cfg.Databases = map[string]config.Database{"test": {Path: vaulttest.NewDatabase(t)}}
	cfg.WarnEnvPassword = false
	configPath := filepath.Join(t.TempDir(), "config.json")
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("cannot encode config: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatalf("cannot write config: %v", err)
	}

	zzh := buildZzh(t)
	daemonDir := t.TempDir()
	baseEnv := append(os.Environ(), "ZZH_CONFIG="+configPath, "ZZH_DAEMON_DIR="+daemonDir, "ZZH_PASSWORD=")

	runZzh := func(env []string, args ...string) (string, error) {
		command := exec.Command(zzh, args...)
		command.Env = env
		output, err := command.CombinedOutput()
		return string(output), err
	}
	t.Cleanup(func() { runZzh(baseEnv, "daemon", "stop") })

	// Unlock with the password from the environment (there is no terminal).
	if output, err := runZzh(append(baseEnv, "ZZH_PASSWORD="+vaulttest.Password), "unlock"); err != nil {
		t.Fatalf("unlock failed: %v\n%s", err, output)
	}

	// From now on no password source at all: only the daemon.
	if output, err := runZzh(baseEnv, "ls"); err != nil {
		t.Fatalf("ls through the daemon failed: %v\n%s", err, output)
	}
	if output, err := runZzh(baseEnv, "status"); err != nil || !strings.Contains(output, "test") {
		t.Fatalf("status must list the database: %v\n%s", err, output)
	}

	if output, err := runZzh(baseEnv, "lock"); err != nil {
		t.Fatalf("lock failed: %v\n%s", err, output)
	}
	output, err := runZzh(baseEnv, "ls")
	if err == nil || !strings.Contains(output, "zzh unlock") {
		t.Fatalf("after lock, ls must refuse and suggest unlock: %v\n%s", err, output)
	}
}

// "--key" picks a KeePass key for one connection: the test server accepts
// only the key "other", while the host is associated with "assigned".
func TestIntegrationKeyOverride(t *testing.T) {
	sshBinary := realSSH(t)

	newKey := func(name string) sshkey.Pair {
		pair, err := sshkey.Generate(sshkey.TypeEd25519, 0, name)
		if err != nil {
			t.Fatalf("cannot generate key: %v", err)
		}
		return pair
	}
	assigned, other := newKey("assigned"), newKey("other")

	allowed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(other.PublicLine))
	if err != nil {
		t.Fatalf("cannot parse public key: %v", err)
	}
	port := startTestServer(t, allowed)

	dbPath := vaulttest.NewDatabase(t)
	err = vaulttest.Modify(dbPath, func(v *vault.Vault) error {
		for name, pair := range map[string]sshkey.Pair{"assigned": assigned, "other": other} {
			if err := v.AddKey(vault.NewKey{Name: name, Type: pair.Type, PublicKey: pair.PublicLine,
				Fingerprint: pair.Fingerprint, PrivatePEM: pair.PrivatePEM}); err != nil {
				return err
			}
		}
		return v.AddHost(vault.NewHost{Name: "with-key", Address: "127.0.0.1", Port: port, User: "tester", KeyName: "assigned"})
	})
	if err != nil {
		t.Fatalf("cannot prepare database: %v", err)
	}

	knownHosts := filepath.ToSlash(filepath.Join(t.TempDir(), "known_hosts"))
	cfg := config.Defaults()
	cfg.Databases = map[string]config.Database{"test": {Path: dbPath}}
	cfg.WarnEnvPassword = false
	cfg.SSH.Binary = sshBinary
	cfg.SSH.ExtraOptions = []string{"StrictHostKeyChecking=no", "UserKnownHostsFile=" + knownHosts, "BatchMode=yes"}
	configPath := filepath.Join(t.TempDir(), "config.json")
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("cannot encode config: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatalf("cannot write config: %v", err)
	}

	zzh := buildZzh(t)
	run := func(args ...string) (string, error) {
		command := exec.Command(zzh, args...)
		command.Env = append(os.Environ(), "ZZH_CONFIG="+configPath, "ZZH_PASSWORD="+vaulttest.Password, "SSH_AUTH_SOCK=")
		output, err := command.CombinedOutput()
		return string(output), err
	}

	// The associated key is refused by the server.
	if output, err := run("with-key", "whoami"); err == nil {
		t.Fatalf("the associated key must be refused by the test server:\n%s", output)
	}

	// --key replaces it for this connection.
	output, err := run("with-key", "--key", "other", "whoami")
	if err != nil || !strings.Contains(output, "authenticated by publickey") {
		t.Fatalf("--key on a KeePass host failed: %v\n%s", err, output)
	}

	// --key also works for a host that is not in KeePass (plain ssh). The
	// target is "localhost", not "127.0.0.1": that address belongs to the
	// with-key entry and would be found by address.
	output, err = run("-F", "none", "-p", strconv.Itoa(port),
		"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile="+knownHosts,
		"-o", "BatchMode=yes", "-o", "IdentityFile=none",
		"-4", "tester@localhost", "--key", "other", "whoami")
	if err != nil || !strings.Contains(output, "authenticated by publickey") || !strings.Contains(output, "not in KeePass") {
		t.Fatalf("--key on an unknown host failed: %v\n%s", err, output)
	}

	// The association stored in KeePass is untouched.
	v, err := vaulttest.Open(dbPath)
	if err != nil {
		t.Fatalf("cannot reopen database: %v", err)
	}
	if host, _ := v.FindHost("with-key"); host.KeyName != "assigned" {
		t.Fatalf("--key must not change the host entry, got key %q", host.KeyName)
	}
}
