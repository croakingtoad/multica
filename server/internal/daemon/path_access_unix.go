//go:build !windows

package daemon

import "golang.org/x/sys/unix"

// accessMode calls access(2) — the same primitive Node's fs.access uses —
// so readable/writable mean exactly what the desktop picker's validation
// means (owner/group/other and ACLs resolved by the kernel, not by reading
// the mode bits).
func accessMode(path string, mode int) error {
	return unix.Access(path, uint32(mode))
}

const (
	pathAccessReadOnly  = int(unix.R_OK)
	pathAccessWriteOnly = int(unix.W_OK)
)
