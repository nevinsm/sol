package workflow

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed defaults/default-work/manifest.toml
//go:embed defaults/default-work/steps/01-load-context.md
//go:embed defaults/default-work/steps/02-implement.md
//go:embed defaults/default-work/steps/03-verify.md
//go:embed defaults/rule-of-five/manifest.toml
//go:embed defaults/code-review/manifest.toml
//go:embed defaults/plan-review/manifest.toml
//go:embed defaults/forge-patrol/manifest.toml
//go:embed defaults/forge-patrol/steps/01-unblock.md
//go:embed defaults/forge-patrol/steps/02-scan.md
//go:embed defaults/forge-patrol/steps/03-claim.md
//go:embed defaults/forge-patrol/steps/04-sync.md
//go:embed defaults/forge-patrol/steps/05-merge.md
//go:embed defaults/forge-patrol/steps/06-gates.md
//go:embed defaults/forge-patrol/steps/07-push.md
//go:embed defaults/forge-patrol/steps/08-handle-result.md
//go:embed defaults/forge-patrol/steps/09-loop.md
//go:embed defaults/forge-patrol/steps/10-health-check.md
var defaultFormulas embed.FS

// knownDefaults lists formula names that are embedded in the binary.
var knownDefaults = map[string]bool{
	"default-work":  true,
	"rule-of-five":  true,
	"code-review":   true,
	"plan-review":   true,
	"forge-patrol":  true,
}

// EnsureFormula checks if a formula exists on disk. If not and it's a
// known default formula, extract it from the embedded defaults.
// Returns the absolute path to the formula directory.
func EnsureFormula(formulaName string) (string, error) {
	dir := FormulaDir(formulaName)

	// If the formula directory exists, use it as-is.
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir, nil
	}

	// Check if it's a known default.
	if !knownDefaults[formulaName] {
		return "", fmt.Errorf("formula %q not found and is not a known default", formulaName)
	}

	// Extract from embedded defaults.
	root := filepath.Join("defaults", formulaName)
	if err := fs.WalkDir(defaultFormulas, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Compute destination path relative to the root.
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(dir, rel)

		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}

		data, err := defaultFormulas.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded file %q: %w", path, err)
		}
		return os.WriteFile(dest, data, 0o644)
	}); err != nil {
		return "", fmt.Errorf("failed to extract default formula %q: %w", formulaName, err)
	}

	return dir, nil
}
