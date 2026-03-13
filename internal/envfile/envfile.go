// Package envfile implements a layered .env file loader for sol.
//
// It reads a sphere-level .env file ($SOL_HOME/.env) and a world-level .env
// file ($SOL_HOME/{world}/.env), merging them so that world-level values
// override sphere-level values on key collision.
//
// Supported .env format:
//   - Lines starting with # are comments and are ignored.
//   - Empty (or whitespace-only) lines are ignored.
//   - Each key-value pair is on a single line: KEY=value
//   - Keys are everything before the first =, trimmed of whitespace.
//   - Values are everything after the first =, trimmed of whitespace.
//   - Values surrounded by matching " or ' quotes have the quotes stripped.
//   - No variable interpolation, no multiline values, no export prefix.
package envfile

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ParseError is returned when an .env file contains a syntax error.
// It carries the file path and line number for actionable diagnostics.
type ParseError struct {
	Path string
	Line int
	Msg  string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s:%d: %s", e.Path, e.Line, e.Msg)
}

// LoadEnv reads the sphere-level and world-level .env files and returns a
// merged map of key-value pairs. World-level values override sphere-level
// values on key collision. Missing .env files are not errors.
func LoadEnv(solHome, worldName string) (map[string]string, error) {
	merged := make(map[string]string)

	// Load sphere-level secrets: $SOL_HOME/.env
	spherePath := filepath.Join(solHome, ".env")
	sphere, err := parseFile(spherePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse sphere .env %q: %w", spherePath, err)
	}
	for k, v := range sphere {
		merged[k] = v
	}

	// Load world-level secrets: $SOL_HOME/{world}/.env
	if worldName != "" {
		worldPath := filepath.Join(solHome, worldName, ".env")
		world, err := parseFile(worldPath)
		if err != nil {
			return nil, fmt.Errorf("failed to parse world .env %q: %w", worldPath, err)
		}
		for k, v := range world {
			merged[k] = v
		}
	}

	return merged, nil
}

// ParseFile parses a single .env file at path and returns the key-value pairs.
// It returns a *ParseError if the file contains a syntax error (with file path
// and line number). Returns an error (not *ParseError) if the file cannot be
// opened. Returns an empty map for an empty file.
//
// Supported syntax:
//   - KEY=value (unquoted)
//   - KEY="value" (double-quoted)
//   - KEY='value' (single-quoted)
//   - KEY= (empty value)
//   - export KEY=value (export prefix stripped)
//   - # comment lines
//   - blank lines
func ParseFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("envfile: cannot open %s: %w", path, err)
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

		// Strip optional 'export ' prefix.
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		// Must contain '='.
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			return nil, &ParseError{
				Path: path,
				Line: lineNum,
				Msg:  fmt.Sprintf("missing '=' separator: %q", line),
			}
		}

		key := line[:idx]
		if key == "" {
			return nil, &ParseError{
				Path: path,
				Line: lineNum,
				Msg:  "empty key name",
			}
		}

		val := line[idx+1:]

		// Strip matching outer quotes.
		if len(val) >= 2 {
			first, last := val[0], val[len(val)-1]
			if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
				val = val[1 : len(val)-1]
			}
		}

		result[key] = val
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("envfile: read error in %s: %w", path, err)
	}

	return result, nil
}

// parseFile reads a .env file and returns a map of key-value pairs.
// Returns an empty map (not an error) if the file does not exist.
func parseFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return make(map[string]string), nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return parse(bufio.NewScanner(f))
}

// parse reads key-value pairs from a scanner line by line.
func parse(scanner *bufio.Scanner) (map[string]string, error) {
	result := make(map[string]string)
	for scanner.Scan() {
		line := scanner.Text()

		// Trim leading/trailing whitespace.
		trimmed := strings.TrimSpace(line)

		// Skip blank lines and comments.
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Split on the first '=' only.
		key, value, found := strings.Cut(trimmed, "=")
		if !found {
			// No '=' found — skip malformed line.
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if key == "" {
			// Empty key — skip.
			continue
		}

		// Strip surrounding quotes if present.
		value = stripQuotes(value)

		result[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// stripQuotes removes a matching pair of surrounding " or ' from a value.
// Only strips when both the first and last characters are the same quote type.
// Returns the original string if it is not quoted.
func stripQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	first := s[0]
	if (first == '"' || first == '\'') && s[len(s)-1] == first {
		return s[1 : len(s)-1]
	}
	return s
}
