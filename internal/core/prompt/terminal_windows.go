//go:build windows

package prompt

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// kernel32 and user32 are Known DLLs, always loaded from System32, so the
// lazy loading of the standard syscall package cannot be hijacked by a DLL
// placed in the current directory.
var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	user32               = syscall.NewLazyDLL("user32.dll")
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	procIsWindowVisible  = user32.NewProc("IsWindowVisible")
	procGetClassNameW    = user32.NewProc("GetClassNameW")
)

// pseudoConsoleClass is the window class of the hidden placeholder window
// that Windows creates for a pseudo console (ConPTY). ConPTY is what Windows
// Terminal, the VS Code integrated terminal and the OpenSSH server use: the
// console is real and the user is looking at it, but its window, as Windows
// sees it, is invisible.
const pseudoConsoleClass = "PseudoConsoleWindow"

// openPlatformTerminal opens the console the process is attached to, through
// the special names CONIN$ and CONOUT$, which reach the console even when
// the standard streams are redirected (the Windows equivalent of /dev/tty).
//
// On Windows a process can be attached to a console that nobody sees: VS
// Code, for example, starts git (and so ssh and zzh) with a hidden console.
// CONIN$ opens fine there, and a question would wait forever for an answer
// that cannot come, freezing the program that called ssh. The console is
// therefore used only when consoleIsVisible says a person can see it.
func openPlatformTerminal() (*terminal, error) {
	if !consoleIsVisible() {
		return nil, errors.New("no visible console")
	}

	// Read and write access on CONIN$ is required to change the console mode
	// (no echo, raw mode), not only to read from it.
	input, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	output, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		input.Close()
		return nil, err
	}

	return &terminal{
		input:  input,
		output: output,
		close: func() {
			input.Close()
			output.Close()
		},
	}, nil
}

// consoleIsVisible reports whether the console of the process is one the
// user can see and type into.
func consoleIsVisible() bool {
	window, _, _ := procGetConsoleWindow.Call()
	if window == 0 {
		// No console at all, or a console created without a window
		// (CREATE_NO_WINDOW, used by programs that hide their children).
		return false
	}

	visible, _, _ := procIsWindowVisible.Call(window)
	return acceptConsole(visible != 0, windowClassName(window))
}

// acceptConsole is the decision rule, separated so that it can be tested
// without real windows: a visible console window (classic conhost) or a
// pseudo console (whose placeholder window is always hidden) is usable;
// any other hidden console belongs to a program running in the background.
func acceptConsole(visible bool, className string) bool {
	return visible || className == pseudoConsoleClass
}

// windowClassName returns the class name of a window, or "" when it cannot
// be read (the window was closed meanwhile): an empty name is simply not
// recognized as a pseudo console.
func windowClassName(window uintptr) string {
	buffer := make([]uint16, 256)
	length, _, _ := procGetClassNameW.Call(window, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if length == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer[:length])
}
