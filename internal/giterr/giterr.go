// Package giterr classifies git command failures caused by a missing or
// rejected credential, and wraps them with operator-actionable guidance.
//
// Sol sets GIT_TERMINAL_PROMPT=0 process-wide (cmd/root.go) so git never
// blocks orchestration on an interactive credential prompt: an HTTPS remote
// with no stored credential fails fast instead of hanging. That fast failure
// still needs to be legible — git's own wording ("could not read Username
// for ...: terminal prompts disabled") doesn't tell the operator what to do
// about it. Wrap turns that raw failure into a pointer at docs/credentials.md.
package giterr

import (
	"bytes"
	"fmt"
)

// authFailureMarkers are substrings git prints (to stderr, captured here via
// CombinedOutput) when it would normally prompt for credentials but instead
// fails immediately because GIT_TERMINAL_PROMPT=0 disables the prompt, or
// because a stored credential was rejected outright.
var authFailureMarkers = [][]byte{
	[]byte("could not read Username"),
	[]byte("could not read Password"),
	[]byte("terminal prompts disabled"),
	[]byte("Authentication failed"),
	[]byte("fatal: Authentication"),
}

// IsAuthFailure reports whether git command output (stdout+stderr, as
// returned by exec.Cmd.CombinedOutput) indicates a missing or rejected
// credential rather than some other failure (network, bad ref, etc.).
func IsAuthFailure(output []byte) bool {
	for _, marker := range authFailureMarkers {
		if bytes.Contains(output, marker) {
			return true
		}
	}
	return false
}

// Wrap returns err unchanged if err is nil or output does not indicate an
// auth failure. Otherwise it wraps err with guidance pointing at
// docs/credentials.md: the remote requires a stored credential (a
// gh-auth-managed .git-credentials entry or an API key) or should be
// switched to SSH — sol never prompts for one.
func Wrap(err error, output []byte) error {
	if err == nil || !IsAuthFailure(output) {
		return err
	}
	return fmt.Errorf("%w — remote requires credentials (HTTPS remote has no stored credential and sol never prompts); see docs/credentials.md", err)
}
