package protocol

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/docs"
)

// InstallCLIReference writes docs/cli.md (embedded in the binary) to
// .claude/sol-cli-reference.md in the given directory. This gives agents
// access to the full CLI reference without needing the source repo.
func InstallCLIReference(dir string) error {
	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	path := filepath.Join(claudeDir, "sol-cli-reference.md")
	if err := os.WriteFile(path, []byte(docs.CLIReference), 0o644); err != nil {
		return fmt.Errorf("failed to write sol-cli-reference.md: %w", err)
	}
	return nil
}
