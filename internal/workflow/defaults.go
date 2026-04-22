package workflow

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/nevinsm/sol/internal/config"
)

// embeddedVersionFile is the marker file written to auto-extracted workflow
// directories. It contains the SHA-256 hash of the embedded content so that
// stale extractions can be detected after a binary upgrade.
const embeddedVersionFile = ".embedded-version"

// validWorkflowName matches alphanumeric names with hyphens and underscores.
var validWorkflowName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// ValidateName checks that a workflow name is safe for use in file
// paths. It rejects names containing path separators, traversal sequences,
// or leading dots. A valid name matches [a-zA-Z0-9][a-zA-Z0-9_-]*.
func ValidateName(name string) error {
	if !validWorkflowName.MatchString(name) {
		return fmt.Errorf("invalid workflow name %q: must not contain path separators or traversal sequences", name)
	}
	return nil
}

//go:embed all:defaults
var defaultWorkflows embed.FS

// knownDefaults lists workflow names that are embedded in the binary.
var knownDefaults = map[string]bool{
	"rule-of-five":   true,
	"code-review":    true,
	"plan-review":    true,
	"guided-design":  true,
	"prd-review":     true,
	"security-audit": true,
}

// Tier indicates which tier resolved a workflow.
type Tier string

const (
	// TierProject is a project-level workflow from {repo}/.sol/workflows/{name}/.
	TierProject Tier = "project"
	// TierUser is a user-level workflow from $SOL_HOME/workflows/{name}/.
	TierUser Tier = "user"
	// TierEmbedded is a built-in workflow extracted from go:embed defaults.
	TierEmbedded Tier = "embedded"
	// TierLocal is used when loading a workflow from an arbitrary directory path.
	TierLocal Tier = "local"
)

// Resolution is the result of resolving a workflow name to a path.
type Resolution struct {
	Path string
	Tier Tier
}

// Entry describes a workflow discovered during tier scanning.
type Entry struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Tier        Tier   `json:"tier"`
	Description string `json:"description"`
	Shadowed    bool   `json:"shadowed,omitempty"`
}

// Resolve resolves a workflow using three-tier lookup:
//  1. Project-level: {repoPath}/.sol/workflows/{name}/ — project-specific workflows
//  2. User-level: $SOL_HOME/workflows/{name}/ — operator customizations
//  3. Embedded: go:embed defaults — built-in workflows (extracted on first use)
//
// Resolution is first-match-wins: project > user > embedded.
// Pass an empty repoPath to skip the project tier.
func Resolve(workflowName, repoPath string) (*Resolution, error) {
	if err := ValidateName(workflowName); err != nil {
		return nil, err
	}

	// Tier 1: Project-level — check {repoPath}/.sol/workflows/{name}/.
	if repoPath != "" {
		projectDir := ProjectDir(repoPath, workflowName)
		if info, err := os.Stat(projectDir); err == nil && info.IsDir() {
			return &Resolution{Path: projectDir, Tier: TierProject}, nil
		}
	}

	// Tier 2: User-level — check $SOL_HOME/workflows/{name}/.
	userDir := Dir(workflowName)
	if info, err := os.Stat(userDir); err == nil && info.IsDir() {
		// If this directory was auto-extracted from an embedded workflow,
		// check whether the embedded version has changed (e.g. binary
		// upgrade). A stale extraction is removed and re-extracted below.
		versionPath := filepath.Join(userDir, embeddedVersionFile)
		if stored, err := os.ReadFile(versionPath); err == nil && knownDefaults[workflowName] {
			currentHash := embeddedHash(workflowName)
			if string(stored) != currentHash {
				// Stale — remove old extraction and re-extract.
				if err := os.RemoveAll(userDir); err != nil {
					return nil, fmt.Errorf("failed to remove stale workflow %q: %w", workflowName, err)
				}
				if err := extractEmbedded(workflowName, userDir); err != nil {
					return nil, fmt.Errorf("failed to re-extract embedded workflow %q: %w", workflowName, err)
				}
				if err := writeVersionMarker(workflowName, userDir); err != nil {
					return nil, fmt.Errorf("failed to write version marker for %q: %w", workflowName, err)
				}
			}
			return &Resolution{Path: userDir, Tier: TierEmbedded}, nil
		}
		// No version marker — this is a user-created or ejected workflow.
		return &Resolution{Path: userDir, Tier: TierUser}, nil
	}

	// Tier 3: Embedded — extract known default to user-level path.
	if !knownDefaults[workflowName] {
		return nil, fmt.Errorf("workflow %q not found and is not a known default", workflowName)
	}

	if err := extractEmbedded(workflowName, userDir); err != nil {
		return nil, fmt.Errorf("failed to extract embedded workflow %q: %w", workflowName, err)
	}
	if err := writeVersionMarker(workflowName, userDir); err != nil {
		return nil, fmt.Errorf("failed to write version marker for %q: %w", workflowName, err)
	}

	return &Resolution{Path: userDir, Tier: TierEmbedded}, nil
}

// extractEmbedded walks the embedded FS for the named workflow and writes
// all files to targetDir. This is the shared extraction logic used by both
// Resolve (implicit extraction) and Eject (explicit extraction).
func extractEmbedded(name, targetDir string) error {
	root := filepath.Join("defaults", name)
	if err := fs.WalkDir(defaultWorkflows, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Compute destination path relative to the root.
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(targetDir, rel)

		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}

		data, err := defaultWorkflows.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded file %q: %w", path, err)
		}
		return os.WriteFile(dest, data, 0o644)
	}); err != nil {
		return fmt.Errorf("failed to extract default workflow %q: %w", name, err)
	}
	return nil
}

// embeddedHash computes a deterministic SHA-256 hash over all files in an
// embedded workflow. The hash covers relative paths and file contents, so any
// change to the embedded defaults produces a different hash.
func embeddedHash(name string) string {
	h := sha256.New()
	root := filepath.Join("defaults", name)
	_ = fs.WalkDir(defaultWorkflows, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := defaultWorkflows.ReadFile(path)
		if err != nil {
			return err
		}
		h.Write([]byte(rel))
		h.Write(data)
		return nil
	})
	return fmt.Sprintf("%x", h.Sum(nil))
}

// writeVersionMarker writes the current embedded hash to the version marker
// file inside targetDir. This marks the directory as auto-extracted so that
// future Resolve calls can detect staleness.
func writeVersionMarker(name, targetDir string) error {
	hash := embeddedHash(name)
	return os.WriteFile(filepath.Join(targetDir, embeddedVersionFile), []byte(hash), 0o644)
}

// Eject copies an embedded workflow to the user or project tier for
// customization. If force is true and the target directory already exists,
// the existing directory is renamed to {name}.bak-{RFC3339timestamp} before
// extracting a fresh copy.
//
// Pass a non-empty repoPath to eject to the project tier instead of the
// user tier.
func Eject(name, repoPath string, force bool) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}

	if !knownDefaults[name] {
		return "", fmt.Errorf("workflow %q is not an embedded workflow — nothing to eject", name)
	}

	// Compute target directory.
	var targetDir string
	if repoPath != "" {
		targetDir = ProjectDir(repoPath, name)
	} else {
		targetDir = Dir(name)
	}

	// Check if target already exists.
	if info, err := os.Stat(targetDir); err == nil && info.IsDir() {
		if !force {
			return "", fmt.Errorf("workflow directory already exists: %s", targetDir)
		}
		// Backup existing directory.
		backupDir := filepath.Join(filepath.Dir(targetDir),
			name+".bak-"+time.Now().UTC().Format("2006-01-02T15-04-05Z"))
		if err := os.Rename(targetDir, backupDir); err != nil {
			return "", fmt.Errorf("failed to backup existing workflow directory: %w", err)
		}
	}

	if err := extractEmbedded(name, targetDir); err != nil {
		return "", fmt.Errorf("failed to extract embedded workflow %q: %w", name, err)
	}

	return targetDir, nil
}

// ProjectDir returns the project-level workflow path.
// {repoPath}/.sol/workflows/{workflowName}/
func ProjectDir(repoPath, workflowName string) string {
	return filepath.Join(repoPath, ".sol", "workflows", workflowName)
}

// List discovers all available workflows across the three resolution
// tiers: project > user > embedded. repoPath may be empty to skip the
// project tier. Returns entries sorted by name, with shadowed entries
// (overridden by a higher-priority tier) marked.
func List(repoPath string) ([]Entry, error) {
	entries := []Entry{}
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
				entries = append(entries, Entry{
					Name:        name,
					Type:        m.Type,
					Tier:        TierProject,
					Description: m.Description,
				})
				seen[name] = true
			}
		}
	}

	// Tier 2: User-level — scan $SOL_HOME/workflows/.
	userBase := filepath.Join(config.Home(), "workflows")
	if dirEntries, err := os.ReadDir(userBase); err == nil {
		for _, de := range dirEntries {
			if !de.IsDir() {
				continue
			}
			name := de.Name()
			dir := filepath.Join(userBase, name)

			// Skip auto-extracted embedded workflows — the embedded-tier
			// scan below will pick them up with the correct label.
			versionPath := filepath.Join(dir, embeddedVersionFile)
			if _, err := os.Stat(versionPath); err == nil && knownDefaults[name] {
				continue
			}

			m, err := LoadManifest(dir)
			if err != nil {
				continue
			}
			entry := Entry{
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
		entry := Entry{
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
	data, err := defaultWorkflows.ReadFile(filepath.Join("defaults", name, "manifest.toml"))
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
func tierPriority(t Tier) int {
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

// Skeleton manifest templates for workflow init scaffolding.
// The placeholder {name} is replaced with the actual workflow name.
const skeletonWorkflow = `name = "{name}"
type = "workflow"
description = ""

[variables]

[[steps]]
id = "start"
title = "Start"
instructions = "steps/01-start.md"
`

// defaultStepContent is the placeholder content for the initial step file.
const defaultStepContent = `# Start

Describe what this step should do.
`

// Init creates a new workflow scaffold at the appropriate tier.
// workflowType must be "workflow".
// If project is true, the workflow is created in the project tier at
// {repoPath}/.sol/workflows/{name}/; otherwise it goes to the user tier
// at $SOL_HOME/workflows/{name}/.
func Init(name, workflowType, repoPath string, project bool) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}

	// Select skeleton template.
	var skeleton string
	switch workflowType {
	case "", "workflow":
		skeleton = skeletonWorkflow
	default:
		return "", fmt.Errorf("invalid workflow type %q: must be workflow", workflowType)
	}

	// Determine target directory.
	var dir string
	if project {
		if repoPath == "" {
			return "", fmt.Errorf("--project requires --world to determine the managed repo path")
		}
		dir = ProjectDir(repoPath, name)
	} else {
		dir = Dir(name)
	}

	// Check if directory already exists.
	if _, err := os.Stat(dir); err == nil {
		return "", fmt.Errorf("workflow directory already exists: %s", dir)
	}

	// Create directory structure.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create workflow directory: %w", err)
	}

	// Write manifest.toml with name substituted.
	manifest := strings.ReplaceAll(skeleton, "{name}", name)
	manifestPath := filepath.Join(dir, "manifest.toml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		// Clean up on failure.
		os.RemoveAll(dir)
		return "", fmt.Errorf("failed to write manifest.toml: %w", err)
	}

	// Create steps/ directory with placeholder step file.
	stepsDir := filepath.Join(dir, "steps")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("failed to create steps directory: %w", err)
	}
	stepPath := filepath.Join(stepsDir, "01-start.md")
	if err := os.WriteFile(stepPath, []byte(defaultStepContent), 0o644); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("failed to write step file: %w", err)
	}

	return dir, nil
}
