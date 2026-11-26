//go:build windows

package style

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableColors turns on the processing of color codes on a Windows console.
// Windows Terminal and the VS Code terminal have it on already; the classic
// console window needs ENABLE_VIRTUAL_TERMINAL_PROCESSING, available since
// Windows 10. If it cannot be enabled the codes would be printed as garbage,
// so the caller falls back to plain text.
func enableColors(file *os.File) bool {
	handle := windows.Handle(file.Fd())

	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	return windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}
