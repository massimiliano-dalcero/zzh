//go:build windows

package gui

// The Win32 functions, structures and constants used by the dialog. They
// come from user32, gdi32, kernel32 and comctl32, all part of Windows.
// Structures follow the C layouts of the Windows SDK exactly (field order
// and sizes): Windows reads and writes them directly.

import (
	"golang.org/x/sys/windows"
)

// The DLLs are loaded from System32 only (NewLazySystemDLL), never from the
// current folder, where a planted DLL could take their place.
var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW             = user32.NewProc("RegisterClassExW")
	procCreateWindowExW              = user32.NewProc("CreateWindowExW")
	procDefWindowProcW               = user32.NewProc("DefWindowProcW")
	procDestroyWindow                = user32.NewProc("DestroyWindow")
	procShowWindow                   = user32.NewProc("ShowWindow")
	procSetForegroundWindow          = user32.NewProc("SetForegroundWindow")
	procSetFocus                     = user32.NewProc("SetFocus")
	procGetMessageW                  = user32.NewProc("GetMessageW")
	procIsDialogMessageW             = user32.NewProc("IsDialogMessageW")
	procTranslateMessage             = user32.NewProc("TranslateMessage")
	procDispatchMessageW             = user32.NewProc("DispatchMessageW")
	procPostQuitMessage              = user32.NewProc("PostQuitMessage")
	procSendMessageW                 = user32.NewProc("SendMessageW")
	procSetWindowTextW               = user32.NewProc("SetWindowTextW")
	procGetWindowTextW               = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW         = user32.NewProc("GetWindowTextLengthW")
	procSetWindowPos                 = user32.NewProc("SetWindowPos")
	procSetTimer                     = user32.NewProc("SetTimer")
	procKillTimer                    = user32.NewProc("KillTimer")
	procLoadIconW                    = user32.NewProc("LoadIconW")
	procLoadCursorW                  = user32.NewProc("LoadCursorW")
	procGetSysColorBrush             = user32.NewProc("GetSysColorBrush")
	procGetDC                        = user32.NewProc("GetDC")
	procReleaseDC                    = user32.NewProc("ReleaseDC")
	procDrawTextW                    = user32.NewProc("DrawTextW")
	procAdjustWindowRectEx           = user32.NewProc("AdjustWindowRectEx")
	procGetSystemMetrics             = user32.NewProc("GetSystemMetrics")
	procGetDpiForSystem              = user32.NewProc("GetDpiForSystem")
	procSetThreadDpiAwarenessContext = user32.NewProc("SetThreadDpiAwarenessContext")
	procFlashWindow                  = user32.NewProc("FlashWindow")
	procCreateFontW                  = gdi32.NewProc("CreateFontW")
	procDeleteObject                 = gdi32.NewProc("DeleteObject")
	procSelectObject                 = gdi32.NewProc("SelectObject")
	procSetTextColor                 = gdi32.NewProc("SetTextColor")
	procSetBkMode                    = gdi32.NewProc("SetBkMode")
	procCreateActCtxW                = kernel32.NewProc("CreateActCtxW")
	procActivateActCtx               = kernel32.NewProc("ActivateActCtx")
	procDeactivateActCtx             = kernel32.NewProc("DeactivateActCtx")
	procReleaseActCtx                = kernel32.NewProc("ReleaseActCtx")
	procGetModuleHandleW             = kernel32.NewProc("GetModuleHandleW")
)

// WNDCLASSEXW
type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSmall  uintptr
}

// MSG
type msg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	ptX      int32
	ptY      int32
	lPrivate uint32
}

// RECT
type rect struct {
	left, top, right, bottom int32
}

// ACTCTXW
type actCtx struct {
	size                  uint32
	flags                 uint32
	source                *uint16
	processorArchitecture uint16
	langID                uint16
	assemblyDirectory     *uint16
	resourceName          uintptr
	applicationName       *uint16
	module                uintptr
}

// INITCOMMONCONTROLSEX
type initCommonControlsEx struct {
	size  uint32
	flags uint32
}

const (
	// Window styles.
	wsCaption         = 0x00C00000
	wsSysMenu         = 0x00080000
	wsChild           = 0x40000000
	wsVisible         = 0x10000000
	wsTabStop         = 0x00010000
	wsGroup           = 0x00020000
	wsExTopmost       = 0x00000008
	wsExDlgModalFrame = 0x00000001
	wsExControlParent = 0x00010000
	wsExClientEdge    = 0x00000200

	// Control styles.
	esPassword      = 0x0020
	esAutoHScroll   = 0x0080
	bsPushButton    = 0x0000
	bsDefPushButton = 0x0001
	ssIcon          = 0x0003
	ssNoPrefix      = 0x0080

	// Messages.
	wmDestroy        = 0x0002
	wmClose          = 0x0010
	wmSetFont        = 0x0030
	wmCommand        = 0x0111
	wmTimer          = 0x0113
	wmCtlColorStatic = 0x0138
	stmSetIcon       = 0x0170

	// Control identifiers. IDOK and IDCANCEL are what IsDialogMessage sends
	// for Enter and Esc, which gives the keyboard behaviour for free.
	idOnce     = 1 // IDOK
	idDeny     = 2 // IDCANCEL
	idRemember = 3
	idEdit     = 10
	timerID    = 1

	// Misc.
	swShow           = 5
	swpNoZOrder      = 0x0004
	swpNoActivate    = 0x0010
	colorWindow      = 5
	transparent      = 1
	idiShield        = 32518
	idcArrow         = 32512
	smCxScreen       = 0
	smCyScreen       = 1
	dtCalcRect       = 0x0400
	dtWordBreak      = 0x0010
	dtNoPrefix       = 0x0800
	fwNormal         = 400
	fwBold           = 700
	defaultCharset   = 1
	clearTypeQuality = 5

	// ACTCTX flags.
	actCtxFlagAssemblyDirectoryValid = 0x004
	actCtxFlagResourceNameValid      = 0x008

	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2, a pseudo handle (-4).
	dpiPerMonitorAwareV2 = ^uintptr(3)

	iccStandardClasses = 0x00004000
)
