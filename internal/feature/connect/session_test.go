package connect

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"zzh/internal/core/askpass"
	"zzh/internal/core/config"
	"zzh/internal/core/localipc"
	"zzh/internal/core/sshkey"
	"zzh/internal/core/vault"
)

// fakeSSHCheck runs inside the test binary started as "ssh" by Connect
// (see TestMain). It does what the real ssh would do with the environment
// prepared by zzh, and reports through its exit code:
//
//	0  everything as expected
//	10 IdentityAgent=SSH_AUTH_SOCK missing from the arguments
//	11 agent unreachable or not holding exactly the expected key
//	12 first password question not answered with the KeePass password
//	13 second password question not relayed to the user
func fakeSSHCheck() int {
	if !slices.Contains(os.Args[1:], "IdentityAgent=SSH_AUTH_SOCK") {
		return 10
	}

	conn, err := localipc.Dial(os.Getenv("SSH_AUTH_SOCK"), 2*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent:", err)
		return 11
	}
	keys, err := agent.NewClient(conn).List()
	conn.Close()
	if err != nil || len(keys) != 1 || ssh.FingerprintSHA256(keys[0]) != os.Getenv("ZZH_FAKE_EXPECT_FP") {
		return 11
	}

	// ssh would run "zzh <question>"; the test binary asks directly.
	first, err := askpass.Ask("max@dd.lan's password: ")
	if err != nil || first != "stored-password" {
		return 12
	}
	second, err := askpass.Ask("max@dd.lan's password: ")
	if err != nil || second != "typed-by-user" {
		return 13
	}
	return 0
}

func TestConnectServesKeyAndPassword(t *testing.T) {
	pair, err := sshkey.Generate(sshkey.TypeEd25519, 0, "ddkey")
	if err != nil {
		t.Fatalf("cannot generate key: %v", err)
	}
	t.Setenv("ZZH_FAKE_SSH_CHECK", "1")
	t.Setenv("ZZH_FAKE_EXPECT_FP", pair.Fingerprint)

	sshConfig := config.Defaults().SSH
	sshConfig.Binary = os.Args[0]

	invocation := parse(t, "dd.lan")

	err = Connect(Request{
		SSH:        sshConfig,
		Host:       vault.Host{Name: "dd.lan", Address: "dd.lan", User: "max", Password: "stored-password", KeyName: "ddkey"},
		Invocation: invocation,
		PrivateKey: pair.PrivatePEM,
		Executable: os.Args[0],
		AskHidden:  func(string) (string, error) { return "typed-by-user", nil },
		AskVisible: func(string) (string, error) { return "yes", nil },
	})
	if err != nil {
		t.Fatalf("connection checks failed: %v", err)
	}
}

func TestAnswerer(t *testing.T) {
	var asked []string
	a := &answerer{
		password: "stored",
		askHidden: func(message string) (string, error) {
			asked = append(asked, "hidden:"+message)
			return "secret", nil
		},
		askVisible: func(message string) (string, error) {
			asked = append(asked, "visible:"+message)
			return "yes", nil
		},
	}

	cases := []struct {
		prompt string
		answer string
	}{
		// Host key confirmation: shown to the user, never answered alone.
		{"Are you sure you want to continue connecting (yes/no/[fingerprint])? ", "yes"},
		{"max@dd.lan's password: ", "stored"},
		// Asked again: the stored password was wrong, the user decides.
		{"max@dd.lan's password: ", "secret"},
		{"Verification code: ", "secret"},
	}
	for _, tc := range cases {
		answer, err := a.answer(tc.prompt)
		if err != nil || answer != tc.answer {
			t.Fatalf("%q: expected %q, got %q (%v)", tc.prompt, tc.answer, answer, err)
		}
	}
	if len(asked) != 3 || asked[0][:8] != "visible:" {
		t.Fatalf("unexpected questions to the user: %q", asked)
	}
}

func TestAnswererWithoutStoredPassword(t *testing.T) {
	a := &answerer{
		askHidden:  func(string) (string, error) { return "", errors.New("Ctrl+C") },
		askVisible: func(string) (string, error) { return "", nil },
	}

	_, err := a.answer("Password: ")
	if !errors.Is(err, askpass.ErrRefused) {
		t.Fatalf("a cancelled question must be a refusal, got %v", err)
	}
}

func TestBuildArgsWithAgent(t *testing.T) {
	invocation := parse(t, "dd.lan")
	got := BuildArgs(config.Defaults().SSH, vault.Host{Name: "dd.lan", Address: "dd.lan"}, invocation, true)

	// The agent must be used, and must not be overridden by the "none"
	// that excludes the system agent.
	if !slices.Contains(got, "IdentityAgent=SSH_AUTH_SOCK") || slices.Contains(got, "IdentityAgent=none") {
		t.Fatalf("unexpected agent options: %q", got)
	}
	if !slices.Contains(got, "IdentityFile=none") {
		t.Fatalf("the default key files must still be excluded: %q", got)
	}
}
