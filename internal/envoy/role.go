package envoy

import (
	"fmt"
	"os"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// RoleConfig returns the startup.RoleConfig for the envoy role.
func RoleConfig() startup.RoleConfig {
	return startup.RoleConfig{
		Role:                "envoy",
		WorktreeDir:         func(w, a string) string { return WorktreePath(w, a) },
		Persona:             envoyPersona,
		Hooks:               envoyHooks,
		SystemPromptContent: protocol.EnvoySystemPrompt,
		ReplacePrompt:       false, // append mode — keep default system prompt
		PrimeBuilder:        envoyPrime,
	}
}

// envoyPersona generates the envoy CLAUDE.local.md content.
func envoyPersona(world, agent string) ([]byte, error) {
	// Read optional persona file.
	var personaContent string
	personaPath := PersonaPath(world, agent)
	if data, err := os.ReadFile(personaPath); err == nil {
		personaContent = string(data)
	}

	ctx := protocol.EnvoyClaudeMDContext{
		AgentName:      agent,
		World:          world,
		SolBinary:      "sol",
		PersonaContent: personaContent,
	}

	// Read tethered writs for multi-writ awareness.
	if err := populateEnvoyWritContext(&ctx, world, agent); err != nil {
		// Non-fatal: log and continue with base persona.
		fmt.Fprintf(os.Stderr, "envoy persona: failed to populate writ context: %v\n", err)
	}

	content := protocol.GenerateEnvoyClaudeMD(ctx)
	return []byte(content), nil
}

// populateEnvoyWritContext reads tethered writs and active_writ from stores,
// populating the EnvoyClaudeMDContext with multi-writ fields.
func populateEnvoyWritContext(ctx *protocol.EnvoyClaudeMDContext, world, agent string) error {
	// Read all tethered writs.
	writIDs, err := tether.List(world, agent, "envoy")
	if err != nil {
		return fmt.Errorf("failed to list tethers: %w", err)
	}
	if len(writIDs) == 0 {
		return nil // no tethers — nothing to populate
	}

	// Read active_writ from sphere store.
	ss, err := store.OpenSphere()
	if err != nil {
		return fmt.Errorf("failed to open sphere store: %w", err)
	}
	defer ss.Close()

	agentID := world + "/" + agent
	agentRecord, err := ss.GetAgent(agentID)
	if err != nil {
		return fmt.Errorf("failed to get agent %q: %w", agentID, err)
	}
	activeWritID := agentRecord.ActiveWrit

	// Open world store to look up each writ.
	ws, err := store.OpenWorld(world)
	if err != nil {
		return fmt.Errorf("failed to open world store: %w", err)
	}
	defer ws.Close()

	// Build WritSummary for each tethered writ.
	for _, writID := range writIDs {
		writ, err := ws.GetWrit(writID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "envoy persona: failed to get writ %q: %v\n", writID, err)
			continue
		}
		kind := writ.Kind
		if kind == "" {
			kind = "code"
		}
		ctx.TetheredWrits = append(ctx.TetheredWrits, protocol.WritSummary{
			ID:     writID,
			Title:  writ.Title,
			Kind:   kind,
			Status: writ.Status,
		})

		// If this is the active writ, populate full context.
		if writID == activeWritID {
			ctx.ActiveWritID = writID
			ctx.ActiveTitle = writ.Title
			ctx.ActiveDesc = writ.Description
			ctx.ActiveKind = kind
			ctx.ActiveOutput = config.WritOutputDir(world, writID)

			// Resolve direct dependencies.
			depIDs, err := ws.GetDependencies(writID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "envoy persona: failed to get deps for %q: %v\n", writID, err)
			} else {
				for _, depID := range depIDs {
					depWrit, err := ws.GetWrit(depID)
					if err != nil {
						continue
					}
					depKind := depWrit.Kind
					if depKind == "" {
						depKind = "code"
					}
					ctx.ActiveDeps = append(ctx.ActiveDeps, protocol.DepOutput{
						WritID:    depID,
						Title:     depWrit.Title,
						Kind:      depKind,
						OutputDir: config.WritOutputDir(world, depID),
					})
				}
			}
		}
	}

	return nil
}

// envoyHooks returns the Claude Code hook configuration for the envoy.
func envoyHooks(world, agent string) startup.HookSet {
	return protocol.HookConfig{
		Hooks: map[string][]protocol.HookMatcherGroup{
			"SessionStart": {
				{
					Matcher: "startup|resume",
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: "sol brief inject --path=.brief/memory.md --max-lines=200",
						},
					},
				},
				{
					Matcher: "compact",
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: "sol brief inject --path=.brief/memory.md --max-lines=200",
						},
					},
				},
			},
			"PreCompact": {
				{
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol handoff --world=%s --agent=%s --reason=compact", world, agent),
						},
					},
				},
			},
			"PreToolUse": append([]protocol.HookMatcherGroup{
				{
					Matcher: "Write|Edit",
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: `FILE=$(jq -r '.tool_input.file_path // empty'); if echo "$FILE" | grep -q '.claude/projects/.*/memory/'; then echo "BLOCKED: Use .brief/memory.md, not Claude Code auto-memory." >&2; exit 2; fi`,
						},
					},
				},
				{
					Matcher: "EnterPlanMode",
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: `echo "BLOCKED: Plan mode overrides your persona and context. Outline your approach in conversation instead. Your persistent memory is at .brief/memory.md — consult it for your role constraints and accumulated knowledge." >&2; exit 2`,
						},
					},
				},
			}, protocol.GuardHooks("envoy")...),
			"UserPromptSubmit": {
				{
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol nudge drain --world=%s --agent=%s", world, agent),
						},
					},
				},
			},
		},
	}
}

// envoyPrime builds the initial prompt for the envoy session.
func envoyPrime(world, agent string) string {
	return fmt.Sprintf(
		"Envoy %s, world %s. If no context appears, run: sol brief inject --path=.brief/memory.md --max-lines=200",
		agent, world)
}
