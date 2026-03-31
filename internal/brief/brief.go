package brief

import (
	"fmt"
	"os"
	"strings"
)

// Inject reads the brief file at path and returns its contents framed in
// <brief>...</brief> tags. If the file doesn't exist or is empty, returns
// empty string and nil error. If content exceeds maxLines, truncates and
// appends a notice.
// maxBriefBytes is the maximum file size (1 MB) that Inject will read.
// Files larger than this are rejected as a safety net before reading into memory.
const maxBriefBytes = 1 << 20

func Inject(path string, maxLines int) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to stat brief %q: %w", path, err)
	}
	if info.Size() > maxBriefBytes {
		return "", fmt.Errorf("brief %q exceeds maximum size (%d bytes > %d)", path, info.Size(), maxBriefBytes)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read brief %q: %w", path, err)
	}

	content := string(data)
	if strings.TrimSpace(content) == "" {
		return "", nil
	}

	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	truncated := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}

	var b strings.Builder
	b.WriteString("<brief>\n")
	b.WriteString(strings.Join(lines, "\n"))
	if truncated {
		b.WriteString("\n---\n")
		b.WriteString(fmt.Sprintf("TRUNCATED: Brief exceeded %d lines. Read the full file at %s and consolidate.", maxLines, path))
	}
	b.WriteString("\n</brief>")

	return b.String(), nil
}

