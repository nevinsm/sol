package quota

import iquota "github.com/nevinsm/sol/internal/quota"

// ScanSession is the per-session JSON row emitted by `sol quota scan --json`.
// The current output is a bare array of these; see NewScanSessions.
type ScanSession struct {
	Session string `json:"session"`
	Account string `json:"account"`
	Limited bool   `json:"limited"`
}

// NewScanSession converts an internal ScanResult to the CLI API ScanSession type.
func NewScanSession(r iquota.ScanResult) ScanSession {
	return ScanSession{
		Session: r.Session,
		Account: r.Account,
		Limited: r.Limited,
	}
}

// NewScanSessions converts a slice of internal ScanResults to CLI API ScanSession types.
func NewScanSessions(results []iquota.ScanResult) []ScanSession {
	sessions := make([]ScanSession, 0, len(results))
	for _, r := range results {
		sessions = append(sessions, NewScanSession(r))
	}
	return sessions
}
