package connect

import (
	"errors"
	"fmt"
	"time"

	"zzh/internal/core/approval"
	"zzh/internal/core/caller"
	"zzh/internal/core/config"
	"zzh/internal/core/gui"
)

// Consent decides whether the program asking for a connection may use the
// keys and passwords stored in KeePass, asking the user when needed (D-061).
//
// The question appears in a graphical dialog when there is no visible
// terminal (a program running in the background), or in the terminal with
// confirm.mode "always". "Allow for a while" is remembered for the program
// and the host.
//
// When the database is locked and nobody can type the password on a
// terminal, the same dialog also asks the master password: approving it
// counts as the confirmation, so the user answers once.
type Consent struct {
	// Mode and RememberFor come from the confirm.* settings.
	Mode        string
	RememberFor time.Duration

	// Interactive is true when a visible terminal can receive questions.
	Interactive bool

	// Target is the host as typed, shown before the vault is open.
	Target string

	Approvals approval.Store

	// Identify, Ask, Confirm and Now are the system services, parameters
	// so that tests can replace them.
	Identify func() caller.Caller
	Ask      func(gui.Request) (gui.Answer, error)
	Confirm  func(question string) (bool, error)
	Now      func() time.Time

	// granted records that the unlock dialog was approved, and remember
	// that it was approved "for a while".
	granted  bool
	remember bool

	who      caller.Caller
	resolved bool
}

// ErrDenied is returned when the user refuses, or does not answer in time.
var ErrDenied = errors.New("access denied in the zzh confirmation")

// errCannotAsk is returned when a confirmation is required but there is
// neither a terminal nor a graphical session to ask it.
var errCannotAsk = errors.New("a confirmation is required (confirm.mode) but there is no terminal or graphical session to ask it; " +
	`unlocked programs without a terminal can be allowed with "zzh config set confirm.mode never"`)

// NewConsent prepares the consent for one connection with the system
// services.
func NewConsent(settings config.ConfirmConfig, interactive bool, target string, approvals approval.Store, self int) *Consent {
	return &Consent{
		Mode:        settings.Mode,
		RememberFor: settings.RememberFor.Std(),
		Interactive: interactive,
		Target:      target,
		Approvals:   approvals,
		Identify:    func() caller.Caller { return caller.Identify(self) },
		Ask:         gui.Ask,
		Now:         time.Now,
	}
}

// caller identifies the requesting program once, when first needed: the
// process walk is skipped for connections that never ask anything.
func (c *Consent) caller() caller.Caller {
	if !c.resolved {
		c.who = c.Identify()
		c.resolved = true
	}
	return c.who
}

// needed reports whether confirm.mode requires asking for this connection.
func (c *Consent) needed() bool {
	switch c.Mode {
	case "never":
		return false
	case "always":
		return true
	}
	return !c.Interactive // "background"
}

func (c *Consent) rememberLabel() string {
	return "Allow for " + humanDuration(c.RememberFor)
}

// UnlockDialog asks the master password in the confirmation dialog. It is
// used by the session when the database is locked and no terminal can
// receive the question. gui.ErrUnavailable means no dialog is possible
// either, and the session reports the usual "no terminal" error.
func (c *Consent) UnlockDialog(database config.ResolvedDatabase) (string, error) {
	answer, err := c.Ask(gui.Request{
		Message: fmt.Sprintf("%s wants to connect to %q through zzh.\n\nThe KeePass database %q is locked: type its master password to allow the connection, or deny it (the plain ssh will run instead).",
			c.caller().Describe(), c.Target, database.Name),
		AskPassword:   true,
		PasswordLabel: fmt.Sprintf("Master password of %q:", database.Name),
		RememberLabel: c.rememberLabel(),
		Timeout:       time.Minute,
	})
	if err != nil {
		return "", err
	}
	if answer.Choice == gui.Deny || answer.Password == "" {
		return "", ErrDenied
	}
	c.granted = true
	c.remember = answer.Choice == gui.AllowForAWhile
	return answer.Password, nil
}

// Check is called once the connection is known to use KeePass data for
// host (a KeePass entry, or a key chosen with --key). It returns nil when
// the connection may go on, ErrDenied when the user refused.
func (c *Consent) Check(host string) error {
	if !c.needed() {
		return nil
	}

	who := c.caller()
	now := c.Now()

	// Already approved in the unlock dialog, for this very connection.
	if c.granted {
		return c.grantIfRemembered(who, host, now)
	}
	if c.Approvals.Allowed(who.Identity(), host, now) {
		return nil
	}

	if c.Interactive {
		// Only with confirm.mode "always": the person at the terminal is
		// asked there, without the "for a while" option.
		allowed, err := c.Confirm(fmt.Sprintf("%s wants to connect to %q with the KeePass credentials. Allow?", who.Describe(), host))
		if err != nil {
			return err
		}
		if !allowed {
			return ErrDenied
		}
		return nil
	}

	answer, err := c.Ask(gui.Request{
		Message: fmt.Sprintf("%s wants to connect to %q with the keys and passwords stored in KeePass.\n\nProgram: %s",
			who.Describe(), host, identityText(who)),
		RememberLabel: c.rememberLabel(),
		Timeout:       time.Minute,
	})
	if errors.Is(err, gui.ErrUnavailable) {
		return errCannotAsk
	}
	if err != nil {
		return fmt.Errorf("cannot show the confirmation dialog: %w", err)
	}
	switch answer.Choice {
	case gui.AllowOnce:
		return nil
	case gui.AllowForAWhile:
		c.remember = true
		return c.grantIfRemembered(who, host, now)
	}
	return ErrDenied
}

// grantIfRemembered stores the authorization when the user chose "for a
// while". A failure to store it does not stop the connection the user just
// allowed: they will simply be asked again next time.
func (c *Consent) grantIfRemembered(who caller.Caller, host string, now time.Time) error {
	if c.remember {
		// Best effort, see above.
		_ = c.Approvals.Grant(who.Identity(), host, now, c.RememberFor)
	}
	return nil
}

// identityText is the full path when known, so that a look-alike program in
// another folder is recognizable.
func identityText(who caller.Caller) string {
	if who.Path != "" {
		return who.Path
	}
	return "path unknown"
}

// humanDuration writes 15m as "15 minutes", 2h as "2 hours", otherwise the
// Go form ("1h30m0s").
func humanDuration(d time.Duration) string {
	switch {
	case d%time.Hour == 0 && d >= time.Hour:
		hours := int(d / time.Hour)
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	case d%time.Minute == 0 && d >= time.Minute:
		minutes := int(d / time.Minute)
		if minutes == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", minutes)
	}
	return d.String()
}
