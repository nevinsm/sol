package cmd

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
)

// spinner displays an animated progress indicator on stderr.
// It is safe for the main goroutine to call setLabel while the
// spinner goroutine reads — a torn string read is harmless for display.
type spinner struct {
	label string
	done  chan struct{}
}

// startSpinner begins an animated spinner on stderr with the given label.
// If stderr is not a terminal the spinner is a no-op (no output).
func startSpinner(label string) *spinner {
	s := &spinner{label: label, done: make(chan struct{})}

	if !term.IsTerminal(int(os.Stderr.Fd())) {
		return s
	}

	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	go func() {
		i := 0
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-s.done:
				fmt.Fprintf(os.Stderr, "\r\033[K") // clear line
				return
			case <-ticker.C:
				fmt.Fprintf(os.Stderr, "\r%s %s", frames[i%len(frames)], s.label)
				i++
			}
		}
	}()
	return s
}

func (s *spinner) setLabel(label string) { s.label = label }
func (s *spinner) stop()                 { close(s.done) }
