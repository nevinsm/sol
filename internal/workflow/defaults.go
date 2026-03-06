package workflow

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
	"github.com/nevinsm/sol/internal/config"
)

//go:embed defaults/default-work/manifest.toml
//go:embed defaults/default-work/steps/01-load-context.md
//go:embed defaults/default-work/steps/02-implement.md
//go:embed defaults/default-work/steps/03-verify.md
//go:embed defaults/rule-of-five/manifest.toml
//go:embed defaults/code-review/manifest.toml
//go:embed defaults/plan-review/manifest.toml
//go:embed defaults/guided-design/manifest.toml
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
//go:embed defaults/thorough-work/manifest.toml
//go:embed defaults/thorough-work/steps/01-design.md
//go:embed defaults/thorough-work/steps/02-implement.md
//go:embed defaults/thorough-work/steps/03-review.md
//go:embed defaults/thorough-work/steps/04-test.md
//go:embed defaults/thorough-work/steps/05-submit.md
//go:embed defaults/idea-to-plan/manifest.toml
//go:embed defaults/idea-to-plan/steps/01-understand-intent.md
//go:embed defaults/idea-to-plan/steps/02-review-requirements.md
//go:embed defaults/idea-to-plan/steps/03-explore-design.md
//go:embed defaults/idea-to-plan/steps/04-review-plan.md
//go:embed defaults/idea-to-plan/steps/05-create-work-items.md
//go:embed defaults/idea-to-plan/steps/06-summarize.md
//go:embed defaults/deep-scan/manifest.toml
//go:embed defaults/deep-scan/steps/01-orient.md
//go:embed defaults/deep-scan/steps/02-survey.md
//go:embed defaults/deep-scan/steps/03-isolate.md
//go:embed defaults/deep-scan/steps/04-document.md
//go:embed defaults/deep-scan/steps/05-chart.md
var defaultFormulas embed.FS

// knownDefaults lists formula names that are embedded in the binary.
var knownDefaults = map[string]bool{
	"default-work":   true,
	"rule-of-five":   true,
	"code-review":    true,
	"plan-review":    true,
	"guided-design":  true,
	"forge-patrol":   true,
	"thorough-work":  true,
	"idea-to-plan":   true,
	"deep-scan":      true,
}

// FormulaTier indicates which tier resolved a formula.
type FormulaTier string

const (
	// TierProject is a project-level workflow from {repo}/.sol/workflows/{name}/.
	TierProject FormulaTier = "project"
	// TierUser is a user-level formula from $SOL_HOME/formulas/{name}/.
	TierUser FormulaTier = "user"
	// TierEmbedded is a built-in formula extracted from go:embed defaults.
	TierEmbedded FormulaTier = "embedded"
)

// FormulaResolution is the result of resolving a formula name to a path.
type FormulaResolution struct {
	Path string
	Tier FormulaTier
}

// FormulaEntry describes a formula discovered during tier scanning.
type FormulaEntry struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Tier        FormulaTier `json:"tier"`
	Description string      `json:"description"`
	Shadowed    bool        `json:"shadowed,omitempty"`
}

// EnsureFormula resolves a formula using three-tier lookup:
//  1. Project-level: {repoPath}/.sol/workflows/{name}/ — project-specific workflows
//  2. User-level: $SOL_HOME/formulas/{name}/ — operator customizations
//  3. Embedded: go:embed defaults — built-in formulas (extracted on first use)
//
// Resolution is first-match-wins: project > user > embedded.
// Pass an empty repoPath to skip the project tier.
func EnsureFormula(formulaName, repoPath string) (*FormulaResolution, error) {
	// Tier 1: Project-level — check {repoPath}/.sol/workflows/{name}/.
	if repoPath != "" {
		projectDir := ProjectFormulaDir(repoPath, formulaName)
		if info, err := os.Stat(projectDir); err == nil && info.IsDir() {
			return &FormulaResolution{Path: projectDir, Tier: TierProject}, nil
		}
	}

	// Tier 2: User-level — check $SOL_HOME/formulas/{name}/.
	userDir := FormulaDir(formulaName)
	if info, err := os.Stat(userDir); err == nil && info.IsDir() {
		return &FormulaResolution{Path: userDir, Tier: TierUser}, nil
	}

	// Tier 3: Embedded — extract known default to user-level path.
	if !knownDefaults[formulaName] {
		return nil, fmt.Errorf("formula %q not found and is not a known default", formulaName)
	}

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
		dest := filepath.Join(userDir, rel)

		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}

		data, err := defaultFormulas.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded file %q: %w", path, err)
		}
		return os.WriteFile(dest, data, 0o644)
	}); err != nil {
		return nil, fmt.Errorf("failed to extract default formula %q: %w", formulaName, err)
	}

	return &FormulaResolution{Path: userDir, Tier: TierEmbedded}, nil
}

// ProjectFormulaDir returns the project-level workflow path.
// {repoPath}/.sol/workflows/{formulaName}/
func ProjectFormulaDir(repoPath, formulaName string) string {
	return filepath.Join(repoPath, ".sol", "workflows", formulaName)
}

// ListFormulas discovers all available formulas across the three resolution
// tiers: project > user > embedded. repoPath may be empty to skip the
// project tier. Returns entries sorted by name, with shadowed entries
// (overridden by a higher-priority tier) marked.
func ListFormulas(repoPath string) ([]FormulaEntry, error) {
	entries := []FormulaEntry{}
	seen := make(map[string]bool)

	// Tier 1: Project-level — scan {repoPath}/.sol/workflows/.
	if repoPath != "" {
		projectBase := filepath.Join(repoPath, ".sol", "workflows")
		if dirEntries, err := os.ReadDir(projectBase); err == nil {
			for _, de := range dirEntries {
				if !de.IsDir() {
					continue
				}
				name := de.Name()
				dir := filepath.Join(projectBase, name)
				m, err := LoadManifest(dir)
				if err != nil {
					continue
				}
				entries = append(entries, FormulaEntry{
					Name:        name,
					Type:        m.Type,
					Tier:        TierProject,
					Description: m.Description,
				})
				seen[name] = true
			}
		}
	}

	// Tier 2: User-level — scan $SOL_HOME/formulas/.
	userBase := filepath.Join(config.Home(), "formulas")
	if dirEntries, err := os.ReadDir(userBase); err == nil {
		for _, de := range dirEntries {
			if !de.IsDir() {
				continue
			}
			name := de.Name()
			dir := filepath.Join(userBase, name)
			m, err := LoadManifest(dir)
			if err != nil {
				continue
			}
			entry := FormulaEntry{
				Name:        name,
				Type:        m.Type,
				Tier:        TierUser,
				Description: m.Description,
			}
			if seen[name] {
				entry.Shadowed = true
			} else {
				seen[name] = true
			}
			entries = append(entries, entry)
		}
	}

	// Tier 3: Embedded — list known defaults not already found.
	for name := range knownDefaults {
		m, err := loadEmbeddedManifest(name)
		if err != nil {
			continue
		}
		entry := FormulaEntry{
			Name:        name,
			Type:        m.Type,
			Tier:        TierEmbedded,
			Description: m.Description,
		}
		if seen[name] {
			entry.Shadowed = true
		} else {
			seen[name] = true
		}
		entries = append(entries, entry)
	}

	// Sort by name, then by tier priority for stable output.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name != entries[j].Name {
			return entries[i].Name < entries[j].Name
		}
		return tierPriority(entries[i].Tier) < tierPriority(entries[j].Tier)
	})

	return entries, nil
}

// loadEmbeddedManifest reads and parses a manifest from the embedded FS
// without extracting it to disk.
func loadEmbeddedManifest(name string) (*Manifest, error) {
	data, err := defaultFormulas.ReadFile(filepath.Join("defaults", name, "manifest.toml"))
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded manifest for %q: %w", name, err)
	}
	var m Manifest
	if _, err := toml.Decode(string(data), &m); err != nil {
		return nil, fmt.Errorf("failed to parse embedded manifest for %q: %w", name, err)
	}
	return &m, nil
}

// tierPriority returns the sort priority for a tier (lower = higher priority).
func tierPriority(t FormulaTier) int {
	switch t {
	case TierProject:
		return 0
	case TierUser:
		return 1
	case TierEmbedded:
		return 2
	default:
		return 3
	}
}
