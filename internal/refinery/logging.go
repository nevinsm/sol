package refinery

import (
	"log/slog"
	"os"
)

// NewLogger creates an slog.Logger writing JSON to path.
// If path is empty, logs to stderr.
// Opens file with O_CREATE|O_APPEND|O_WRONLY.
func NewLogger(path string) (*slog.Logger, *os.File, error) {
	if path == "" {
		logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		return logger, nil, nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}

	logger := slog.New(slog.NewJSONHandler(f, nil))
	return logger, f, nil
}
