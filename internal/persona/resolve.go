package persona

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/softfail"
)

// validPersonaName matches alphanumeric names with hyphens and underscores.
var validPersonaName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// ValidateName checks that a persona name is safe for use in file paths.
func ValidateName(name string) error {
	if !validPersonaName.MatchString(name) {
		return fmt.Errorf("invalid persona name %q: must match [a-zA-Z0-9][a-zA-Z0-9_-]*", name)
	}
	return nil
}

// Tier indicates which tier resolved a persona template.
type Tier string

const (
	// TierProject is a project-level persona from {repo}/.sol/personas/{name}.md.
	TierProject Tier = "project"
	// TierUser is a user-level persona from $SOL_HOME/personas/{name}.md.
	TierUser Tier = "user"
	// TierEmbedded is a built-in persona from go:embed defaults.
	TierEmbedded Tier = "embedded"
)

// Resolution is the result of resolving a persona name to content.
type Resolution struct {
	Content []byte
	Tier    Tier
}

// ProjectPath returns the project-level persona file path.
func ProjectPath(repoPath, name string) string {
	return filepath.Join(repoPath, ".sol", "personas", name+".md")
}

// UserPath returns the user-level persona file path.
func UserPath(name string) string {
	return filepath.Join(config.Home(), "personas", name+".md")
}

// UserDir returns the user-level personas directory.
func UserDir() string {
	return filepath.Join(config.Home(), "personas")
}

// Resolve resolves a persona template using three-tier lookup:
//  1. Project-level: {repoPath}/.sol/personas/{name}.md
//  2. User-level: $SOL_HOME/personas/{name}.md
//  3. Embedded: go:embed defaults/{name}.md
//
// Resolution is first-match-wins: project > user > embedded.
// Pass an empty repoPath to skip the project tier.
func Resolve(name, repoPath string) (*Resolution, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}

	// Tier 1: Project-level.
	if repoPath != "" {
		projectFile := ProjectPath(repoPath, name)
		data, err := os.ReadFile(projectFile)
		if err == nil && len(data) > 0 {
			return &Resolution{Content: data, Tier: TierProject}, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			// Permission denied / IO error: log and fall through to next tier
			// rather than silently dropping the persona.
			softfail.Log(nil, "persona.Resolve: read project tier failed", err)
		}
	}

	// Tier 2: User-level.
	userFile := UserPath(name)
	data, err := os.ReadFile(userFile)
	if err == nil && len(data) > 0 {
		return &Resolution{Content: data, Tier: TierUser}, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		softfail.Log(nil, "persona.Resolve: read user tier failed", err)
	}

	// Tier 3: Embedded.
	if !knownDefaults[name] {
		return nil, fmt.Errorf("persona %q not found (checked project, user, and built-in templates)", name)
	}

	data, err = defaultPersonas.ReadFile("defaults/" + name + ".md")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded persona %q: %w", name, err)
	}

	return &Resolution{Content: data, Tier: TierEmbedded}, nil
}
