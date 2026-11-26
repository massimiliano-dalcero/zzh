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
	answer, err := Ask(Request{
		Message:       "zzh test dialog: it closes by itself.\nDo not click.",
		AskPassword:   true,
		PasswordLabel: "Password (test)",
		RememberLabel: "Allow for 15 minutes",
		Timeout:       2 * time.Second,
	})
	if err != nil {
		t.Fatalf("dialog failed: %v", err)
	}
	if answer.Choice != Deny || answer.Password != "" {
		t.Fatalf("a timed out dialog must deny without password, got %+v", answer)
	}
}
