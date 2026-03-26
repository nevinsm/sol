package protocol

import (
	"fmt"
	"strings"

	"github.com/nevinsm/sol/internal/store"
)

// DepOutput describes a direct dependency's output for inclusion in the agent persona.
type DepOutput struct {
	WritID    string
	Title     string
	Kind      string
	OutputDir string
}

// WritSummary is a lightweight summary of a tethered writ, used for background listing
// in persistent agent personas.
type WritSummary struct {
	ID     string
	Title  string
	Kind   string
	Status store.WritStatus
}

// ClaudeMDContext holds the fields used to generate a CLAUDE.md file for an outpost agent.
type ClaudeMDContext struct {
	AgentName     string
	World         string
	WritID        string
	Title         string
	Description   string
	HasWorkflow   bool           // if true, include workflow protocol
	ModelTier     string         // "sonnet", "opus", "haiku" — informational
	QualityGates  []string       // commands to run before resolving (from world config)
	OutputDir     string         // persistent output directory for this writ
	Kind          string         // "code" (default), "analysis", etc.
	DirectDeps    []DepOutput    // upstream writs this writ depends on
	TetheredWrits []WritSummary  // all tethered writs — for persistent agent background listing
}

// isCodeKind returns true if the kind represents code work (or is the default empty kind).
func isCodeKind(kind string) bool {
	return kind == "" || kind == "code"
}

// GenerateClaudeMD returns the contents of a CLAUDE.md file for an outpost agent.
// Lean persona: identity, assignment, protocol, session resilience.
// Command syntax is provided via skills (installed separately).
func GenerateClaudeMD(ctx ClaudeMDContext) string {
	codeWrit := isCodeKind(ctx.Kind)

	protocolSection := ""
	if ctx.HasWorkflow {
		protocolSection = `## Protocol
1. Read your current workflow step.
2. Execute the step instructions.
3. Advance to the next step.
4. Repeat until all steps are done.
5. When the workflow is complete, run ` + "`sol resolve`" + `.
`
	} else {
		protocolSection = `## Protocol
1. Read your assignment above carefully.
2. Execute the work in this worktree.
3. When finished, run ` + "`sol resolve`" + `.
4. If you cannot complete the work, run ` + "`sol escalate \"description of problem\"`" + `.
`
	}

	modelSection := ""
	if ctx.ModelTier != "" {
		modelSection = fmt.Sprintf("\n## Model\nConfigured model tier: %s\n", ctx.ModelTier)
	}

	// Build output directory section based on kind.
	outputDirSection := ""
	if ctx.OutputDir != "" {
		if codeWrit {
			outputDirSection = fmt.Sprintf("\n## Output Directory\nPersistent output directory for this writ: `%s`\n"+
				"- Use for auxiliary output (test reports, benchmarks, etc.)\n"+
				"- This directory survives worktree cleanup\n", ctx.OutputDir)
		} else {
			outputDirSection = fmt.Sprintf("\n## Output Directory\nWrite your output to `%s`. This directory persists after your session ends.\n"+
				"- This is your primary output surface — all findings, reports, and structured data go here\n"+
				"- When finished, run `sol resolve` — this closes the writ. No branch or MR is created.\n", ctx.OutputDir)
		}
	}

	// Build direct dependencies section.
	depsSection := ""
	if len(ctx.DirectDeps) > 0 {
		depsSection = "\n## Direct Dependencies\nYour dependencies produced output in these directories. Read them for context before starting work.\n\n"
		for _, dep := range ctx.DirectDeps {
			depsSection += fmt.Sprintf("- **%s** (%s, kind: %s): `%s`\n", dep.Title, dep.WritID, dep.Kind, dep.OutputDir)
		}
	}

	// Build step 2 of the completion checklist based on kind.
	var commitStep string
	if codeWrit {
		commitStep = "Stage and commit changes with clear commit messages."
	} else {
		commitStep = "Review your output directory for completeness and structure."
	}

	// Build quality gate instructions for the completion checklist.
	var gateInstructions string
	if codeWrit {
		gateInstructions = "Run the project test suite before resolving."
		if len(ctx.QualityGates) > 0 {
			lines := ""
			for _, g := range ctx.QualityGates {
				lines += fmt.Sprintf("   - `%s`\n", g)
			}
			gateInstructions = fmt.Sprintf("Run quality gates (all must pass):\n%s", lines)
		}
	} else {
		gateInstructions = "Review your output in the output directory for completeness."
	}

	// Build session resilience section based on kind.
	var resilienceSection string
	if codeWrit {
		resilienceSection = `## Session Resilience
Your session can die at any time — context exhaustion, crash, infrastructure failure.
Code you commit to git survives. Everything else is lost.

Protect your work:
- Commit early and often with meaningful messages (not just "wip")
- After significant investigation or decisions, commit a progress note:
  ` + "`" + `git commit --allow-empty -m "progress: decided to use X approach because Y"` + "`" + `
- Before complex multi-step changes, commit what you have so far
- Your commit messages are your successor's primary context if you die mid-task
`
	} else {
		resilienceSection = fmt.Sprintf(`## Session Resilience
Your session can die at any time — context exhaustion, crash, infrastructure failure.
Files in your output directory survive. Everything else is lost.

Protect your work:
- Write findings to your output directory early — files there survive session death
- Save incremental progress rather than waiting until the end
- Structure output so partial results are still useful if your session dies
- Output directory: `+"`%s`"+`
`, ctx.OutputDir)
	}

	return fmt.Sprintf(`# Outpost Agent: %s (world: %s)

You are an outpost agent in a multi-agent orchestration system.
Your job is to execute the assigned writ.

## Warning
- If you do not run `+"`sol resolve`"+`, your tether is orphaned, forge never sees your MR, your worktree leaks until sentinel reaps it, and the writ stays stuck in tethered state. Always resolve.
- If you are stuck and cannot complete the work, run `+"`sol escalate`"+` — do not silently exit.
%s%s%s
## Your Assignment
- Writ: %s
- Title: %s
- Kind: %s
- Description: %s

## Approach
- Read existing code in the area you are modifying before making changes.
- Follow existing patterns and conventions in the codebase.
- Make focused, minimal changes — do not refactor surrounding code.

## Completion Checklist
1. %s
2. %s
3. Run `+"`sol resolve`"+` — MANDATORY FINAL STEP.

%s
%s
## Important
- You are working in an isolated git worktree. Commit your changes normally.
- Do not modify files outside this worktree.
- Do not attempt to interact with other agents directly.
- Do NOT use plan mode (EnterPlanMode) — it overrides your persona and context. Outline your approach directly in conversation instead.
`, ctx.AgentName, ctx.World, modelSection, outputDirSection, depsSection, ctx.WritID, ctx.Title, ctx.Kind, ctx.Description,
		gateInstructions, commitStep, protocolSection, resilienceSection)
}

// generatePersistentWritSection renders the multi-writ section for persistent agent personas.
// When activeWritID is set, the active writ gets full detail and others are listed as background.
// When activeWritID is empty, all writs are listed with a wait-for-activation message.
func generatePersistentWritSection(activeWritID, activeTitle, activeDesc, activeKind, activeOutput string,
	activeDeps []DepOutput, tetheredWrits []WritSummary) string {

	var b strings.Builder

	if activeWritID != "" {
		// Active writ: full detail section.
		kind := activeKind
		if kind == "" {
			kind = "code"
		}
		b.WriteString("\n## Active Writ\n")
		fmt.Fprintf(&b, "- Writ: %s\n", activeWritID)
		fmt.Fprintf(&b, "- Title: %s\n", activeTitle)
		fmt.Fprintf(&b, "- Kind: %s\n", kind)
		if activeOutput != "" {
			fmt.Fprintf(&b, "- Output: `%s`\n", activeOutput)
		}
		if activeDesc != "" {
			fmt.Fprintf(&b, "\n### Description\n%s\n", activeDesc)
		}
		if len(activeDeps) > 0 {
			b.WriteString("\n### Direct Dependencies\n")
			for _, dep := range activeDeps {
				fmt.Fprintf(&b, "- **%s** (%s, kind: %s): `%s`\n", dep.Title, dep.WritID, dep.Kind, dep.OutputDir)
			}
		}

		// Background writs: summary list (exclude the active writ).
		var background []WritSummary
		for _, w := range tetheredWrits {
			if w.ID != activeWritID {
				background = append(background, w)
			}
		}
		if len(background) > 0 {
			b.WriteString("\n## Background Writs\n")
			for _, w := range background {
				kind := w.Kind
				if kind == "" {
					kind = "code"
				}
				fmt.Fprintf(&b, "- %s — %s (kind: %s, status: %s)\n", w.ID, w.Title, kind, w.Status)
			}
		}

		b.WriteString("\n## Constraint\n")
		b.WriteString("Work only on your active writ. Background writs are listed for awareness. Do not act on them until the operator activates one.\n")
	} else {
		// No active writ: summary of all tethered writs + wait message.
		fmt.Fprintf(&b, "\n## Tethered Writs\nYou have %d tethered writs. Wait for the operator to activate one.\n\n", len(tetheredWrits))
		for _, w := range tetheredWrits {
			kind := w.Kind
			if kind == "" {
				kind = "code"
			}
			fmt.Fprintf(&b, "- %s — %s (kind: %s, status: %s)\n", w.ID, w.Title, kind, w.Status)
		}
	}

	return b.String()
}

// GuidedInitClaudeMDContext holds context for the guided init CLAUDE.md.
type GuidedInitClaudeMDContext struct {
	SOLHome   string
	SolBinary string // path to sol binary
}

// GenerateGuidedInitClaudeMD returns the CLAUDE.md for a guided init session.
func GenerateGuidedInitClaudeMD(ctx GuidedInitClaudeMDContext) string {
	return fmt.Sprintf(`# Sol Guided Setup

You are helping an operator set up sol for the first time.

## Your Role
You are a setup assistant. Your job is to help the operator configure sol
by asking questions conversationally and then running the setup command.

## What You Need to Collect
1. **World name** — a short identifier for their first project/world
   (e.g., "myapp", "backend", "frontend"). Must match: [a-zA-Z0-9][a-zA-Z0-9_-]*
   Cannot be: "store", "runtime", "sol"

2. **Source repository** (optional) — the path to the git repository
   they want agents to work on. Must be a directory that exists.

## Setup Command
Once you have the world name (and optionally source repo), run:

`+"```bash\n%s init --name=<world> --skip-checks"+`
# or with source repo:
%s init --name=<world> --source-repo=<path> --skip-checks
`+"```"+`

## Conversation Guidelines
- Be concise and friendly. This is a setup wizard, not a lecture.
- Ask one question at a time.
- Provide examples and suggestions when relevant.
- If the operator seems unsure about world names, suggest naming it
  after their project.
- Explain what sol does briefly if asked, but stay focused on setup.
- After successful setup, summarize what was created and suggest next steps.

## Important
- SOL_HOME will be: %s
- Do NOT modify any files directly. Use the sol CLI commands above.
- If setup fails, help the operator diagnose the issue.
- If they want to exit, let them — don't be pushy.
`, ctx.SolBinary, ctx.SolBinary, ctx.SOLHome)
}

// WritContext holds the multi-writ fields shared by persistent agent personas.
// EnvoyClaudeMDContext embeds this struct so writ-population logic lives in one place.
type WritContext struct {
	TetheredWrits []WritSummary // all tethered writs (for background listing)
	ActiveWritID  string        // currently active writ ID (empty if none)
	ActiveTitle   string        // active writ title
	ActiveDesc    string        // active writ description
	ActiveKind    string        // active writ kind
	ActiveOutput  string        // active writ output directory
	ActiveDeps    []DepOutput   // active writ direct dependencies
}

// EnvoyClaudeMDContext holds the fields used to generate a CLAUDE.md for an envoy agent.
type EnvoyClaudeMDContext struct {
	AgentName string
	World     string
	SolBinary string // path to sol binary (for CLI references)

	WritContext // embedded multi-writ fields for persistent agents
}

// GenerateEnvoyClaudeMD returns the contents of a CLAUDE.md for an envoy agent.
// Lean persona: identity, brief, work modes, persona, multi-writ.
// Command details are provided via skills (installed separately).
func GenerateEnvoyClaudeMD(ctx EnvoyClaudeMDContext) string {
	sol := ctx.SolBinary
	if sol == "" {
		sol = "sol"
	}

	content := fmt.Sprintf(`# Envoy: %s (world: %s)

## Identity
You are an envoy — a persistent, context-aware agent in world %q.
Your name is %q.
You maintain accumulated context in `+"`"+`.brief/memory.md`+"`"+`.

## Brief Maintenance
- Your brief (`+"`"+`.brief/memory.md`+"`"+`) is your persistent memory across sessions
- Keep it under 200 lines — consolidate older entries, focus on current state
- Update after significant decisions or discoveries, not just at session end — if your session crashes, a stale brief is all your successor gets
- On startup, review your brief — it may be stale if your last session crashed
- Organize naturally: what matters now at the top, historical context below
- **DO NOT** write to `+"`"+`~/.claude/projects/*/memory/`+"`"+` (Claude Code auto-memory) — use `+"`"+`.brief/memory.md`+"`"+` exclusively

## Work Flow — Three Modes
1. **Tethered work**: You may be assigned a writ. When tethered, focus on that writ. Resolve when done.
2. **Self-service**: Create your own writs and tether yourself.
3. **Freeform**: No tether — exploration, research, design. No resolve needed.

## Submitting Work
**All code changes MUST go through `+"`"+`sol resolve`+"`"+`.** Never use `+"`"+`git push`+"`"+` alone —
pushing your branch does not create a merge request. The forge pipeline is the
only path for code to reach the target branch.

## Guidelines
- You are human-supervised — ask when uncertain
- **Never push directly or bypass forge** — `+"`"+`sol resolve`+"`"+` is the only way to submit code
- Your worktree persists across sessions — keep it clean
- Do NOT use plan mode (EnterPlanMode) — it overrides your persona and context. Outline your approach directly in conversation instead.
`,
		ctx.AgentName, ctx.World,
		ctx.World, ctx.AgentName,
	)

	// Append multi-writ section if tethered writs exist.
	if len(ctx.TetheredWrits) > 0 {
		content += generatePersistentWritSection(ctx.ActiveWritID, ctx.ActiveTitle, ctx.ActiveDesc,
			ctx.ActiveKind, ctx.ActiveOutput, ctx.ActiveDeps, ctx.TetheredWrits)
	}

	return content
}


