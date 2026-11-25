package askpass

import (
	"errors"
	"strings"
	"testing"
)

// useServer starts a server and points this process's environment at it,
// as ssh would do for the askpass process.
func useServer(t *testing.T, handler Handler) *Server {
	t.Helper()
	server, err := Start(handler)
	if err != nil {
		t.Fatalf("cannot start server: %v", err)
	}
	t.Cleanup(func() { server.Close() })

	for _, variable := range server.Environment("zzh.exe") {
		name, value, _ := strings.Cut(variable, "=")
		t.Setenv(name, value)
	}
	return server
}

func TestAskReceivesAnswer(t *testing.T) {
	var received string
	useServer(t, func(prompt string) (string, error) {
		received = prompt
		return "s3cret", nil
	})

	if !Requested() {
		t.Fatalf("the askpass variables must be detected")
	}

	// Multi-line prompts are what ssh sends for host key confirmations.
	prompt := "The authenticity of host...\nAre you sure (yes/no/[fingerprint])? "
	answer, err := Ask(prompt)
	if err != nil || answer != "s3cret" {
		t.Fatalf("unexpected answer %q (%v)", answer, err)
	}
	if received != prompt {
		t.Fatalf("prompt altered: %q", received)
	}
}

func TestAskRefused(t *testing.T) {
	useServer(t, func(string) (string, error) { return "", ErrRefused })

	if _, err := Ask("password: "); !errors.Is(err, ErrRefused) {
		t.Fatalf("expected ErrRefused, got %v", err)
	}
}

// A client without the right token gets nothing, and the handler is never
// called: this protects the stored password from other local programs.
func TestWrongTokenGetsNothing(t *testing.T) {
	called := false
	useServer(t, func(string) (string, error) {
		called = true
		return "s3cret", nil
	})
	t.Setenv(EnvToken, strings.Repeat("0", 64))

	answer, err := Ask("password: ")
	if err == nil || answer != "" {
		t.Fatalf("a wrong token must be refused, got %q", answer)
	}
	if called {
		t.Fatalf("the handler must not run for a wrong token")
	}
}

func TestEnvironmentForSSH(t *testing.T) {
	server := useServer(t, func(string) (string, error) { return "", nil })

	env := strings.Join(server.Environment(`C:\tools\zzh.exe`), "\n")
	for _, expected := range []string{`SSH_ASKPASS=C:\tools\zzh.exe`, "SSH_ASKPASS_REQUIRE=force", EnvEndpoint + "=", EnvToken + "="} {
		if !strings.Contains(env, expected) {
			t.Errorf("missing %q in:\n%s", expected, env)
		}
	}
}

func TestAskWithoutServer(t *testing.T) {
	t.Setenv(EnvEndpoint, "")
	t.Setenv(EnvToken, "")
	if Requested() {
		t.Fatalf("no askpass request without the variables")
	}
	if _, err := Ask("x"); err == nil {
		t.Fatalf("Ask must fail outside an ssh session started by zzh")
	}
}
