// Package envfile implements a layered .env file loader for sol.
//
// It reads a sphere-level .env file ($SOL_HOME/.env) and a world-level .env
// file ($SOL_HOME/{world}/.env), merging them so that world-level values
// override sphere-level values on key collision.
//
// The canonical format supports:
//   - Blank lines and # comments (ignored)
//   - Inline # comments in unquoted values (stripped when preceded by whitespace)
//   - Optional "export " prefix (stripped)
//   - Single or double quoted values (quotes stripped, inline # preserved);
//     anything after the closing quote on the same line is ignored
//   - Lines without "=" are rejected with *ParseError
//   - Lines with an empty key (e.g. "=value") are rejected with *ParseError
package envfile

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ParseError records a syntax error at a specific line in a .env file.
type ParseError struct {
	Path string
	Line int
	Msg  string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("env file %q line %d: %s", e.Path, e.Line, e.Msg)
}

// ParseFile parses a .env file and returns a map of key→value pairs.
//
// Rules:
//   - Blank lines and lines whose first non-space character is '#' are skipped.
//   - An optional "export " prefix is stripped before parsing.
//   - The first '=' on the line is the key/value separator.
//   - A line with no '=' is a syntax error; ParseFile returns a *ParseError.
//   - A line with an empty key (e.g. "=value") is a syntax error; ParseFile returns a *ParseError.
//   - Values wrapped in matching single or double quotes have those quotes
//     stripped. The matching close quote is the next occurrence of the same
//     quote character; anything between the surrounding quotes is preserved
//     literally (including '#'), and anything after the close quote on the
//     same line (typically whitespace and an inline '# comment') is ignored.
//   - In unquoted values, an inline '#' preceded by whitespace starts a
//     comment; the '#' and everything after it (plus the trailing whitespace
//     before it) is stripped. Inside quoted values, '#' is always literal.
func ParseFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open env file %q: %w", path, err)
	}
	defer f.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip blank lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip optional "export " prefix.
		if rest, ok := strings.CutPrefix(line, "export "); ok {
			line = strings.TrimSpace(rest)
		}

		// Require "=" separator.
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, &ParseError{
				Path: path,
				Line: lineNum,
				Msg:  fmt.Sprintf("missing '=' in %q", line),
			}
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		// Reject empty keys (e.g. "=value").
		if key == "" {
			return nil, &ParseError{
				Path: path,
				Line: lineNum,
				Msg:  fmt.Sprintf("empty key in %q", line),
			}
		}

		// Strip matching surrounding quotes (single or double). If the value
		// starts with a quote, the matching close quote is the next occurrence
		// of the same quote character. The substring between them is the
		// literal value (preserving '#' and other special characters); any
		// trailing content on the same line (e.g. whitespace and an inline
		// '# comment') is ignored. If no close quote is found the leading
		// quote is treated as a literal character and the value falls through
		// to the unquoted branch.
		quoted := false
		if len(value) >= 1 && (value[0] == '"' || value[0] == '\'') {
			q := value[0]
			if end := strings.IndexByte(value[1:], q); end >= 0 {
				value = value[1 : 1+end]
				quoted = true
			}
		}

		// For unquoted values, strip inline comments. A '#' only starts a
		// comment when preceded by whitespace; "value#nocomment" is a literal.
		if !quoted {
			if i := indexInlineComment(value); i >= 0 {
				value = strings.TrimRight(value[:i], " \t")
			}
		}

		result[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read env file %q: %w", path, err)
	}

	return result, nil
}

// indexInlineComment returns the index of the first '#' character in s that
// is preceded by an ASCII whitespace character (space or tab), or -1 if no
// such character exists. This identifies inline comment markers in unquoted
// .env values, where "value # comment" starts a comment but "value#literal"
// does not.
func indexInlineComment(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '#' && i > 0 && (s[i-1] == ' ' || s[i-1] == '\t') {
			return i
		}
	}
	return -1
}

// LoadEnv reads the sphere-level and world-level .env files and returns a
// merged map of key-value pairs. World-level values override sphere-level
// values on key collision. Missing .env files are not errors.
func LoadEnv(solHome, worldName string) (map[string]string, error) {
	merged := make(map[string]string)

	// Load sphere-level secrets: $SOL_HOME/.env
	spherePath := filepath.Join(solHome, ".env")
	sphere, err := ParseFile(spherePath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("failed to parse sphere .env %q: %w", spherePath, err)
	}
	for k, v := range sphere {
		merged[k] = v
	}

	// Load world-level secrets: $SOL_HOME/{world}/.env
	if worldName != "" {
		worldPath := filepath.Join(solHome, worldName, ".env")
		world, err := ParseFile(worldPath)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("failed to parse world .env %q: %w", worldPath, err)
		}
		for k, v := range world {
			merged[k] = v
		}
	}

	return merged, nil
}
