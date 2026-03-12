// Package processutil provides process lifecycle helpers shared across sol components.
package processutil

import (
	"fmt"
	"os"
	"strings"
	"syscall"
)

// IsRunning reports whether a process with the given PID is alive and not a zombie.
//
// On Linux it reads /proc/{pid}/stat to detect zombie (defunct) processes, which
// appear alive to syscall.Kill(pid, 0). Falls back to the signal-0 probe on
// systems where /proc is unavailable (e.g. macOS).
func IsRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	// Attempt /proc-based check first (Linux).
	if alive, ok := isRunningProc(pid); ok {
		return alive
	}

	// Fallback: signal-0 probe (works on all Unix; cannot distinguish zombies).
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// isRunningProc reads /proc/{pid}/stat and returns (alive, true) when /proc is
// available. Returns (false, false) when /proc is not present so the caller can
// fall back to a different method.
func isRunningProc(pid int) (alive bool, ok bool) {
	path := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(path)
	if err != nil {
		// /proc not available or process already gone.
		if os.IsNotExist(err) {
			// The file not existing could mean no /proc at all, or process gone.
			// If /proc itself doesn't exist we can't tell — signal the caller to
			// fall back.
			if _, statErr := os.Stat("/proc"); os.IsNotExist(statErr) {
				return false, false // /proc not available — use fallback
			}
			return false, true // process is gone
		}
		return false, false // unreadable — use fallback
	}

	// /proc/{pid}/stat format: "pid (comm) state ..."
	// The state field is the third token. Locate it after the closing ')' of the
	// comm field because the comm itself can contain spaces and parentheses.
	s := string(data)
	lastParen := strings.LastIndex(s, ")")
	if lastParen < 0 || lastParen+2 >= len(s) {
		return false, false // unexpected format — use fallback
	}
	// After the closing paren there is a space, then the single-character state.
	state := s[lastParen+2]
	if state == 'Z' {
		return false, true // zombie — not usefully alive
	}
	return true, true
}
