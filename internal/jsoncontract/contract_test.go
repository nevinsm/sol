package jsoncontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"

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

// ---------------------------------------------------------------------------
// Test constants and helpers
// ---------------------------------------------------------------------------

const contractWorld = "ctest"

// runCommandRaw runs the sol binary and returns stdout without fataling on error.
// Useful for commands that exit non-zero but still produce valid JSON (e.g. daemon
// status commands returning {"status":"stopped"} with exit 1).
func runCommandRaw(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	bin := solBin(t)
	cmd := exec.Command(bin, args...)
	cmd.Dir = os.TempDir()
	cmd.Env = append(os.Environ(), "SOL_HOME="+os.Getenv("SOL_HOME"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		t.Logf("runCommandRaw(%v) stderr: %s", args, stderr.String())
	}
	return stdout.Bytes(), err
}

// runCommandJSONRaw runs the sol binary with --json and returns parsed output,
// tolerating non-zero exit codes as long as stdout contains valid JSON.
func runCommandJSONRaw(t *testing.T, args ...string) []byte {
	t.Helper()
	args = append(args, "--json")
	raw, err := runCommandRaw(t, args...)
	if err != nil && len(raw) == 0 {
		t.Fatalf("runCommandJSONRaw(%v) failed with no output: %v", args, err)
	}
	// Verify it's valid JSON.
	if !json.Valid(raw) {
		t.Fatalf("runCommandJSONRaw(%v) returned invalid JSON: %s", args, raw)
	}
	return raw
}

// writeConsulHeartbeat creates a minimal consul heartbeat file so that
// `consul status --json` outputs structured JSON instead of plain text.
func writeConsulHeartbeat(t *testing.T) {
	t.Helper()
	solHome := os.Getenv("SOL_HOME")
	hbDir := filepath.Join(solHome, "consul")
	if err := os.MkdirAll(hbDir, 0o755); err != nil {
		t.Fatalf("create consul dir: %v", err)
	}
	hb := fmt.Sprintf(`{"status":"running","timestamp":"%s","patrol_count":1,"stale_tethers":0,"caravan_feeds":0,"escalations":0}`,
		time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(hbDir, "heartbeat.json"), []byte(hb), 0o644); err != nil {
		t.Fatalf("write consul heartbeat: %v", err)
	}
}

// writeBrokerHeartbeat creates a minimal broker heartbeat file so that
// `broker status --json` outputs structured JSON instead of plain text.
func writeBrokerHeartbeat(t *testing.T) {
	t.Helper()
	solHome := os.Getenv("SOL_HOME")
	hbDir := filepath.Join(solHome, ".runtime")
	if err := os.MkdirAll(hbDir, 0o755); err != nil {
		t.Fatalf("create .runtime: %v", err)
	}
	hb := fmt.Sprintf(`{"status":"running","timestamp":"%s","patrol_count":1,"provider_health":"unknown","consecutive_failures":0}`,
		time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(hbDir, "broker-heartbeat.json"), []byte(hb), 0o644); err != nil {
		t.Fatalf("write broker heartbeat: %v", err)
	}
}

// setupContractWorld creates an isolated env and initializes a world.
func setupContractWorld(t *testing.T) Env {
	t.Helper()
	env := SetupEnv(t)
	// Clear SOL_WORLD to prevent parent env from leaking.
	t.Setenv("SOL_WORLD", "")
	RunCommand(t, "world", "init", contractWorld, "--source-repo="+env.SourceRepo)
	return env
}

// createTestWrit creates a writ and returns its ID.
func createTestWrit(t *testing.T) string {
	t.Helper()
	raw := RunCommand(t, "writ", "create", "--world="+contractWorld, "--title=contract-test-writ", "--json")
	var w struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatalf("parse writ create: %v\nraw: %s", err, raw)
	}
	if w.ID == "" {
		t.Fatalf("writ create returned empty ID\nraw: %s", raw)
	}
	return w.ID
}

// createTestEnvoy creates an envoy agent in the contract world.
func createTestEnvoy(t *testing.T, name string) {
	t.Helper()
	RunCommand(t, "envoy", "create", name, "--world="+contractWorld)
}

// createTestCaravan creates a caravan and returns its ID.
func createTestCaravan(t *testing.T, name string) string {
	t.Helper()
	raw := RunCommand(t, "caravan", "create", name, "--json")
	var c struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("parse caravan create: %v\nraw: %s", err, raw)
	}
	return c.ID
}

// createTestAccount creates a named account.
func createTestAccount(t *testing.T, handle string) {
	t.Helper()
	RunCommand(t, "account", "add", handle)
}

// assertIDFormat checks that an ID matches the expected prefix + hex pattern.
func assertIDFormat(t *testing.T, id, prefix string) {
	t.Helper()
	pattern := regexp.MustCompile(`^` + prefix + `[0-9a-f]{16}$`)
	if !pattern.MatchString(id) {
		t.Errorf("ID %q does not match expected format %s<16-hex>", id, prefix)
	}
}

// assertTimeRFC3339 checks that a time.Time is not zero (i.e. it was parsed).
func assertTimeRFC3339(t *testing.T, name string, ts time.Time) {
	t.Helper()
	if ts.IsZero() {
		t.Errorf("time field %q is zero (not parsed or missing)", name)
	}
}

// assertEnum checks that a value is in the allowed set.
func assertEnum(t *testing.T, name, value string, allowed []string) {
	t.Helper()
	for _, a := range allowed {
		if value == a {
			return
		}
	}
	t.Errorf("enum field %q = %q; not in allowed set %v", name, value, allowed)
}

// ---------------------------------------------------------------------------
// Contract tests — one per W2.2 registry entry
// ---------------------------------------------------------------------------

// --- accounts ---

func TestContract_AccountDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	SetupEnv(t)
	createTestAccount(t, "del-test")
	raw := RunCommand(t, "account", "delete", "del-test", "--confirm", "--json")
	var resp accounts.DeleteResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "handle", "deleted")
	if resp.Handle != "del-test" {
		t.Errorf("expected handle=del-test, got %s", resp.Handle)
	}
	if !resp.Deleted {
		t.Error("expected deleted=true")
	}
}

func TestContract_AccountList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	SetupEnv(t)
	createTestAccount(t, "list-test")

	raw := RunCommand(t, "account", "list", "--json")
	// account list returns a JSON array of ListEntry.
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("unmarshal array: %v\nraw: %s", err, raw)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one account entry")
	}
	var entry accounts.ListEntry
	AssertJSONShape(t, entries[0], &entry)
	if entry.Handle == "" {
		t.Error("expected non-empty handle")
	}
}

// --- agents ---

func TestContract_AgentDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	createTestEnvoy(t, "del-envoy")
	raw := RunCommand(t, "envoy", "delete", "del-envoy", "--world="+contractWorld, "--confirm", "--json")
	var resp agents.DeleteResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "name", "world", "deleted")
	if !resp.Deleted {
		t.Error("expected deleted=true")
	}
}

func TestContract_AgentSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	createTestEnvoy(t, "sync-envoy")
	raw := RunCommand(t, "envoy", "sync", "sync-envoy", "--world="+contractWorld, "--json")
	var resp agents.SyncResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "name", "world", "synced")
}

// --- broker ---

func TestContract_BrokerStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	SetupEnv(t)
	// Broker only outputs JSON when a heartbeat exists; create one.
	writeBrokerHeartbeat(t)
	raw := RunCommand(t, "broker", "status", "--json")
	var resp broker.StatusResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "status", "checked_at")
}

// --- caravans ---

func TestContract_CaravanCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	writID := createTestWrit(t)
	caravanID := createTestCaravan(t, "check-caravan")
	RunCommand(t, "caravan", "add", caravanID, writID, "--world="+contractWorld)
	// Commission the caravan so items become checkable.
	RunCommand(t, "caravan", "commission", caravanID)

	raw := RunCommand(t, "caravan", "check", caravanID, "--json")
	var resp caravans.CheckResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "id", "name", "status", "items")
	assertIDFormat(t, resp.ID, "car-")
}

func TestContract_CaravanDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	// Newly created caravan is in drydock → deletable.
	caravanID := createTestCaravan(t, "del-caravan")
	raw := RunCommand(t, "caravan", "delete", caravanID, "--confirm", "--json")
	var resp caravans.DeleteResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "id", "deleted")
	if !resp.Deleted {
		t.Error("expected deleted=true")
	}
}

func TestContract_CaravanDepList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	caravanID := createTestCaravan(t, "dep-caravan")
	raw := RunCommand(t, "caravan", "dep", "list", caravanID, "--json")
	var resp caravans.DepListResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "id", "name", "depends_on", "depended_by")
}

func TestContract_CaravanLaunch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	writID := createTestWrit(t)
	caravanID := createTestCaravan(t, "launch-caravan")
	RunCommand(t, "caravan", "add", caravanID, writID, "--world="+contractWorld)
	RunCommand(t, "caravan", "commission", caravanID)
	createTestEnvoy(t, "launch-envoy")

	raw := RunCommand(t, "caravan", "launch", caravanID, "--world="+contractWorld, "--json")
	var resp caravans.LaunchResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "caravan_id", "world", "dispatched", "blocked")
	assertIDFormat(t, resp.CaravanID, "car-")
}

// --- chronicle ---

func TestContract_ChronicleStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	SetupEnv(t)
	// Chronicle exits non-zero when stopped but still outputs valid JSON.
	raw := runCommandJSONRaw(t, "chronicle", "status")
	var resp chronicle.StatusResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "status")
	assertEnum(t, "status", resp.Status, []string{"running", "stopped", "stale"})
}

// --- consul ---

func TestContract_ConsulStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	SetupEnv(t)
	// Consul only outputs JSON when a heartbeat exists; create one.
	writeConsulHeartbeat(t)
	raw := runCommandJSONRaw(t, "consul", "status")
	var resp consul.StatusResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "status", "checked_at")
	assertEnum(t, "status", resp.Status, []string{"running", "stopped", "stale"})
}

// --- cost ---

func TestContract_CostAgent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	createTestEnvoy(t, "cost-envoy")
	raw := RunCommand(t, "cost", "--agent=cost-envoy", "--world="+contractWorld, "--json")
	var resp cost.AgentCostResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "world", "agent", "period")
}

func TestContract_CostCaravan(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	caravanID := createTestCaravan(t, "cost-caravan")
	raw := RunCommand(t, "cost", "--caravan="+caravanID, "--json")
	var resp cost.CaravanCostResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "caravan_id", "caravan_name", "period")
}

func TestContract_CostWorld(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	raw := RunCommand(t, "cost", "--world="+contractWorld, "--json")
	var resp cost.WorldCostResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "world", "period")
}

func TestContract_CostWrit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	writID := createTestWrit(t)
	raw := RunCommand(t, "cost", "--writ="+writID, "--world="+contractWorld, "--json")
	var resp cost.WritCostResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "writ_id", "period")
}

// --- dispatch ---

func TestContract_Cast(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	// Cast requires an outpost agent (not envoy).
	RunCommand(t, "agent", "create", "cast-agent", "--world="+contractWorld)
	writID := createTestWrit(t)

	raw := RunCommand(t, "cast", writID, "--world="+contractWorld, "--agent=cast-agent", "--json")
	var resp dispatch.CastResult
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "writ_id", "agent_name", "worktree_path", "session_name", "branch")
	assertIDFormat(t, resp.WritID, "sol-")
	if resp.SessionName == "" {
		t.Error("expected non-empty session_name")
	}
}

// --- doctor ---

func TestContract_Doctor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	SetupEnv(t)
	raw := RunCommand(t, "doctor", "--json")
	var resp doctor.DoctorResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "checks")
	if len(resp.Checks) == 0 {
		t.Error("expected at least one check")
	}
	RequireFields(t, raw, "checks.0.name", "checks.0.message")
}

// --- forge ---

func TestContract_ForgeStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	// Forge status exits 1 when forge isn't running but still outputs valid JSON.
	raw := runCommandJSONRaw(t, "forge", "status", "--world="+contractWorld)
	var resp forge.ForgeStatusResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "world", "running")
	if resp.World != contractWorld {
		t.Errorf("expected world=%s, got %s", contractWorld, resp.World)
	}
}

func TestContract_ForgeAwait(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	// forge await is JSON-only (no --json flag needed); use short timeout.
	raw := RunCommand(t, "forge", "await", "--world="+contractWorld, "--timeout=1")
	var resp forge.ForgeAwaitResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "woke", "messages", "waited_seconds")
}

func TestContract_ForgeSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	raw := RunCommand(t, "forge", "sync", "--world="+contractWorld, "--json")
	var resp forge.ForgeSyncResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "world", "fetched", "head_commit")
	if resp.World != contractWorld {
		t.Errorf("expected world=%s, got %s", contractWorld, resp.World)
	}
}

// --- ledger ---

func TestContract_LedgerStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	SetupEnv(t)
	// Ledger exits non-zero when stopped but still outputs valid JSON.
	raw := runCommandJSONRaw(t, "ledger", "status")
	var resp ledger.StatusResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "status")
	assertEnum(t, "status", resp.Status, []string{"running", "stopped", "stale"})
}

// --- prefect ---

func TestContract_PrefectStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	SetupEnv(t)
	// Prefect exits non-zero when stopped but still outputs valid JSON.
	raw := runCommandJSONRaw(t, "prefect", "status")
	var resp prefect.StatusResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "status")
	assertEnum(t, "status", resp.Status, []string{"running", "stopped", "stale"})
}

// --- quota ---

func TestContract_QuotaRotate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	// quota rotate resolves a world; set SOL_WORLD explicitly.
	t.Setenv("SOL_WORLD", contractWorld)
	// Use --confirm to get a successful JSON response.
	raw := RunCommand(t, "quota", "rotate", "--confirm", "--json")
	var resp quota.RotateResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "actions", "expired", "dry_run")
}

func TestContract_QuotaStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	SetupEnv(t)
	raw := RunCommand(t, "quota", "status", "--json")
	var resp quota.StatusResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "accounts")
}

// --- schema ---

func TestContract_SchemaMigrate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	// Use --confirm to execute (all migrations are already applied in a fresh env).
	raw := RunCommand(t, "schema", "migrate", "--confirm", "--json")
	var resp schema.MigrateResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "applied_migrations")
}

// --- sentinel ---

func TestContract_SentinelStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	// Sentinel exits non-zero when not running but still outputs valid JSON.
	raw := runCommandJSONRaw(t, "sentinel", "status", "--world="+contractWorld)
	var resp sentinel.StatusResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "world", "running")
}

// --- status ---

func TestContract_StatusSphere(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	raw := RunCommand(t, "status", "--json")
	var resp status.SphereStatusResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "sol_home", "prefect", "consul", "chronicle", "ledger", "broker", "worlds", "tokens", "health")
	assertEnum(t, "health", resp.Health, []string{"healthy", "degraded", "critical"})
}

func TestContract_StatusWorld(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	// Status exits 2 (degraded) when daemons aren't running, but still outputs valid JSON.
	raw := runCommandJSONRaw(t, "status", contractWorld)
	var resp status.WorldStatusResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "world", "prefect", "forge", "sentinel", "merge_queue", "tokens", "summary")
	if resp.World != contractWorld {
		t.Errorf("expected world=%s, got %s", contractWorld, resp.World)
	}
}

func TestContract_StatusCombined(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	// Set SOL_WORLD to trigger combined status (auto-detects world → combined response).
	t.Setenv("SOL_WORLD", contractWorld)
	// Exits 2 (degraded) when daemons aren't running, but still outputs valid JSON.
	raw := runCommandJSONRaw(t, "status")
	var resp status.CombinedStatusResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "world", "consul")
}

// --- workflows ---

func TestContract_WorkflowInit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	raw := RunCommand(t, "workflow", "init", "test-wf", "--world="+contractWorld, "--json")
	var resp workflows.InitResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "name", "scope", "path")
	if resp.Name != "test-wf" {
		t.Errorf("expected name=test-wf, got %s", resp.Name)
	}
}

func TestContract_WorkflowShow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	// Init a workflow first, then show it.
	RunCommand(t, "workflow", "init", "show-wf", "--world="+contractWorld)
	raw := RunCommand(t, "workflow", "show", "show-wf", "--world="+contractWorld, "--json")
	var resp workflows.ShowResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "name", "type", "path", "valid", "tier")
}

// --- worlds ---

func TestContract_WorldDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	env := SetupEnv(t)
	// Create a throwaway world to delete.
	RunCommand(t, "world", "init", "delworld", "--source-repo="+env.SourceRepo)
	raw := RunCommand(t, "world", "delete", "delworld", "--confirm", "--json")
	var resp worlds.DeleteResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "name", "deleted")
	if !resp.Deleted {
		t.Error("expected deleted=true")
	}
}

func TestContract_WorldExport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	outDir := t.TempDir()
	raw := RunCommand(t, "world", "export", contractWorld, "--output="+outDir+"/export.tar.gz", "--json")
	var resp worlds.ExportResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "archive_path", "world", "size_bytes")
	if resp.World != contractWorld {
		t.Errorf("expected world=%s, got %s", contractWorld, resp.World)
	}
}

func TestContract_WorldStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	raw := RunCommand(t, "world", "status", contractWorld, "--json")
	var resp worlds.StatusResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "world", "config")
}

func TestContract_WorldSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	raw := RunCommand(t, "world", "sync", contractWorld, "--json")
	var resp worlds.SyncResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "name", "fetched", "head_commit")
	if resp.Name != contractWorld {
		t.Errorf("expected name=%s, got %s", contractWorld, resp.Name)
	}
}

// --- writs ---

func TestContract_WritClean(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	raw := RunCommand(t, "writ", "clean", "--world="+contractWorld, "--confirm", "--json")
	var resp writs.WritCleanResult
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "writs_cleaned", "dirs_removed", "bytes_freed", "retention_days")
}

func TestContract_WritDepList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	writID := createTestWrit(t)
	raw := RunCommand(t, "writ", "dep", "list", writID, "--world="+contractWorld, "--json")
	var resp writs.DepListResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "writ_id", "depends_on", "depended_by")
	assertIDFormat(t, resp.WritID, "sol-")
}

func TestContract_WritTrace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test")
	}
	setupContractWorld(t)
	writID := createTestWrit(t)
	raw := RunCommand(t, "writ", "trace", writID, "--world="+contractWorld, "--json")
	var resp writs.TraceResponse
	AssertJSONShape(t, raw, &resp)
	RequireFields(t, raw, "world", "writ")
	if resp.World != contractWorld {
		t.Errorf("expected world=%s, got %s", contractWorld, resp.World)
	}
	if resp.Writ != nil {
		assertIDFormat(t, resp.Writ.ID, "sol-")
		assertTimeRFC3339(t, "writ.created_at", resp.Writ.CreatedAt)
		assertEnum(t, "writ.status", resp.Writ.Status, []string{"open", "tethered", "closed"})
		assertEnum(t, "writ.kind", resp.Writ.Kind, []string{"code", "research", "review", "ops", "debug"})
	}
}

// Compile-time assertions to ensure all registry types are referenced.
var _ = []any{
	accounts.DeleteResponse{},
	accounts.ListEntry{},
	agents.DeleteResponse{},
	agents.SyncResponse{},
	broker.StatusResponse{},
	caravans.CheckResponse{},
	caravans.DeleteResponse{},
	caravans.DepListResponse{},
	caravans.LaunchResponse{},
	chronicle.StatusResponse{},
	consul.StatusResponse{},
	cost.AgentCostResponse{},
	cost.CaravanCostResponse{},
	cost.WorldCostResponse{},
	cost.WritCostResponse{},
	dispatch.CastResult{},
	doctor.DoctorResponse{},
	forge.ForgeStatusResponse{},
	forge.ForgeAwaitResponse{},
	forge.ForgeSyncResponse{},
	ledger.StatusResponse{},
	prefect.StatusResponse{},
	quota.RotateResponse{},
	quota.StatusResponse{},
	schema.MigrateResponse{},
	sentinel.StatusResponse{},
	status.SphereStatusResponse{},
	status.WorldStatusResponse{},
	status.CombinedStatusResponse{},
	workflows.InitResponse{},
	workflows.ShowResponse{},
	worlds.DeleteResponse{},
	worlds.ExportResponse{},
	worlds.StatusResponse{},
	worlds.SyncResponse{},
	writs.WritCleanResult{},
	writs.DepListResponse{},
	writs.TraceResponse{},
}
