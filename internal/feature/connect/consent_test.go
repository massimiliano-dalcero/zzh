package connect

import (
	"errors"
	"strings"
	"testing"
	"time"

	"zzh/internal/core/approval"
	"zzh/internal/core/caller"
	"zzh/internal/core/config"
	"zzh/internal/core/gui"
)

// fakeConsent returns a Consent whose dialog answers with answer and counts
// how many times it was shown; nothing real is ever displayed.
func fakeConsent(t *testing.T, mode string, interactive bool, answer gui.Answer) (*Consent, *int) {
	t.Helper()
	shown := 0
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	return &Consent{
		Mode:        mode,
		RememberFor: 15 * time.Minute,
		Interactive: interactive,
		Target:      "github.com",
		Approvals:   approval.NewStore(t.TempDir()),
		Identify: func() caller.Caller {
			return caller.Caller{Name: "git.exe", Path: `C:\Git\cmd\git.exe`, Parent: "Code.exe"}
		},
		Ask: func(request gui.Request) (gui.Answer, error) {
			shown++
			return answer, nil
		},
		Confirm: func(string) (bool, error) { return true, nil },
		Now:     func() time.Time { return now },
	}, &shown
}

func TestConsentModes(t *testing.T) {
	cases := []struct {
		mode        string
		interactive bool
		asks        bool
	}{
		{"never", false, false},
		{"background", true, false}, // the user typed the command
		{"background", false, true}, // a program in the background
		{"always", false, true},
	}
	for _, tc := range cases {
		c, shown := fakeConsent(t, tc.mode, tc.interactive, gui.Answer{Choice: gui.AllowOnce})
		if err := c.Check("github"); err != nil {
			t.Fatalf("%s/%v: unexpected error %v", tc.mode, tc.interactive, err)
		}
		if (*shown > 0) != tc.asks {
			t.Errorf("%s, interactive %v: dialog shown %d times", tc.mode, tc.interactive, *shown)
		}
	}
}

func TestConsentDenyAndTimeout(t *testing.T) {
	c, _ := fakeConsent(t, "background", false, gui.Answer{Choice: gui.Deny})
	if err := c.Check("github"); !errors.Is(err, ErrDenied) {
		t.Fatalf("deny must return ErrDenied, got %v", err)
	}
}

// "Allow for a while" is remembered for the program and the host only.
func TestConsentRemembers(t *testing.T) {
	c, shown := fakeConsent(t, "background", false, gui.Answer{Choice: gui.AllowForAWhile})
	if err := c.Check("github"); err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if err := c.Check("github"); err != nil || *shown != 1 {
		t.Fatalf("the second request must use the authorization, dialog shown %d times (%v)", *shown, err)
	}
	if err := c.Check("prod-db"); err != nil || *shown != 2 {
		t.Fatalf("another host must be asked again, dialog shown %d times", *shown)
	}
}

// The unlock dialog asks the password and counts as the confirmation.
func TestConsentUnlockDialog(t *testing.T) {
	c, shown := fakeConsent(t, "background", false, gui.Answer{Choice: gui.AllowForAWhile, Password: "master"})
	var request gui.Request
	c.Ask = func(r gui.Request) (gui.Answer, error) {
		*shown++
		request = r
		return gui.Answer{Choice: gui.AllowForAWhile, Password: "master"}, nil
	}

	password, err := c.UnlockDialog(config.ResolvedDatabase{Name: "personal"})
	if err != nil || password != "master" {
		t.Fatalf("unexpected result %q %v", password, err)
	}
	if !request.AskPassword || !strings.Contains(request.Heading, "git.exe (started by Code.exe)") ||
		!strings.Contains(request.Heading, `"github.com"`) || request.RememberLabel != "Allow for 15 minutes" {
		t.Fatalf("unexpected request %+v", request)
	}

	// No second dialog for the same connection, and the authorization is
	// stored because the user chose "for a while".
	if err := c.Check("github"); err != nil || *shown != 1 {
		t.Fatalf("the unlock approval must count as confirmation (shown %d, %v)", *shown, err)
	}
	if !c.Approvals.Allowed(`C:\Git\cmd\git.exe`, "github", c.Now()) {
		t.Fatalf("the authorization must be stored")
	}
}

func TestConsentUnlockDenied(t *testing.T) {
	for _, answer := range []gui.Answer{{Choice: gui.Deny}, {Choice: gui.AllowOnce}} { // AllowOnce without password
		c, _ := fakeConsent(t, "background", false, answer)
		if _, err := c.UnlockDialog(config.ResolvedDatabase{Name: "personal"}); !errors.Is(err, ErrDenied) {
			t.Fatalf("answer %+v must deny, got %v", answer, err)
		}
	}
}

// Nobody to ask: the connection does not get the KeePass data.
func TestConsentWithoutAnyWayToAsk(t *testing.T) {
	c, _ := fakeConsent(t, "background", false, gui.Answer{})
	c.Ask = func(gui.Request) (gui.Answer, error) { return gui.Answer{}, gui.ErrUnavailable }
	if err := c.Check("github"); err == nil || !strings.Contains(err.Error(), "confirm.mode never") {
		t.Fatalf("expected an explanation pointing to confirm.mode, got %v", err)
	}
}

// confirm.mode always in a terminal asks there, not in a dialog.
func TestConsentAlwaysInTerminal(t *testing.T) {
	c, shown := fakeConsent(t, "always", true, gui.Answer{Choice: gui.AllowOnce})
	var question string
	c.Confirm = func(q string) (bool, error) { question = q; return false, nil }
	if err := c.Check("github"); !errors.Is(err, ErrDenied) {
		t.Fatalf("a no in the terminal must deny, got %v", err)
	}
	if *shown != 0 || !strings.Contains(question, "git.exe") {
		t.Fatalf("expected a terminal question, got dialog %d / %q", *shown, question)
	}
}

func TestHumanDuration(t *testing.T) {
	for d, expected := range map[time.Duration]string{
		15 * time.Minute: "15 minutes", time.Minute: "1 minute", time.Hour: "1 hour",
		2 * time.Hour: "2 hours", 90 * time.Second: "1m30s",
	} {
		if got := humanDuration(d); got != expected {
			t.Errorf("humanDuration(%v) = %q, expected %q", d, got, expected)
		}
	}
}
