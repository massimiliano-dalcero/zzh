//go:build windows

package caller

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// lookup reads one process from a Toolhelp snapshot of the system: its
// parent and executable name, plus the full path when the process can be
// opened with the limited rights every user has on their own processes.
//
// A new snapshot per call keeps the code simple; the walk reads at most a
// few processes, once per connection that needs a confirmation.
func lookup(pid int) (process, bool) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return process{}, false
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if int(entry.ProcessID) != pid {
			continue
		}
		return process{
			pid:    pid,
			parent: int(entry.ParentProcessID),
			name:   windows.UTF16ToString(entry.ExeFile[:]),
			path:   fullPath(entry.ProcessID),
		}, true
	}
	return process{}, false
}

// fullPath returns the executable path of a process, or "" when it cannot be
// opened (a process of another user or of the system).
func fullPath(pid uint32) string {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(handle)

	buffer := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buffer[:size])
}
