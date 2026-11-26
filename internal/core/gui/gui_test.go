package gui

import (
	"os"
	"testing"
	"time"
)

func TestParseAnswer(t *testing.T) {
	cases := map[string]Answer{
		"once\nsecret":         {Choice: AllowOnce, Password: "secret"},
		"remember\nsecret \n":  {Choice: AllowForAWhile, Password: "secret "},
		"remember\r\n":         {Choice: AllowForAWhile},
		"deny\n":               {Choice: Deny},
		"":                     {Choice: Deny},
		"garbage":              {Choice: Deny},
		"once\npass\nwith\nnl": {Choice: AllowOnce, Password: "pass\nwith\nnl"},
	}
	for output, expected := range cases {
		if got := parseAnswer(output); got != expected {
			t.Errorf("parseAnswer(%q) = %+v, expected %+v", output, got, expected)
		}
	}
}

// TestDialogTimesOut shows the real dialog and lets it time out, checking
// the whole chain (helper started, dialog drawn, answer read). It puts a
// window on the screen for two seconds, so it only runs on request:
// ZZH_TEST_GUI=1 go test ./internal/core/gui/
func TestDialogTimesOut(t *testing.T) {
	if os.Getenv("ZZH_TEST_GUI") == "" {
		t.Skip("set ZZH_TEST_GUI=1 to show a real dialog")
	}
	if !Available() {
		t.Skip("no graphical session")
	}
	// ZZH_TEST_GUI_TIMEOUT keeps it open longer, e.g. to look at it.
	timeout := 2 * time.Second
	if custom, err := time.ParseDuration(os.Getenv("ZZH_TEST_GUI_TIMEOUT")); err == nil {
		timeout = custom
	}
	answer, err := Ask(Request{
		Heading:       `zzh test dialog: git.exe (started by Code.exe) wants to connect to "github".`,
		Message:       "It closes by itself. Do not click.",
		Detail:        `Program: C:\Program Files\Git\cmd\git.exe`,
		AskPassword:   true,
		PasswordLabel: `Master password of "default":`,
		RememberLabel: "Allow for 15 minutes",
		Timeout:       timeout,
	})
	if err != nil {
		t.Fatalf("dialog failed: %v", err)
	}
	if answer.Choice != Deny || answer.Password != "" {
		t.Fatalf("a timed out dialog must deny without password, got %+v", answer)
	}
}
