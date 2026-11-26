//go:build windows

package gui

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// dialogScript draws the dialog with WinForms, available in the Windows
// PowerShell that ships with every Windows 10 and 11. It reads its texts
// from ZZH_DIALOG_* variables and prints the answer on stdout (see
// parseAnswer). Enter in the password field means "Allow once", Esc means
// "Deny", and the timer answers "Deny" when nobody does.
const dialogScript = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[System.Windows.Forms.Application]::EnableVisualStyles()

$script:result = 'deny'
$form = New-Object System.Windows.Forms.Form
$form.Text = $env:ZZH_DIALOG_TITLE
$form.TopMost = $true
$form.StartPosition = 'CenterScreen'
$form.FormBorderStyle = 'FixedDialog'
$form.MaximizeBox = $false
$form.MinimizeBox = $false
$form.AutoSize = $true
$form.AutoSizeMode = 'GrowAndShrink'
$form.Padding = New-Object System.Windows.Forms.Padding(14)

$panel = New-Object System.Windows.Forms.FlowLayoutPanel
$panel.FlowDirection = 'TopDown'
$panel.AutoSize = $true
$panel.WrapContents = $false

$message = New-Object System.Windows.Forms.Label
$message.Text = $env:ZZH_DIALOG_MESSAGE
$message.AutoSize = $true
$message.MaximumSize = New-Object System.Drawing.Size(480, 0)
$message.Margin = New-Object System.Windows.Forms.Padding(0, 0, 0, 12)
$panel.Controls.Add($message)

$box = $null
if ($env:ZZH_DIALOG_PASSWORD -eq '1') {
    $label = New-Object System.Windows.Forms.Label
    $label.Text = $env:ZZH_DIALOG_PASSWORD_LABEL
    $label.AutoSize = $true
    $panel.Controls.Add($label)
    $box = New-Object System.Windows.Forms.TextBox
    $box.UseSystemPasswordChar = $true
    $box.Width = 480
    $box.Margin = New-Object System.Windows.Forms.Padding(0, 4, 0, 12)
    $panel.Controls.Add($box)
}

$buttons = New-Object System.Windows.Forms.FlowLayoutPanel
$buttons.FlowDirection = 'LeftToRight'
$buttons.AutoSize = $true
function New-Choice([string]$text, [string]$value) {
    $button = New-Object System.Windows.Forms.Button
    $button.Text = $text
    $button.Tag = $value
    $button.AutoSize = $true
    $button.Add_Click({ param($sender, $e) $script:result = $sender.Tag; $form.Close() })
    $buttons.Controls.Add($button)
    return $button
}
$once = New-Choice $env:ZZH_DIALOG_ONCE 'once'
$null = New-Choice $env:ZZH_DIALOG_REMEMBER 'remember'
$deny = New-Choice $env:ZZH_DIALOG_DENY 'deny'
$form.AcceptButton = $once
$form.CancelButton = $deny
$panel.Controls.Add($buttons)
$form.Controls.Add($panel)

$timer = New-Object System.Windows.Forms.Timer
$timer.Interval = [int]$env:ZZH_DIALOG_TIMEOUT_MS
$timer.Add_Tick({ $script:result = 'deny'; $form.Close() })
$form.Add_Shown({
    $form.Activate()
    if ($box -ne $null) { $box.Focus() } else { $once.Focus() }
    $timer.Start()
})
[void]$form.ShowDialog()
$timer.Stop()

$password = ''
if ($box -ne $null -and $script:result -ne 'deny') { $password = $box.Text }
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
[Console]::Out.Write($script:result + "` + "`" + `n" + $password)
`

// powershellPath is the Windows PowerShell of System32, by full path: a
// powershell.exe found through PATH could be any program.
func powershellPath() string {
	return filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
}

// available reports whether this process runs in a user's interactive
// session: services run in session 0, where a dialog would be invisible
// and would only make the caller wait for the timeout.
func available() bool {
	var session uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &session); err != nil || session == 0 {
		return false
	}
	_, err := os.Stat(powershellPath())
	return err == nil
}

func ask(request Request) (Answer, error) {
	passwordFlag := "0"
	if request.AskPassword {
		passwordFlag = "1"
	}

	// The process is killed some seconds after the dialog's own timer, in
	// case PowerShell hangs before showing it.
	ctx, cancel := context.WithTimeout(context.Background(), request.Timeout+15*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, powershellPath(),
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-STA",
		"-EncodedCommand", encodeScript(dialogScript))
	command.Env = append(os.Environ(),
		"ZZH_DIALOG_TITLE="+title,
		"ZZH_DIALOG_MESSAGE="+request.Message,
		"ZZH_DIALOG_PASSWORD="+passwordFlag,
		"ZZH_DIALOG_PASSWORD_LABEL="+request.PasswordLabel,
		"ZZH_DIALOG_ONCE="+labelOnce,
		"ZZH_DIALOG_REMEMBER="+request.RememberLabel,
		"ZZH_DIALOG_DENY="+labelDeny,
		"ZZH_DIALOG_TIMEOUT_MS="+strconv.FormatInt(request.Timeout.Milliseconds(), 10),
	)
	// PowerShell is a console program: without this flag it would flash
	// a console window next to the dialog.
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}

	output, err := command.Output()
	if err != nil {
		return Answer{}, err
	}
	return parseAnswer(string(output)), nil
}

// encodeScript prepares a script for -EncodedCommand: base64 of its UTF-16
// little-endian text, which avoids every quoting problem of the Windows
// command line.
func encodeScript(script string) string {
	units := utf16.Encode([]rune(script))
	bytes := make([]byte, 2*len(units))
	for i, unit := range units {
		bytes[2*i] = byte(unit)
		bytes[2*i+1] = byte(unit >> 8)
	}
	return base64.StdEncoding.EncodeToString(bytes)
}
