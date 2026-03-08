package protocol

import (
	"fmt"
	"os"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// PopulateWritContext reads tethered writs and the active_writ from stores,
// returning a WritContext with multi-writ fields populated. This is the shared
// implementation used by both envoy and governor persona generation.
//
// The role parameter is the tether role (e.g., "envoy", "governor").
// If no tethers exist, an empty WritContext is returned with no error.
func PopulateWritContext(world, agent, role string) (WritContext, error) {
	var ctx WritContext

	// Read all tethered writs.
	writIDs, err := tether.List(world, agent, role)
	if err != nil {
		return ctx, fmt.Errorf("failed to list tethers: %w", err)
	}
	if len(writIDs) == 0 {
		return ctx, nil // no tethers — nothing to populate
	}

	// Read active_writ from sphere store.
	ss, err := store.OpenSphere()
	if err != nil {
		return ctx, fmt.Errorf("failed to open sphere store: %w", err)
	}
	defer ss.Close()

	agentID := world + "/" + agent
	agentRecord, err := ss.GetAgent(agentID)
	if err != nil {
		return ctx, fmt.Errorf("failed to get agent %q: %w", agentID, err)
	}
	activeWritID := agentRecord.ActiveWrit

	// Open world store to look up each writ.
	ws, err := store.OpenWorld(world)
	if err != nil {
		return ctx, fmt.Errorf("failed to open world store: %w", err)
	}
	defer ws.Close()

	// Build WritSummary for each tethered writ.
	for _, writID := range writIDs {
		writ, err := ws.GetWrit(writID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s persona: failed to get writ %q: %v\n", role, writID, err)
			continue
		}
		kind := writ.Kind
		if kind == "" {
			kind = "code"
		}
		ctx.TetheredWrits = append(ctx.TetheredWrits, WritSummary{
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
				fmt.Fprintf(os.Stderr, "%s persona: failed to get deps for %q: %v\n", role, writID, err)
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
					ctx.ActiveDeps = append(ctx.ActiveDeps, DepOutput{
						WritID:    depID,
						Title:     depWrit.Title,
						Kind:      depKind,
						OutputDir: config.WritOutputDir(world, depID),
					})
				}
			}
		}
	}

	return ctx, nil
}
