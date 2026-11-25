package connect

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"
)

// The test binary doubles as a fake ssh: when ZZH_FAKE_SSH_EXIT is set,
// TestMain behaves like an ssh process that prints its arguments and exits
// with the requested code. This tests the real process handling without
// depending on an ssh server.
func TestMain(m *testing.M) {
	if code := os.Getenv("ZZH_FAKE_SSH_EXIT"); code != "" {
		fmt.Println(os.Args[1:])
		exitCode, err := strconv.Atoi(code)
		if err != nil {
			os.Exit(99)
		}
		os.Exit(exitCode)
	}
	if os.Getenv("ZZH_FAKE_SSH_CHECK") != "" {
		os.Exit(fakeSSHCheck())
	}
	os.Exit(m.Run())
}

func TestRunPropagatesExitCode(t *testing.T) {
	t.Setenv("ZZH_FAKE_SSH_EXIT", "3")

	err := Run(os.Args[0], []string{"--", "web01"}, nil)

	var status ExitStatus
	if !errors.As(err, &status) || status.Code != 3 {
		t.Fatalf("expected ExitStatus{3}, got %v", err)
	}
}

func TestRunSuccess(t *testing.T) {
	t.Setenv("ZZH_FAKE_SSH_EXIT", "0")

	if err := Run(os.Args[0], []string{"--", "web01"}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunMissingBinary(t *testing.T) {
	err := Run("zzh-no-such-ssh-binary", nil, nil)

	var status ExitStatus
	if err == nil || errors.As(err, &status) {
		t.Fatalf("a missing binary must be a plain error, got %v", err)
	}
}
