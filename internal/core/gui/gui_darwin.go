//go:build darwin

package gui

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// dialogScript is AppleScript run by osascript. The texts are read from the
// environment ("system attribute"), not from the command line. "giving up
// after" closes the dialog when nobody answers; the cancel button ("Deny")
// raises an error, caught and turned into "deny".
const dialogScript = `
set msg to system attribute "ZZH_DIALOG_MESSAGE"
set askPassword to system attribute "ZZH_DIALOG_PASSWORD"
set onceLabel to system attribute "ZZH_DIALOG_ONCE"
set rememberLabel to system attribute "ZZH_DIALOG_REMEMBER"
set denyLabel to system attribute "ZZH_DIALOG_DENY"
set waitSeconds to (system attribute "ZZH_DIALOG_TIMEOUT_S") as integer
set pw to ""
try
	if askPassword is "1" then
		set msg to msg & return & return & (system attribute "ZZH_DIALOG_PASSWORD_LABEL")
		set r to display dialog msg with title "zzh" default answer "" with hidden answer buttons {denyLabel, rememberLabel, onceLabel} default button onceLabel cancel button denyLabel giving up after waitSeconds with icon caution
		set pw to text returned of r
	else
		set r to display dialog msg with title "zzh" buttons {denyLabel, rememberLabel, onceLabel} default button onceLabel cancel button denyLabel giving up after waitSeconds with icon caution
	end if
	if gave up of r then return "deny" & linefeed
	set chosen to button returned of r
	if chosen is onceLabel then return "once" & linefeed & pw
	if chosen is rememberLabel then return "remember" & linefeed & pw
	return "deny" & linefeed
on error
	return "deny" & linefeed
end try
`

// available reports whether a dialog can reach the user: not from an ssh
// session into this Mac (no screen there), and only if osascript exists.
func available() bool {
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		return false
	}
	_, err := exec.LookPath("osascript")
	return err == nil
}

func ask(request Request) (Answer, error) {
	passwordFlag := "0"
	if request.AskPassword {
		passwordFlag = "1"
	}
	ctx, cancel := context.WithTimeout(context.Background(), request.Timeout+15*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, "osascript", "-e", dialogScript)
	command.Env = append(os.Environ(),
		"ZZH_DIALOG_MESSAGE="+request.Message,
		"ZZH_DIALOG_PASSWORD="+passwordFlag,
		"ZZH_DIALOG_PASSWORD_LABEL="+request.PasswordLabel,
		"ZZH_DIALOG_ONCE="+labelOnce,
		"ZZH_DIALOG_REMEMBER="+request.RememberLabel,
		"ZZH_DIALOG_DENY="+labelDeny,
		"ZZH_DIALOG_TIMEOUT_S="+strconv.Itoa(int(request.Timeout.Seconds())),
	)
	output, err := command.Output()
	if err != nil {
		return Answer{}, err
	}
	return parseAnswer(string(output)), nil
}
