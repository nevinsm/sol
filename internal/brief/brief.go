package brief

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// maxScanLineBytes is the maximum length of a single line bufio.Scanner will
// accept. Briefs are line-oriented Markdown; lines longer than 1 MB are
// pathological and will produce a read error rather than be silently truncated.
const maxScanLineBytes = 1 << 20

// Inject reads the brief file at path and returns its contents framed in
// <brief>...</brief> tags. If the file doesn't exist or is empty, returns
// empty string and nil error.
//
// Inject streams the file via bufio.Scanner and keeps only the LAST maxLines
// lines in memory. This makes truncation work for arbitrarily large brief
// files (envoys accumulate brief content over weeks; a wedged hard byte cap
// would permanently break their SessionStart hook). If truncation occurs, a
// notice is appended to the returned content and a warning is logged to
// stderr so the operator notices the brief is overdue for consolidation.
func Inject(path string, maxLines int) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to stat brief %q: %w", path, err)
	}
	if info.Size() == 0 {
		return "", nil
	}
	if maxLines < 1 {
		maxLines = 1
	}

	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open brief %q: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), maxScanLineBytes)

	// Ring buffer holding the last maxLines lines we have seen.
	tail := make([]string, 0, maxLines)
	totalLines := 0
	for scanner.Scan() {
		totalLines++
		if len(tail) < maxLines {
			tail = append(tail, scanner.Text())
			continue
		}
		copy(tail, tail[1:])
		tail[len(tail)-1] = scanner.Text()
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read brief %q: %w", path, err)
	}

	if totalLines == 0 {
		return "", nil
	}

	// Treat all-whitespace content as empty (matches prior behavior).
	allBlank := true
	for _, line := range tail {
		if strings.TrimSpace(line) != "" {
			allBlank = false
			break
		}
	}
	if allBlank {
		return "", nil
	}

	truncated := totalLines > maxLines
	if truncated {
		fmt.Fprintf(os.Stderr,
			"brief: %q is %d bytes / %d lines; truncated to last %d lines for injection — consolidate the brief\n",
			path, info.Size(), totalLines, maxLines)
	}

	var b strings.Builder
	b.WriteString("<brief>\n")
	b.WriteString(strings.Join(tail, "\n"))
	if truncated {
		b.WriteString("\n---\n")
		fmt.Fprintf(&b,
			"TRUNCATED: Brief exceeded %d lines (%d total). Showing the last %d lines. Read the full file at %s and consolidate.",
			maxLines, totalLines, maxLines, path)
	}
	b.WriteString("\n</brief>")

	return b.String(), nil
}
