//go:build windows

package daemon

import (
	"log/slog"
	"unsafe"

	"golang.org/x/sys/windows"
)

// _waccess modes (ucrtbase). Node's fs.access uses the same primitive, so
// readable/writable mean exactly what the desktop picker's validation
// means — ACLs and the read-only attribute resolved by the OS, not by
// reading the mode bits.
const (
	pathAccessReadOnly  = 4 // R_OK
	pathAccessWriteOnly = 2 // W_OK
)

var ucrtbaseDLL = windows.NewLazySystemDLL("ucrtbase.dll")

var procWAccess = ucrtbaseDLL.NewProc("_waccess")

// accessMode calls _waccess(path, mode): 0 when the access right holds,
// non-zero otherwise. If the entry point cannot be resolved (ucrtbase is present
// on every supported Windows), the check fails open — this is a picker UX
// hint, and the daemon re-checks authoritatively before running a task.
func accessMode(path string, mode int) error {
	if err := procWAccess.Find(); err != nil {
		slog.Warn("windows path access check unavailable; allowing access", "error", err)
		return nil
	}
	wpath, err := windows.UTF16FromString(path)
	if err != nil {
		return err
	}
	ret, _, _ := procWAccess.Call(
		uintptr(unsafe.Pointer(&wpath[0])),
		uintptr(mode),
	)
	if ret == 0 {
		return nil
	}
	// _waccess reports through the CRT's errno, not Win32 last-error. The
	// return code is the authoritative allow/deny signal; use a stable
	// non-nil error only to preserve accessMode's cross-platform contract.
	return windows.ERROR_ACCESS_DENIED
}
