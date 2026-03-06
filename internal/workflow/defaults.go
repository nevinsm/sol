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
