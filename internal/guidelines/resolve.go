package guidelines

import (
	"fmt"
	"os"
)

// Tier indicates which tier resolved a guidelines template.
type Tier string

const (
	TierProject  Tier = "project"
	TierUser     Tier = "user"
	TierEmbedded Tier = "embedded"
)

// Resolution is the result of resolving a guidelines name to content.
type Resolution struct {
	Name    string
	Content []byte
	Tier    Tier
}

// Resolve resolves a guidelines template using three-tier lookup:
//  1. Project-level: {repoPath}/.sol/guidelines/{name}.md
//  2. User-level: $SOL_HOME/guidelines/{name}.md
//  3. Embedded: go:embed defaults (extracted to user tier on first use)
//
// Pass an empty repoPath to skip the project tier.
func Resolve(name, repoPath string) (*Resolution, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}

	// Tier 1: Project-level.
	if repoPath != "" {
		path := projectFilePath(repoPath, name)
		if data, err := os.ReadFile(path); err == nil {
			return &Resolution{Name: name, Content: data, Tier: TierProject}, nil
		}
	}

	// Tier 2: User-level.
	userPath := userFilePath(name)
	if data, err := os.ReadFile(userPath); err == nil {
		return &Resolution{Name: name, Content: data, Tier: TierUser}, nil
	}

	// Tier 3: Embedded — extract to user tier on first use.
	if !knownDefaults[name] {
		return nil, fmt.Errorf("guidelines template %q not found", name)
	}

	extractedPath, err := extractToUser(name)
	if err != nil {
		return nil, fmt.Errorf("failed to extract embedded guidelines %q: %w", name, err)
	}

	data, err := os.ReadFile(extractedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read extracted guidelines %q: %w", extractedPath, err)
	}

	return &Resolution{Name: name, Content: data, Tier: TierEmbedded}, nil
}

// ResolveTemplateName determines which guidelines template to use for a given
// writ kind. Resolution order:
//  1. Explicit name (from --guidelines flag) — returned as-is.
//  2. World config [guidelines] mapping — look up kind in the map.
//  3. Built-in fallback: kind "code" or "" → "default", anything else → "analysis".
func ResolveTemplateName(explicit string, kind string, worldMapping map[string]string) string {
	if explicit != "" {
		return explicit
	}

	if worldMapping != nil {
		if name, ok := worldMapping[kind]; ok {
			return name
		}
	}

	// Built-in fallback.
	if kind == "" || kind == "code" {
		return "default"
	}
	return "analysis"
}
