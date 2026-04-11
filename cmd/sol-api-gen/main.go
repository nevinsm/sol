// Command sol-api-gen generates JSON Schema files for sol's CLI API types.
//
// It reflects on every registered cliapi response type and writes one
// JSON Schema document per command to docs/api/<command>.schema.json.
//
// Usage:
//
//	go run ./cmd/sol-api-gen
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/invopop/jsonschema"

	// cliapi sub-packages — one import per entity that has response types.
	"github.com/nevinsm/sol/internal/cliapi/accounts"
	"github.com/nevinsm/sol/internal/cliapi/agents"
	"github.com/nevinsm/sol/internal/cliapi/broker"
	"github.com/nevinsm/sol/internal/cliapi/caravans"
	"github.com/nevinsm/sol/internal/cliapi/chronicle"
	"github.com/nevinsm/sol/internal/cliapi/consul"
	"github.com/nevinsm/sol/internal/cliapi/cost"
	"github.com/nevinsm/sol/internal/cliapi/dispatch"
	"github.com/nevinsm/sol/internal/cliapi/doctor"
	"github.com/nevinsm/sol/internal/cliapi/forge"
	"github.com/nevinsm/sol/internal/cliapi/ledger"
	"github.com/nevinsm/sol/internal/cliapi/prefect"
	"github.com/nevinsm/sol/internal/cliapi/quota"
	"github.com/nevinsm/sol/internal/cliapi/schema"
	"github.com/nevinsm/sol/internal/cliapi/sentinel"
	"github.com/nevinsm/sol/internal/cliapi/status"
	"github.com/nevinsm/sol/internal/cliapi/workflows"
	"github.com/nevinsm/sol/internal/cliapi/worlds"
	"github.com/nevinsm/sol/internal/cliapi/writs"
)

// registry maps CLI command names to their response types.
// This is intentionally explicit — we know exactly which schemas exist.
var registry = map[string]any{
	// accounts
	"account-delete": accounts.DeleteResponse{},
	"account-list":   accounts.ListEntry{},

	// agents
	"agent-delete": agents.DeleteResponse{},
	"agent-sync":   agents.SyncResponse{},

	// broker
	"broker-status": broker.StatusResponse{},

	// caravans
	"caravan-check":    caravans.CheckResponse{},
	"caravan-delete":   caravans.DeleteResponse{},
	"caravan-dep-list": caravans.DepListResponse{},
	"caravan-launch":   caravans.LaunchResponse{},

	// chronicle
	"chronicle-status": chronicle.StatusResponse{},

	// consul
	"consul-status": consul.StatusResponse{},

	// cost
	"cost-agent":   cost.AgentCostResponse{},
	"cost-caravan": cost.CaravanCostResponse{},
	"cost-world":   cost.WorldCostResponse{},
	"cost-writ":    cost.WritCostResponse{},

	// dispatch
	"cast": dispatch.CastResult{},

	// doctor
	"doctor": doctor.DoctorResponse{},

	// forge
	"forge-status": forge.ForgeStatusResponse{},
	"forge-await":  forge.ForgeAwaitResponse{},
	"forge-sync":   forge.ForgeSyncResponse{},

	// ledger
	"ledger-status": ledger.StatusResponse{},

	// prefect
	"prefect-status": prefect.StatusResponse{},

	// quota
	"quota-rotate": quota.RotateResponse{},
	"quota-status": quota.StatusResponse{},

	// schema
	"schema-migrate": schema.MigrateResponse{},

	// sentinel
	"sentinel-status": sentinel.StatusResponse{},

	// status
	"status-sphere":   status.SphereStatusResponse{},
	"status-world":    status.WorldStatusResponse{},
	"status-combined": status.CombinedStatusResponse{},

	// workflows
	"workflow-init": workflows.InitResponse{},
	"workflow-show": workflows.ShowResponse{},

	// worlds
	"world-delete": worlds.DeleteResponse{},
	"world-export": worlds.ExportResponse{},
	"world-status": worlds.StatusResponse{},
	"world-sync":   worlds.SyncResponse{},

	// writs
	"writ-clean":    writs.WritCleanResult{},
	"writ-dep-list": writs.DepListResponse{},
	"writ-trace":    writs.TraceResponse{},
}

func main() {
	outDir := filepath.Join("docs", "api")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatalf("create output dir: %v", err)
	}

	// Sort command names for deterministic processing order.
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)

	r := &jsonschema.Reflector{
		DoNotReference: true,
	}

	for _, name := range names {
		v := registry[name]
		s := r.Reflect(v)

		data, err := marshalDeterministic(s)
		if err != nil {
			log.Fatalf("marshal schema %q: %v", name, err)
		}

		outPath := filepath.Join(outDir, name+".schema.json")
		if err := os.WriteFile(outPath, data, 0o644); err != nil {
			log.Fatalf("write %q: %v", outPath, err)
		}
		fmt.Printf("wrote %s\n", outPath)
	}
}

// marshalDeterministic serializes a JSON Schema with deterministic key ordering.
// The invopop/jsonschema library uses orderedmap for properties (stable), but
// $defs uses a plain map[string]*Schema. We marshal to a generic structure
// and sort map keys to guarantee reproducible output.
func marshalDeterministic(s *jsonschema.Schema) ([]byte, error) {
	// First pass: marshal using the library's own MarshalJSON.
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}

	// Decode into a generic structure, then re-encode with sorted keys.
	// json.MarshalIndent sorts map[string]any keys alphabetically, which
	// guarantees deterministic output for the $defs map.
	var generic any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&generic); err != nil {
		return nil, err
	}

	out, err := json.MarshalIndent(generic, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}
