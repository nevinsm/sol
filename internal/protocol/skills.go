package protocol

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"

	"github.com/nevinsm/sol/internal/adapter"
	"github.com/nevinsm/sol/internal/softfail"
)

// BuildSkills generates skill content for the given context and returns it as
// []adapter.Skill without writing to disk. Returns an error if the role is unknown.
//
// Per-skill render failures are tolerated: the failure is logged via
// [softfail.Log] and the bundle includes a visible marker
// (`[skill render failed: <name>]`) so the agent has a signal that the skill
// was skipped. This avoids a silent zero-skill bundle, which would otherwise
// leave the agent unaware of a regression in the template inputs.
func BuildSkills(ctx SkillContext) ([]adapter.Skill, error) {
	names, err := RoleSkills(ctx.Role)
	if err != nil {
		return nil, err
	}
	result := make([]adapter.Skill, 0, len(names))
	for _, name := range names {
		content, renderErr := generateSkill(name, ctx)
		if renderErr != nil {
			softfail.Log(nil, "protocol.render_skill", fmt.Errorf("skill %q: %w", name, renderErr))
			content = fmt.Sprintf("[skill render failed: %s — see sol logs]\n", name)
		}
		result = append(result, adapter.Skill{Name: name, Content: content})
	}
	return result, nil
}

// SkillContext holds common fields used when generating skill content for agents.
type SkillContext struct {
	World      string
	AgentName  string
	SolBinary  string // path to sol binary (defaults to "sol")
	Role       string // outpost, forge, envoy
	MainBranch string // world's main branch name; defaults to "main" if empty
}

func (ctx SkillContext) sol() string {
	if ctx.SolBinary != "" {
		return ctx.SolBinary
	}
	return "sol"
}

// skillData holds precomputed template data derived from SkillContext.
type skillData struct {
	Sol          string // ctx.sol() result, e.g. "sol"
	World        string
	AgentName    string
	Role         string
	ResolveFlags string // e.g. " --world=sol-dev --agent=Polaris" or "" for outpost
	MainBranch   string // world's main branch (e.g. "main", "develop"); defaults to "main"
}

func newSkillData(ctx SkillContext) skillData {
	mainBranch := ctx.MainBranch
	if mainBranch == "" {
		mainBranch = "main"
	}
	d := skillData{
		Sol:        ctx.sol(),
		World:      ctx.World,
		AgentName:  ctx.AgentName,
		Role:       ctx.Role,
		MainBranch: mainBranch,
	}
	if ctx.Role != "outpost" {
		d.ResolveFlags = " --world=" + ctx.World + " --agent=" + ctx.AgentName
	}
	return d
}

//go:embed skilltmpl/*.md.tmpl
var skillTemplates embed.FS

var parsedTemplates *template.Template

func init() {
	parsedTemplates = template.Must(template.ParseFS(skillTemplates, "skilltmpl/*.md.tmpl"))

	// Validate no duplicate skill names within any role.
	for role, skills := range roleSkillsMap {
		seen := make(map[string]bool, len(skills))
		for _, name := range skills {
			if seen[name] {
				panic(fmt.Sprintf("skills: duplicate skill %q in role %q", name, role))
			}
			seen[name] = true
		}
	}
}

// roleSkillsMap defines which skills belong to each role.
var roleSkillsMap = map[string][]string{
	"outpost": {"resolve-and-handoff"},
	"envoy":   {"resolve-and-submit", "writ-management", "dispatch", "handoff", "status-monitoring", "caravan-management", "world-operations", "mail"},
}

// RoleSkills returns the skill names for a given role.
// Returns an error if the role is not recognized.
func RoleSkills(role string) ([]string, error) {
	skills, ok := roleSkillsMap[role]
	if !ok {
		return nil, fmt.Errorf("skills: unknown role %q — no skills installed", role)
	}
	// Return a copy to prevent mutation.
	out := make([]string, len(skills))
	copy(out, skills)
	return out, nil
}

// generateSkill renders the named skill template with the given context.
// Returns the rendered content and a nil error on success, or an empty
// string and the underlying template error on failure. Callers are
// responsible for surfacing the error (see [BuildSkills]).
func generateSkill(name string, ctx SkillContext) (string, error) {
	data := newSkillData(ctx)
	var buf bytes.Buffer
	if err := parsedTemplates.ExecuteTemplate(&buf, name+".md.tmpl", data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
