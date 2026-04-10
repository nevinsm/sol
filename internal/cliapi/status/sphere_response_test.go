package status

import (
	"encoding/json"
	"testing"

	"github.com/nevinsm/sol/internal/broker"
	internstatus "github.com/nevinsm/sol/internal/status"
)

func TestFromSphereStatus(t *testing.T) {
	input := &internstatus.SphereStatus{
		SOLHome: "/home/user/sol",
		Prefect: internstatus.PrefectInfo{Running: true, PID: 1234},
		Consul: internstatus.ConsulInfo{
			Running:      true,
			HeartbeatAge: "5m",
			PatrolCount:  42,
			Stale:        false,
		},
		Chronicle: internstatus.ChronicleInfo{
			Running:         true,
			PID:             2345,
			EventsProcessed: 100,
			HeartbeatAge:    "2m",
		},
		Ledger: internstatus.LedgerInfo{
			Running:      true,
			PID:          3456,
			Port:         4317,
			HeartbeatAge: "1m",
		},
		Broker: internstatus.BrokerInfo{
			Running:        true,
			HeartbeatAge:   "30s",
			PatrolCount:    10,
			ProviderHealth: "healthy",
			Providers: []broker.ProviderHealthEntry{
				{Provider: "claude", Health: "healthy"},
			},
			TokenHealth: []broker.AccountTokenHealth{
				{Handle: "primary", Type: "oauth_token", Status: "ok"},
			},
		},
		Worlds: []internstatus.WorldSummary{
			{
				Name:     "sol-dev",
				Agents:   3,
				Working:  2,
				Idle:     1,
				Forge:    true,
				Sentinel: true,
				Health:   "healthy",
			},
		},
		Tokens: internstatus.TokenInfo{
			InputTokens:  50000,
			OutputTokens: 10000,
			CacheTokens:  5000,
			AgentCount:   3,
			CostUSD:      1.50,
		},
		Caravans: []internstatus.CaravanInfo{
			{
				ID:         "caravan-1",
				Name:       "cli-api-firm-up",
				Status:     "active",
				TotalItems: 10,
				ReadyItems: 3,
				DoneItems:  2,
			},
		},
		Escalations: &internstatus.EscalationSummary{
			Total:      2,
			BySeverity: map[string]int{"high": 1, "low": 1},
		},
		MailCount: 5,
		Health:    "healthy",
	}

	resp := FromSphereStatus(input)

	// Verify top-level fields.
	if resp.SOLHome != "/home/user/sol" {
		t.Errorf("SOLHome = %q, want %q", resp.SOLHome, "/home/user/sol")
	}
	if resp.Health != "healthy" {
		t.Errorf("Health = %q, want %q", resp.Health, "healthy")
	}
	if resp.MailCount != 5 {
		t.Errorf("MailCount = %d, want %d", resp.MailCount, 5)
	}

	// Verify prefect.
	if !resp.Prefect.Running || resp.Prefect.PID != 1234 {
		t.Errorf("Prefect = %+v, want Running=true PID=1234", resp.Prefect)
	}

	// Verify consul.
	if !resp.Consul.Running || resp.Consul.PatrolCount != 42 {
		t.Errorf("Consul = %+v, want Running=true PatrolCount=42", resp.Consul)
	}

	// Verify worlds.
	if len(resp.Worlds) != 1 {
		t.Fatalf("Worlds len = %d, want 1", len(resp.Worlds))
	}
	if resp.Worlds[0].Name != "sol-dev" || resp.Worlds[0].Working != 2 {
		t.Errorf("Worlds[0] = %+v, unexpected", resp.Worlds[0])
	}

	// Verify tokens.
	if resp.Tokens.InputTokens != 50000 || resp.Tokens.CostUSD != 1.50 {
		t.Errorf("Tokens = %+v, unexpected", resp.Tokens)
	}

	// Verify caravans.
	if len(resp.Caravans) != 1 || resp.Caravans[0].Name != "cli-api-firm-up" {
		t.Errorf("Caravans = %+v, unexpected", resp.Caravans)
	}

	// Verify escalations.
	if resp.Escalations == nil || resp.Escalations.Total != 2 {
		t.Errorf("Escalations = %+v, want Total=2", resp.Escalations)
	}

	// Verify broker providers converted.
	if len(resp.Broker.Providers) != 1 || resp.Broker.Providers[0].Health != "healthy" {
		t.Errorf("Broker.Providers = %+v, unexpected", resp.Broker.Providers)
	}
	if len(resp.Broker.TokenHealth) != 1 || resp.Broker.TokenHealth[0].Handle != "primary" {
		t.Errorf("Broker.TokenHealth = %+v, unexpected", resp.Broker.TokenHealth)
	}
}

func TestFromSphereStatusMinimal(t *testing.T) {
	input := &internstatus.SphereStatus{
		SOLHome: "/sol",
		Health:  "degraded",
	}

	resp := FromSphereStatus(input)

	if resp.SOLHome != "/sol" {
		t.Errorf("SOLHome = %q, want %q", resp.SOLHome, "/sol")
	}
	if resp.Health != "degraded" {
		t.Errorf("Health = %q, want %q", resp.Health, "degraded")
	}
	if resp.Worlds != nil {
		t.Errorf("Worlds = %v, want nil", resp.Worlds)
	}
	if resp.Caravans != nil {
		t.Errorf("Caravans = %v, want nil", resp.Caravans)
	}
	if resp.Escalations != nil {
		t.Errorf("Escalations = %v, want nil", resp.Escalations)
	}
}

func TestFromSphereStatusJSONShape(t *testing.T) {
	// Verify that the JSON shape of the cliapi type matches the internal type.
	input := &internstatus.SphereStatus{
		SOLHome: "/sol",
		Prefect: internstatus.PrefectInfo{Running: true, PID: 100},
		Consul:  internstatus.ConsulInfo{Running: true, Stale: false},
		Health:  "healthy",
	}

	// Marshal internal type directly.
	internalJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal internal: %v", err)
	}

	// Marshal cliapi type.
	resp := FromSphereStatus(input)
	cliJSON, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal cliapi: %v", err)
	}

	// Parse both to maps and compare keys.
	var internalMap, cliMap map[string]any
	if err := json.Unmarshal(internalJSON, &internalMap); err != nil {
		t.Fatalf("unmarshal internal: %v", err)
	}
	if err := json.Unmarshal(cliJSON, &cliMap); err != nil {
		t.Fatalf("unmarshal cliapi: %v", err)
	}

	// Verify all top-level keys match.
	for key := range internalMap {
		if _, ok := cliMap[key]; !ok {
			t.Errorf("key %q present in internal JSON but missing in cliapi JSON", key)
		}
	}
	for key := range cliMap {
		if _, ok := internalMap[key]; !ok {
			t.Errorf("key %q present in cliapi JSON but missing in internal JSON", key)
		}
	}
}
