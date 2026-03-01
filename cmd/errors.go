package cmd

import (
	"errors"
	"fmt"
)

// exitError wraps an exit code for commands that need to communicate
// non-zero status. The root command's Execute path translates this
// into an os.Exit call after all defers have run.
type exitError struct {
	code int
}

func (e *exitError) Error() string {
	return fmt.Sprintf("exit status %d", e.code)
}

// ExitCode returns the exit code, or 0 if the error is nil or not an
// exitError.
func ExitCode(err error) int {
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return 0
}
