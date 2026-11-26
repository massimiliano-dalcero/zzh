//go:build windows

package gui

// The dialog is a native Win32 window created by zzh itself: no helper
// process, no PowerShell (which group policies may restrict), nothing to
// start, and the password never leaves zzh's memory. It is drawn with the
// controls every Windows dialog uses (static texts, a password edit box,
// push buttons) and laid out by hand:
//
//	┌ zzh ─────────────────────────────────────────────┐
//	│ [shield]  git.exe (started by Code.exe) wants…   │  heading, bold
//	│           It will use the keys and passwords…    │  message
//	│           Program: C:\…\git.exe                  │  detail, gray
//	│           Master password of "default":          │  only when locked
//	│           [●●●●●●●●                          ]   │
//	│           This window closes in 60 s…            │  countdown, gray
//	│      [Allow once] [Allow for 15 minutes] [Deny]  │
//	└──────────────────────────────────────────────────┘
//
// Two things make it look like a current Windows dialog rather than a
// Windows 95 one: the thread is declared DPI aware (sharp text on scaled
// screens) and the visual styles of Common Controls 6 are activated for
// the time of the dialog (modern buttons and edit box). If either is not
// available the dialog still works, with a plainer look.

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// available reports whether this process runs in a user's interactive
// session: services run in session 0, where a window would be invisible
// and would only make the caller wait for the timeout.
func available() bool {
	var session uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &session); err != nil {
		return false
	}
	return session != 0
}

// dialogMutex allows one dialog at a time: the window procedure works on a
// single shared state.
var dialogMutex sync.Mutex

// dialogState is what the window procedure needs while the dialog is open.
type dialogState struct {
	window, edit, countdown, detail uintptr
	remaining                       int
	answer                          Answer
	whiteBrush                      uintptr
}

var active *dialogState

func ask(request Request) (Answer, error) {
	dialogMutex.Lock()
	defer dialogMutex.Unlock()

	// A window belongs to the thread that created it, and its messages are
	// delivered to that thread: the goroutine must not move to another one
	// while the dialog is open.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	restoreDPI := makeThreadDPIAware()
	defer restoreDPI()
	restoreStyles := enableVisualStyles()
	defer restoreStyles()

	className, err := registerClass()
	if err != nil {
		return Answer{}, err
	}

	state := &dialogState{remaining: int(request.Timeout.Seconds()), answer: Answer{Choice: Deny}}
	state.whiteBrush, _, _ = procGetSysColorBrush.Call(colorWindow)
	active = state
	defer func() { active = nil }()

	if err := buildWindow(className, request, state); err != nil {
		return Answer{}, err
	}

	// The first ShowWindow of a process ignores its argument and uses the
	// show mode chosen by the parent process at startup. Programs that run
	// git or ssh in the background start them hidden (VS Code does), so the
	// first call would keep the dialog invisible: the second one shows it.
	procShowWindow.Call(state.window, swShow)
	procShowWindow.Call(state.window, swShow)
	// A background program may not take the focus (Windows prevents focus
	// stealing): the window is topmost anyway, and flashes in the taskbar
	// when it cannot come to the front.
	if ok, _, _ := procSetForegroundWindow.Call(state.window); ok == 0 {
		procFlashWindow.Call(state.window, 1)
	}
	if state.edit != 0 {
		procSetFocus.Call(state.edit)
	}
	procSetTimer.Call(state.window, timerID, 1000, 0)

	var m msg
	for {
		result, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if result == 0 || int32(result) == -1 {
			break
		}
		// IsDialogMessage gives the keyboard behaviour of a dialog: Tab
		// between controls, Enter for "Allow once" (IDOK), Esc for "Deny"
		// (IDCANCEL).
		if handled, _, _ := procIsDialogMessageW.Call(state.window, uintptr(unsafe.Pointer(&m))); handled == 0 {
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
	}
	return state.answer, nil
}

// --- class and window procedure ---

var (
	classOnce     sync.Once
	classNamePtr  *uint16 // kept alive: Windows holds the pointer
	classErr      error
	windowProcRef uintptr
)

// registerClass registers the window class once per process; the window
// procedure callback is created once too (Go limits their number).
func registerClass() (*uint16, error) {
	classOnce.Do(func() {
		classNamePtr, classErr = windows.UTF16PtrFromString("ZzhConfirmationDialog")
		if classErr != nil {
			return
		}
		windowProcRef = windows.NewCallback(windowProc)
		instance, _, _ := procGetModuleHandleW.Call(0)
		cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
		brush, _, _ := procGetSysColorBrush.Call(colorWindow)
		class := wndClassEx{
			wndProc:    windowProcRef,
			instance:   instance,
			cursor:     cursor,
			background: brush,
			className:  classNamePtr,
		}
		class.size = uint32(unsafe.Sizeof(class))
		if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
			classErr = fmt.Errorf("cannot register the dialog window class: %w", err)
		}
	})
	return classNamePtr, classErr
}

func windowProc(hwnd, message, wParam, lParam uintptr) uintptr {
	state := active
	if state == nil {
		result, _, _ := procDefWindowProcW.Call(hwnd, message, wParam, lParam)
		return result
	}

	switch message {
	case wmCommand:
		switch wParam & 0xFFFF {
		case idOnce:
			finish(state, AllowOnce)
		case idRemember:
			finish(state, AllowForAWhile)
		case idDeny:
			finish(state, Deny)
		}
		return 0

	case wmTimer:
		state.remaining--
		if state.remaining <= 0 {
			finish(state, Deny)
			return 0
		}
		setText(state.countdown, countdownText(state.remaining))
		return 0

	case wmCtlColorStatic:
		// Static texts on the white background; detail and countdown in
		// gray, as secondary information.
		hdc := wParam
		procSetBkMode.Call(hdc, transparent)
		if lParam == state.detail || lParam == state.countdown {
			procSetTextColor.Call(hdc, rgb(96, 96, 96))
		}
		return state.whiteBrush

	case wmClose:
		finish(state, Deny)
		return 0

	case wmDestroy:
		procKillTimer.Call(hwnd, timerID)
		procPostQuitMessage.Call(0)
		return 0
	}

	result, _, _ := procDefWindowProcW.Call(hwnd, message, wParam, lParam)
	return result
}

// finish records the answer, reading the password when the user allowed,
// and closes the window.
func finish(state *dialogState, choice Choice) {
	state.answer = Answer{Choice: choice}
	if choice != Deny && state.edit != 0 {
		state.answer.Password = getText(state.edit)
	}
	if state.edit != 0 {
		// Best effort: do not leave the password in the control's memory.
		setText(state.edit, "")
	}
	procDestroyWindow.Call(state.window)
}

func countdownText(seconds int) string {
	return fmt.Sprintf("This window closes in %d s and then denies the request.", seconds)
}

// --- layout ---

// buildWindow creates the window and its controls. Sizes are in pixels at
// 96 DPI and scaled to the screen's DPI.
func buildWindow(className *uint16, request Request, state *dialogState) error {
	dpi := systemDPI()
	scale := func(v int32) int32 { return v * dpi / 96 }

	normal := createFont(dpi, fwNormal)
	bold := createFont(dpi, fwBold)

	const (
		margin       = 16
		iconSize     = 32
		gap          = 12
		contentWidth = 440
	)
	contentX := scale(margin + iconSize + gap)
	width := scale(contentWidth)

	type block struct {
		text  string
		font  uintptr
		after int32
		store *uintptr
	}
	blocks := []block{
		{request.Heading, bold, 8, nil},
		{request.Message, normal, 8, nil},
		{request.Detail, normal, 12, &state.detail},
	}

	// Measure every text with its font and the available width.
	y := scale(margin)
	type placed struct {
		block
		y, height int32
	}
	var texts []placed
	for _, b := range blocks {
		if b.text == "" {
			continue
		}
		height := measure(b.font, b.text, width)
		texts = append(texts, placed{b, y, height})
		y += height + scale(b.after)
	}

	var labelY, labelHeight, editY, editHeight int32
	if request.AskPassword {
		labelHeight = measure(normal, request.PasswordLabel, width)
		labelY = y
		y += labelHeight + scale(4)
		editHeight = scale(24)
		editY = y
		y += editHeight + scale(12)
	}

	countdownY := y
	countdownHeight := measure(normal, countdownText(state.remaining), width)
	y += countdownHeight + scale(16)

	// Buttons, right-aligned, each as wide as its text plus padding.
	labels := []struct {
		text  string
		id    uintptr
		style uintptr
	}{
		{labelOnce, idOnce, bsDefPushButton},
		{request.RememberLabel, idRemember, bsPushButton},
		{labelDeny, idDeny, bsPushButton},
	}
	buttonHeight := scale(28)
	buttonWidths := make([]int32, len(labels))
	var rowWidth int32
	for i, l := range labels {
		w := textWidth(normal, l.text) + scale(28)
		if w < scale(96) {
			w = scale(96)
		}
		buttonWidths[i] = w
		rowWidth += w
	}
	rowWidth += scale(8) * int32(len(labels)-1)
	buttonsY := y
	y += buttonHeight + scale(margin)

	clientWidth := contentX + width + scale(margin)
	clientHeight := y

	// The window: caption with close button, topmost so that a request from
	// a background program is not hidden behind other windows.
	style := uintptr(wsCaption | wsSysMenu)
	exStyle := uintptr(wsExTopmost | wsExDlgModalFrame | wsExControlParent)
	frame := rect{0, 0, clientWidth, clientHeight}
	procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&frame)), style, 0, exStyle)
	windowWidth := frame.right - frame.left
	windowHeight := frame.bottom - frame.top
	screenWidth, _, _ := procGetSystemMetrics.Call(smCxScreen)
	screenHeight, _, _ := procGetSystemMetrics.Call(smCyScreen)

	instance, _, _ := procGetModuleHandleW.Call(0)
	window, _, err := procCreateWindowExW.Call(exStyle,
		uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(utf16(title))), style,
		uintptr((int32(screenWidth)-windowWidth)/2), uintptr((int32(screenHeight)-windowHeight)/2),
		uintptr(windowWidth), uintptr(windowHeight), 0, 0, instance, 0)
	if window == 0 {
		return fmt.Errorf("cannot create the dialog window: %w", err)
	}
	state.window = window

	child := func(class, text string, style, exStyle uintptr, x, y, w, h int32, id uintptr, font uintptr) uintptr {
		hwnd, _, _ := procCreateWindowExW.Call(exStyle,
			uintptr(unsafe.Pointer(utf16(class))), uintptr(unsafe.Pointer(utf16(text))),
			wsChild|wsVisible|style, uintptr(x), uintptr(y), uintptr(w), uintptr(h),
			window, id, instance, 0)
		if font != 0 {
			procSendMessageW.Call(hwnd, wmSetFont, font, 1)
		}
		return hwnd
	}

	// Shield icon: the system's symbol for a security decision.
	icon := child("STATIC", "", ssIcon, 0, scale(margin), scale(margin), scale(iconSize), scale(iconSize), 0, 0)
	shield, _, _ := procLoadIconW.Call(0, idiShield)
	procSendMessageW.Call(icon, stmSetIcon, shield, 0)

	for _, t := range texts {
		hwnd := child("STATIC", t.text, ssNoPrefix, 0, contentX, t.y, width, t.height, 0, t.font)
		if t.store != nil {
			*t.store = hwnd
		}
	}
	if request.AskPassword {
		child("STATIC", request.PasswordLabel, ssNoPrefix, 0, contentX, labelY, width, labelHeight, 0, normal)
		state.edit = child("EDIT", "", esPassword|esAutoHScroll|wsTabStop|wsGroup, wsExClientEdge,
			contentX, editY, width, editHeight, idEdit, normal)
	}
	state.countdown = child("STATIC", countdownText(state.remaining), ssNoPrefix, 0,
		contentX, countdownY, width, countdownHeight, 0, normal)

	x := clientWidth - scale(margin) - rowWidth
	for i, l := range labels {
		child("BUTTON", l.text, l.style|wsTabStop, 0, x, buttonsY, buttonWidths[i], buttonHeight, l.id, normal)
		x += buttonWidths[i] + scale(8)
	}
	return nil
}

// --- helpers ---

// utf16 converts a Go string for Windows. The texts given to windows are
// copied by Windows when the control is created, so the pointer only has
// to live for the call.
func utf16(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		// Only strings with a NUL byte fail: never the case for the texts
		// built by zzh; an empty text is shown instead.
		p, _ = windows.UTF16PtrFromString("")
	}
	return p
}

func setText(hwnd uintptr, text string) {
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(utf16(text))))
}

func getText(hwnd uintptr) string {
	length, _, _ := procGetWindowTextLengthW.Call(hwnd)
	buffer := make([]uint16, length+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	return windows.UTF16ToString(buffer)
}

func rgb(r, g, b uint8) uintptr {
	return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16
}

// systemDPI returns the screen's DPI, 96 when Windows is too old to tell.
func systemDPI() int32 {
	if procGetDpiForSystem.Find() != nil {
		return 96
	}
	dpi, _, _ := procGetDpiForSystem.Call()
	if dpi == 0 {
		return 96
	}
	return int32(dpi)
}

// createFont creates the Windows UI font (Segoe UI, 9 points) at the given
// DPI. The fonts live until the process ends: a dialog is rare, and the
// process exits soon after.
func createFont(dpi int32, weight int32) uintptr {
	height := -(9*dpi + 36) / 72 // -MulDiv(9, dpi, 72)
	font, _, _ := procCreateFontW.Call(uintptr(height), 0, 0, 0, uintptr(weight), 0, 0, 0,
		defaultCharset, 0, 0, clearTypeQuality, 0, uintptr(unsafe.Pointer(utf16("Segoe UI"))))
	return font
}

// measure returns the height of text wrapped at width with font.
func measure(font uintptr, text string, width int32) int32 {
	r := rect{0, 0, width, 0}
	drawCalc(font, text, &r, dtCalcRect|dtWordBreak|dtNoPrefix)
	return r.bottom - r.top
}

// textWidth returns the width of text on one line with font.
func textWidth(font uintptr, text string) int32 {
	r := rect{0, 0, 10000, 0}
	drawCalc(font, text, &r, dtCalcRect|dtNoPrefix)
	return r.right - r.left
}

func drawCalc(font uintptr, text string, r *rect, format uintptr) {
	hdc, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, hdc)
	previous, _, _ := procSelectObject.Call(hdc, font)
	defer procSelectObject.Call(hdc, previous)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(utf16(text))), ^uintptr(0), uintptr(unsafe.Pointer(r)), format)
}

// makeThreadDPIAware makes the windows of this thread render at the
// screen's real resolution (Windows 10 1703 and later). Without it Windows
// stretches the dialog on scaled screens and the text looks blurred.
func makeThreadDPIAware() func() {
	if procSetThreadDpiAwarenessContext.Find() != nil {
		return func() {}
	}
	previous, _, _ := procSetThreadDpiAwarenessContext.Call(dpiPerMonitorAwareV2)
	return func() {
		if previous != 0 {
			procSetThreadDpiAwarenessContext.Call(previous)
		}
	}
}

// enableVisualStyles activates Common Controls 6 for this thread, the
// library that draws modern buttons and edit boxes. A program normally
// asks for it in a manifest embedded at build time; zzh borrows the one
// Windows ships inside shell32.dll (resource 124, which exists for this
// purpose) through an activation context, so no build step is needed. It
// returns the function that deactivates it; on failure the dialog simply
// keeps the classic look.
func enableVisualStyles() func() {
	noop := func() {}
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		return noop
	}
	context := actCtx{
		flags:             actCtxFlagAssemblyDirectoryValid | actCtxFlagResourceNameValid,
		source:            utf16("shell32.dll"),
		assemblyDirectory: utf16(systemDir),
		resourceName:      124,
	}
	context.size = uint32(unsafe.Sizeof(context))
	handle, _, _ := procCreateActCtxW.Call(uintptr(unsafe.Pointer(&context)))
	if handle == uintptr(windows.InvalidHandle) {
		return noop
	}
	var cookie uintptr
	if ok, _, _ := procActivateActCtx.Call(handle, uintptr(unsafe.Pointer(&cookie))); ok == 0 {
		procReleaseActCtx.Call(handle)
		return noop
	}

	// With the context active, loading comctl32 by name gets version 6
	// from the Windows side-by-side store; initializing it registers the
	// modern classes behind BUTTON, EDIT and STATIC.
	if err := initCommonControls(); err != nil {
		procDeactivateActCtx.Call(0, cookie)
		procReleaseActCtx.Call(handle)
		return noop
	}
	return func() {
		procDeactivateActCtx.Call(0, cookie)
		procReleaseActCtx.Call(handle)
	}
}

func initCommonControls() error {
	module, err := windows.LoadLibraryEx("comctl32.dll", 0, windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	if err != nil {
		return err
	}
	address, err := windows.GetProcAddress(module, "InitCommonControlsEx")
	if err != nil {
		return err
	}
	controls := initCommonControlsEx{flags: iccStandardClasses}
	controls.size = uint32(unsafe.Sizeof(controls))
	if ok, _, _ := syscall.SyscallN(address, uintptr(unsafe.Pointer(&controls))); ok == 0 {
		return errors.New("InitCommonControlsEx failed")
	}
	return nil
}
