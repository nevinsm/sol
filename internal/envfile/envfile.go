// Package envfile implements a layered .env file loader for sol.
//
// It reads a sphere-level .env file ($SOL_HOME/.env) and a world-level .env
// file ($SOL_HOME/{world}/.env), merging them so that world-level values
// override sphere-level values on key collision.
//
// The canonical format supports:
//   - Blank lines and # comments (ignored)
//   - Optional "export " prefix (stripped)
//   - Single or double quoted values (quotes stripped)
//   - Lines without "=" are rejected with *ParseError
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
//   - Values wrapped in matching single or double quotes have those quotes stripped.
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

		// Strip matching surrounding quotes (single or double).
		if len(value) >= 2 {
			q := value[0]
			if (q == '"' || q == '\'') && value[len(value)-1] == q {
				value = value[1 : len(value)-1]
			}
		}

		result[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read env file %q: %w", path, err)
	}

	return result, nil
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
