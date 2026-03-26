package guidelines

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/nevinsm/sol/internal/config"
)

// validName matches alphanumeric names with hyphens and underscores.
var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// ValidateName checks that a guidelines template name is safe for use in
// file paths. Rejects names containing path separators, traversal sequences,
// or leading dots.
func ValidateName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid guidelines name %q: must match [a-zA-Z0-9][a-zA-Z0-9_-]*", name)
	}
	return nil
}

//go:embed defaults/default.md
//go:embed defaults/analysis.md
//go:embed defaults/investigation.md
var defaultGuidelines embed.FS

// knownDefaults lists guidelines names embedded in the binary.
var knownDefaults = map[string]bool{
	"default":       true,
	"analysis":      true,
	"investigation": true,
}

// IsKnownDefault returns true if the name is a built-in embedded template.
func IsKnownDefault(name string) bool {
	return knownDefaults[name]
}

// readEmbedded reads an embedded guidelines template by name.
func readEmbedded(name string) ([]byte, error) {
	path := filepath.Join("defaults", name+".md")
	data, err := defaultGuidelines.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded guidelines %q: %w", name, err)
	}
	return data, nil
}

// extractToUser extracts an embedded template to the user tier so it can be
// customized. Only writes if the file doesn't already exist.
func extractToUser(name string) (string, error) {
	userPath := userFilePath(name)

	// Already extracted — return existing.
	if _, err := os.Stat(userPath); err == nil {
		return userPath, nil
	}

	data, err := readEmbedded(name)
	if err != nil {
		return "", err
	}

	dir := filepath.Dir(userPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create guidelines directory %q: %w", dir, err)
	}

	if err := os.WriteFile(userPath, data, 0o644); err != nil {
		return "", fmt.Errorf("failed to write guidelines file %q: %w", userPath, err)
	}

	return userPath, nil
}

// projectFilePath returns the project-level guidelines path.
func projectFilePath(repoPath, name string) string {
	return filepath.Join(repoPath, ".sol", "guidelines", name+".md")
}

// userFilePath returns the user-level guidelines path.
func userFilePath(name string) string {
	return filepath.Join(config.Home(), "guidelines", name+".md")
}
