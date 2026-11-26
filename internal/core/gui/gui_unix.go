//go:build !windows && !darwin

package gui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// On Linux and the BSDs the dialog is drawn by zenity (GNOME and most
// desktops) or kdialog (KDE), whichever is installed. Neither can show a
// hidden field together with three buttons and return both, so the dialog
// is two steps when the password is needed: first the question, then, if
// allowed, the password.

// available reports whether there is a display and a tool to draw on it.
func available() bool {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	return tool() != ""
}

// tool returns "zenity", "kdialog" or "".
func tool() string {
	for _, name := range []string{"zenity", "kdialog"} {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	return ""
}

func ask(request Request) (Answer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), request.Timeout+15*time.Second)
	defer cancel()

	var choice Choice
	var err error
	if tool() == "zenity" {
		choice, err = zenityQuestion(ctx, request)
	} else {
		choice, err = kdialogQuestion(ctx, request)
	}
	if err != nil || choice == Deny || !request.AskPassword {
		return Answer{Choice: choice}, err
	}

	password, ok := askPassword(ctx, request)
	if !ok {
		return Answer{Choice: Deny}, nil
	}
	return Answer{Choice: choice, Password: password}, nil
}

// zenityQuestion: exit 0 is the OK button, an extra button prints its
// label and exits 1 like Cancel, the timeout exits 5.
func zenityQuestion(ctx context.Context, request Request) (Choice, error) {
	command := exec.CommandContext(ctx, "zenity", "--question", "--title=zzh",
		"--text="+request.plainText(),
		"--ok-label="+labelOnce, "--cancel-label="+labelDeny,
		"--extra-button="+request.RememberLabel,
		"--timeout="+strconv.Itoa(int(request.Timeout.Seconds())))
	output, err := command.Output()
	if err == nil {
		return AllowOnce, nil
	}
	if strings.TrimSpace(string(output)) == request.RememberLabel {
		return AllowForAWhile, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return Deny, nil // Deny, Esc, closed window, timeout
	}
	return Deny, err
}

// kdialogQuestion: --yesnocancel exits 0, 1 or 2 for its three buttons.
// kdialog has no timeout of its own: the context kills it.
func kdialogQuestion(ctx context.Context, request Request) (Choice, error) {
	command := exec.CommandContext(ctx, "kdialog", "--title", "zzh",
		"--yes-label", labelOnce, "--no-label", request.RememberLabel, "--cancel-label", labelDeny,
		"--yesnocancel", request.plainText())
	err := command.Run()
	if err == nil {
		return AllowOnce, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if exit.ExitCode() == 1 {
			return AllowForAWhile, nil
		}
		return Deny, nil
	}
	return Deny, err
}

// askPassword shows the hidden field; ok is false when it was cancelled.
func askPassword(ctx context.Context, request Request) (string, bool) {
	var command *exec.Cmd
	if tool() == "zenity" {
		command = exec.CommandContext(ctx, "zenity", "--entry", "--hide-text", "--title=zzh",
			"--text="+request.PasswordLabel,
			"--timeout="+strconv.Itoa(int(request.Timeout.Seconds())))
	} else {
		command = exec.CommandContext(ctx, "kdialog", "--title", "zzh", "--password", request.PasswordLabel)
	}
	output, err := command.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSuffix(string(output), "\n"), true
}
