// Package cliflag provides shared helpers for CLI flags that carry
// free-form, potentially long text (descriptions, summaries, message
// bodies). Long text passed inline as a single flag value is a
// shell-metacharacter and length hazard; ResolveText gives commands a
// companion "-file" flag that accepts a file path or "-" for stdin.
package cliflag

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ResolveText resolves a long-text CLI input from either an inline flag
// value or a companion "-file" flag. At most one of inlineValue / fileValue
// may be non-empty; supplying both is an error naming inlineFlag and
// fileFlag (flag names without the leading "--", used only to compose the
// error message).
//
// A fileValue of "-" reads the text from stdin. Any other fileValue is
// treated as a file path and read with os.ReadFile. A single trailing
// newline (or "\r\n") is trimmed from file/stdin content, matching the
// common convention that editors and heredocs append a final newline that
// isn't meant to be part of the value.
//
// If fileValue is empty, inlineValue is returned unchanged (including when
// both are empty, e.g. an optional flag the caller never set).
func ResolveText(inlineValue, fileValue, inlineFlag, fileFlag string) (string, error) {
	if inlineValue != "" && fileValue != "" {
		return "", fmt.Errorf("--%s and --%s are mutually exclusive", inlineFlag, fileFlag)
	}
	if fileValue == "" {
		return inlineValue, nil
	}

	var data []byte
	if fileValue == "-" {
		read, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("failed to read --%s from stdin: %w", fileFlag, err)
		}
		data = read
	} else {
		read, err := os.ReadFile(fileValue)
		if err != nil {
			return "", fmt.Errorf("failed to read --%s %q: %w", fileFlag, fileValue, err)
		}
		data = read
	}

	text := strings.TrimSuffix(string(data), "\n")
	text = strings.TrimSuffix(text, "\r")
	return text, nil
}
