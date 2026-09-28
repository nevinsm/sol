package sentinel

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/store"
)

func TestProgressDetectionOutputChanged(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	mock.alive["sol-ember-Toast"] = true

	assessCalled := false
	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		assessCalled = true
		return &AssessmentResult{Status: "progressing", Confidence: "high", SuggestedAction: "none"}, nil
	}

	// First patrol: establish baseline.
	mock.captures["sol-ember-Toast"] = "output v1"
	w.patrol(context.Background())

	// Second patrol: different output — should NOT trigger assessment.
	mock.captures["sol-ember-Toast"] = "output v2"
	w.patrol(context.Background())

	if assessCalled {
		t.Error("assessment should not be triggered when output changes")
	}
}

func TestProgressDetectionOutputUnchanged(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "same output"

	assessCalled := false
	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		assessCalled = true
		return &AssessmentResult{Status: "progressing", Confidence: "high", SuggestedAction: "none"}, nil
	}

	// First patrol: establish baseline.
	w.patrol(context.Background())

	// Second patrol: same output — should trigger assessment.
	w.patrol(context.Background())

	if !assessCalled {
		t.Error("assessment should be triggered when output is unchanged")
	}
}

// TestProgressDetectionCaptureFailureSetssentinel verifies that a capture failure
// records captureErrorSentinel in lastCaptures so the next successful capture
// establishes a fresh baseline instead of comparing against a stale pre-failure hash.
func TestProgressDetectionCaptureFailureSetssentinel(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	mock.alive["sol-ember-Toast"] = true

	assessCalled := false
	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		assessCalled = true
		return &AssessmentResult{Status: "progressing", Confidence: "high", SuggestedAction: "none"}, nil
	}

	// Patrol 1: successful capture — establish baseline with "output v1".
	mock.captures["sol-ember-Toast"] = "output v1"
	w.patrol(context.Background())

	// Patrol 2: capture fails — sentinel should be recorded instead of stale hash.
	delete(mock.captures, "sol-ember-Toast")
	w.patrol(context.Background())

	// Verify sentinel was stored (not stale hash from patrol 1).
	agent, _ := sphereStore.GetAgent("ember/Toast")
	if got := w.lastCaptures[agent.ID]; got != captureErrorSentinel {
		t.Errorf("after capture failure, lastCaptures = %q; want %q", got, captureErrorSentinel)
	}

	// Patrol 3: successful capture with same output as before the failure.
	// This should NOT trigger assessment because the sentinel means we establish
	// a new baseline (sentinel → "output v1" counts as "changed").
	mock.captures["sol-ember-Toast"] = "output v1"
	w.patrol(context.Background())

	if assessCalled {
		t.Error("assessment should not be triggered on first successful capture after failure (fresh baseline)")
	}

	// Patrol 4: same output again — now stall detection should fire.
	w.patrol(context.Background())

	if !assessCalled {
		t.Error("assessment should be triggered on second consecutive unchanged capture after failure recovery")
	}
}

func TestAssessmentNudge(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "stuck output"

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "stuck",
			Confidence:      "high",
			SuggestedAction: "nudge",
			NudgeMessage:    "You appear stuck. Try checking the error log.",
		}, nil
	}

	// First patrol: baseline.
	w.patrol(context.Background())
	// Second patrol: same output → assessment → nudge.
	w.patrol(context.Background())

	// The pane only ever sees the fixed doorbell now — content goes through
	// the durable nudge queue instead of riding the pane directly.
	injected := mock.getInjected()
	if len(injected) != 1 {
		t.Fatalf("expected 1 injection (doorbell), got %d", len(injected))
	}
	if injected[0].Text != nudge.DoorbellMessage {
		t.Errorf("doorbell text = %q, want %q", injected[0].Text, nudge.DoorbellMessage)
	}

	messages, err := nudge.Drain("sol-ember-Toast")
	if err != nil {
		t.Fatalf("nudge.Drain failed: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 queued nudge message, got %d", len(messages))
	}
	if messages[0].Body != "You appear stuck. Try checking the error log." {
		t.Errorf("queued nudge body = %q, want %q", messages[0].Body, "You appear stuck. Try checking the error log.")
	}
	if messages[0].Sender != "sentinel" {
		t.Errorf("queued nudge sender = %q, want %q", messages[0].Sender, "sentinel")
	}
}

func TestAssessmentEscalate(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "error output"

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "stuck",
			Confidence:      "high",
			SuggestedAction: "escalate",
			Reason:          "auth token expired",
		}, nil
	}

	// First patrol: baseline.
	w.patrol(context.Background())
	// Second patrol: same output → assessment → escalate.
	w.patrol(context.Background())

	// No nudge should be injected.
	injected := mock.getInjected()
	if len(injected) != 0 {
		t.Errorf("expected 0 injections on escalation, got %d", len(injected))
	}

	// Check that a protocol message was sent (RECOVERY_NEEDED).
	msgs, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) == 0 {
		t.Error("expected RECOVERY_NEEDED protocol message to operator")
	}
}

func TestAssessmentNone(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "output"

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "progressing",
			Confidence:      "high",
			SuggestedAction: "none",
			Reason:          "agent is compiling",
		}, nil
	}

	// First patrol: baseline.
	w.patrol(context.Background())
	// Second patrol: same output → assessment → none.
	w.patrol(context.Background())

	// No nudge, no escalation.
	injected := mock.getInjected()
	if len(injected) != 0 {
		t.Errorf("expected 0 injections for action=none, got %d", len(injected))
	}
}

func TestAssessmentLowConfidenceIgnored(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "output"

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "stuck",
			Confidence:      "low",
			SuggestedAction: "nudge",
			NudgeMessage:    "Should not be sent",
		}, nil
	}

	// First patrol: baseline.
	w.patrol(context.Background())
	// Second patrol: same output → assessment → low confidence → no action.
	w.patrol(context.Background())

	injected := mock.getInjected()
	if len(injected) != 0 {
		t.Errorf("expected 0 injections for low confidence, got %d", len(injected))
	}
}

func TestAssessmentFailureNonBlocking(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "output"

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return nil, fmt.Errorf("AI service unavailable")
	}

	// First patrol: baseline.
	w.patrol(context.Background())
	// Second patrol: same output → assessment → failure → should not crash.
	err := w.patrol(context.Background())

	if err != nil {
		t.Errorf("patrol should succeed even when assessment fails, got error: %v", err)
	}

	// No nudge or escalation.
	injected := mock.getInjected()
	if len(injected) != 0 {
		t.Errorf("expected 0 injections on assessment failure, got %d", len(injected))
	}
}

// TestBuildAssessmentPromptIncludesWaitingOnBackgroundGuidance verifies the
// prompt sent to the assessor documents the waiting_on_background suggested
// action, the "detached" verdict field, and guidance for recognizing both a
// harness-tracked background wait and a provably detached one (nohup/disown/
// setsid, a killed monitor, or a process that no longer exists).
func TestBuildAssessmentPromptIncludesWaitingOnBackgroundGuidance(t *testing.T) {
	agent := store.Agent{Name: "Toast", ID: "ember/Toast", ActiveWrit: "sol-abc1234500000000"}
	prompt := buildAssessmentPrompt(agent, "some output", 80, 3*time.Minute)

	wantSubstrings := []string{
		`"suggested_action": "none|nudge|escalate|waiting_on_background"`,
		`"detached": false`,
		"waiting_on_background",
		"background shell",
		"Monitor",
		"nohup",
		"disown",
		"setsid",
		"detached",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(prompt, want) {
			t.Errorf("buildAssessmentPrompt() missing expected substring %q", want)
		}
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "clean JSON",
			input: `{"status":"stuck","confidence":"high","reason":"test","suggested_action":"nudge","nudge_message":"hello"}`,
			want:  "stuck",
		},
		{
			name:  "JSON with surrounding text",
			input: "Here is the analysis:\n{\"status\":\"progressing\",\"confidence\":\"medium\",\"reason\":\"compiling\",\"suggested_action\":\"none\",\"nudge_message\":\"\"}\nEnd of response.",
			want:  "progressing",
		},
		{
			name:    "no JSON",
			input:   "This is just text without any JSON",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := extractJSON([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("extractJSON() error: %v", err)
			}
			if result.Status != tt.want {
				t.Errorf("status = %q, want %q", result.Status, tt.want)
			}
		})
	}
}

func TestAssessmentEscalateCreatesEscalation(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-esc-assess1")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "error output"

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "stuck",
			Confidence:      "high",
			SuggestedAction: "escalate",
			Reason:          "auth token expired",
		}, nil
	}

	// First patrol: baseline capture.
	w.patrol(context.Background())
	// Second patrol: same output → assessment → escalate.
	w.patrol(context.Background())

	// Should have created a formal escalation in sphere.db.
	escs, err := sphereStore.ListEscalations("")
	if err != nil {
		t.Fatalf("ListEscalations() error: %v", err)
	}

	var found *store.Escalation
	for i := range escs {
		if escs[i].Source == "ember/sentinel" && escs[i].Severity == "high" &&
			strings.Contains(escs[i].Description, "Toast") &&
			strings.Contains(escs[i].Description, "auth token expired") {
			found = &escs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected a high-severity escalation from ember/sentinel mentioning agent Toast")
	}
	if found.SourceRef != "writ:sol-esc-assess1" {
		t.Errorf("escalation source_ref = %q, want %q", found.SourceRef, "writ:sol-esc-assess1")
	}
	if found.Status != "open" {
		t.Errorf("escalation status = %q, want %q", found.Status, "open")
	}

	// Protocol message should still be sent alongside.
	msgs, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) == 0 {
		t.Error("expected RECOVERY_NEEDED protocol message alongside escalation")
	}
}

// TestAssessmentWaitingOnBackgroundSuppressesMail verifies that a
// waiting_on_background verdict (not detached) sends no autarch mail and
// creates no escalation — the agent is correctly parked on a harness-
// tracked background wait.
func TestAssessmentWaitingOnBackgroundSuppressesMail(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.WaitGraceCount = 3

	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-wait0000000001")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "running background make test..."

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "waiting",
			Confidence:      "high",
			SuggestedAction: "waiting_on_background",
			Reason:          "harness-tracked background test run in progress",
		}, nil
	}

	// First patrol: baseline.
	w.patrol(context.Background())
	// Second patrol: same output → assessment → waiting_on_background (streak 1 of 3).
	w.patrol(context.Background())

	injected := mock.getInjected()
	if len(injected) != 0 {
		t.Errorf("expected 0 nudges for waiting_on_background, got %d", len(injected))
	}

	msgs, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 RECOVERY_NEEDED mail before grace expires, got %d", len(msgs))
	}

	escs, err := sphereStore.ListEscalations("")
	if err != nil {
		t.Fatalf("ListEscalations() error: %v", err)
	}
	if len(escs) != 0 {
		t.Errorf("expected 0 escalations before grace expires, got %d", len(escs))
	}
}

// TestAssessmentWaitingOnBackgroundGraceExpiresEscalates verifies that N
// consecutive waiting_on_background patrols with unchanged output escalate
// to RECOVERY_NEEDED once the WaitGraceCount grace period is exhausted.
func TestAssessmentWaitingOnBackgroundGraceExpiresEscalates(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.WaitGraceCount = 3

	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-wait0000000002")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "running background make test..."

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "waiting",
			Confidence:      "high",
			SuggestedAction: "waiting_on_background",
			Reason:          "harness-tracked background test run in progress",
		}, nil
	}

	// Patrol 1: baseline.
	w.patrol(context.Background())
	// Patrols 2-3: waiting streak 1, 2 — grace not yet expired (< 3).
	w.patrol(context.Background())
	w.patrol(context.Background())

	if msgs, _ := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED"); len(msgs) != 0 {
		t.Fatalf("expected 0 RECOVERY_NEEDED mail before grace expires, got %d", len(msgs))
	}

	// Patrol 4: waiting streak 3 — grace expires, should escalate.
	w.patrol(context.Background())

	msgs, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected RECOVERY_NEEDED mail once grace expires")
	}

	escs, err := sphereStore.ListEscalations("")
	if err != nil {
		t.Fatalf("ListEscalations() error: %v", err)
	}
	var found *store.Escalation
	for i := range escs {
		if strings.Contains(escs[i].Description, "grace expired") {
			found = &escs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected an escalation mentioning grace expired")
	}
}

// TestAssessmentDetachedWaitEscalatesImmediately verifies that a
// waiting_on_background verdict marked detached bypasses the grace period
// entirely and escalates on the very first assessment.
func TestAssessmentDetachedWaitEscalatesImmediately(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.WaitGraceCount = 3

	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-wait0000000003")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "nohup make test > out.log 2>&1 & disown"

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "waiting",
			Confidence:      "high",
			SuggestedAction: "waiting_on_background",
			Reason:          "process detached via nohup/disown, no completion signal possible",
			Detached:        true,
		}, nil
	}

	// Patrol 1: baseline.
	w.patrol(context.Background())
	// Patrol 2: same output → assessment → detached → escalate immediately
	// despite WaitGraceCount=3 not being reached.
	w.patrol(context.Background())

	msgs, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected immediate RECOVERY_NEEDED mail for detached wait")
	}

	escs, err := sphereStore.ListEscalations("")
	if err != nil {
		t.Fatalf("ListEscalations() error: %v", err)
	}
	var found *store.Escalation
	for i := range escs {
		if strings.Contains(escs[i].Description, "detached") {
			found = &escs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected an escalation mentioning the detached wait")
	}
}

func TestAssessmentEscalateNoWritStillCreatesEscalation(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Agent with no active writ.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "stuck output"

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "stuck",
			Confidence:      "high",
			SuggestedAction: "escalate",
			Reason:          "infrastructure problem",
		}, nil
	}

	w.patrol(context.Background())
	w.patrol(context.Background())

	escs, err := sphereStore.ListEscalations("")
	if err != nil {
		t.Fatalf("ListEscalations() error: %v", err)
	}

	var found *store.Escalation
	for i := range escs {
		if escs[i].Source == "ember/sentinel" && strings.Contains(escs[i].Description, "infrastructure problem") {
			found = &escs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected escalation even with no active writ")
	}
	// source_ref should be empty when agent has no active writ.
	if found.SourceRef != "" {
		t.Errorf("escalation source_ref = %q, want empty (no active writ)", found.SourceRef)
	}
}

// TestAssessmentEscalateDedupsMail verifies that repeated "escalate" verdicts
// for the same agent/writ (e.g. because output stays unchanged patrol after
// patrol) produce exactly one durable escalation AND exactly one
// RECOVERY_NEEDED mail — not one mail per patrol. This is the regression
// covered by sol-d51b8700819ae313: escalateAgent used to dedup the
// escalation but send the mail unconditionally on every call.
func TestAssessmentEscalateDedupsMail(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-esc-dedup0001")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "error output"

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "stuck",
			Confidence:      "high",
			SuggestedAction: "escalate",
			Reason:          "auth token expired",
		}, nil
	}

	// Patrol 1: baseline. Patrols 2 and 3: unchanged output → assessment →
	// escalate, twice in a row.
	w.patrol(context.Background())
	w.patrol(context.Background())
	w.patrol(context.Background())

	escs, err := sphereStore.ListEscalations("")
	if err != nil {
		t.Fatalf("ListEscalations() error: %v", err)
	}
	if len(escs) != 1 {
		t.Errorf("expected exactly 1 escalation after 2 consecutive escalate verdicts, got %d", len(escs))
	}

	msgs, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected exactly 1 RECOVERY_NEEDED mail after 2 consecutive escalate verdicts, got %d", len(msgs))
	}
}

// TestAssessmentWaitingOnBackgroundEscalatesOncePerStall verifies that a
// waiting_on_background streak that runs well past grace expiry (2x
// WaitGraceCount patrols with unchanged output) produces exactly one
// escalation and exactly one RECOVERY_NEEDED mail — not one every
// WaitGraceCount patrols. This is the regression covered by
// sol-d51b8700819ae313: the grace path used to delete(waitingCounts) after
// escalating, which rebuilt the streak and re-escalated every WaitGraceCount
// patrols for as long as the agent stayed parked.
func TestAssessmentWaitingOnBackgroundEscalatesOncePerStall(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.WaitGraceCount = 3

	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-wait0000000004")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "running background make test..."

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "waiting",
			Confidence:      "high",
			SuggestedAction: "waiting_on_background",
			Reason:          "harness-tracked background test run in progress",
		}, nil
	}

	// Patrol 1: baseline. Patrols 2..(1 + 2*WaitGraceCount): unchanged output,
	// waiting_on_background every time — a streak of 2x grace.
	w.patrol(context.Background())
	for i := 0; i < 2*cfg.WaitGraceCount; i++ {
		w.patrol(context.Background())
	}

	escs, err := sphereStore.ListEscalations("")
	if err != nil {
		t.Fatalf("ListEscalations() error: %v", err)
	}
	if len(escs) != 1 {
		t.Errorf("expected exactly 1 escalation for a streak of 2x grace, got %d", len(escs))
	}

	msgs, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected exactly 1 RECOVERY_NEEDED mail for a streak of 2x grace, got %d", len(msgs))
	}
}

// TestAssessmentWaitingOnBackgroundNewStreakMailsAgain verifies that once an
// agent's output changes (breaking the waiting_on_background streak and
// clearing the escalation marker), a fresh stall that exhausts grace again
// sends a second RECOVERY_NEEDED mail. Output change happens to resolve any
// existing escalation is out of scope here — this test only checks that the
// per-stall gate doesn't wrongly suppress a genuinely new stall.
func TestAssessmentWaitingOnBackgroundNewStreakMailsAgain(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.WaitGraceCount = 3

	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-wait0000000005")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "running background make test v1..."

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "waiting",
			Confidence:      "high",
			SuggestedAction: "waiting_on_background",
			Reason:          "harness-tracked background test run in progress",
		}, nil
	}

	// Patrol 1: baseline. Patrols 2-4: waiting streak reaches grace (3) and
	// escalates once.
	w.patrol(context.Background())
	w.patrol(context.Background())
	w.patrol(context.Background())
	w.patrol(context.Background())

	msgs, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 RECOVERY_NEEDED mail after first streak's grace expires, got %d", len(msgs))
	}

	// The escalation created above is still open (nothing resolved it), so
	// output changing and exhausting grace again exercises the "already
	// escalated for this writ" dedup inside escalateAgent, not the
	// per-streak waitEscalated marker. Resolve it so the new streak's
	// escalation attempt actually creates a fresh escalation and mail,
	// isolating the behavior this test targets (waitEscalated clearing on
	// output change lets the grace path try to escalate again).
	escs, err := sphereStore.ListEscalations("")
	if err != nil {
		t.Fatalf("ListEscalations() error: %v", err)
	}
	for _, e := range escs {
		if err := sphereStore.ResolveEscalation(e.ID); err != nil {
			t.Fatalf("ResolveEscalation() error: %v", err)
		}
	}

	// Output changes — breaks the streak, clears waitingCounts and
	// waitEscalated. This patrol only establishes the new baseline hash (no
	// assessment yet, mirroring the baseline patrol at the top of the test).
	mock.captures["sol-ember-Toast"] = "running background make test v2..."
	w.patrol(context.Background())

	// New streak: 3 more patrols with output unchanged (still "v2") to
	// exhaust grace again.
	w.patrol(context.Background())
	w.patrol(context.Background())
	w.patrol(context.Background())

	msgs, err = sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 RECOVERY_NEEDED mails (one per stall) after a new streak, got %d", len(msgs))
	}
}

// TestAssessmentNudgeDedupsMail verifies that repeated "nudge" verdicts for
// the same agent with unchanged output produce exactly one informational
// RECOVERY_NEEDED mail per streak, even though the nudge itself is enqueued
// on every patrol.
func TestAssessmentNudgeDedupsMail(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-nudge000000001")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "stuck output"

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "stuck",
			Confidence:      "high",
			SuggestedAction: "nudge",
			NudgeMessage:    "You appear stuck. Try checking the error log.",
		}, nil
	}

	// Patrol 1: baseline. Patrols 2-4: unchanged output → nudge, three times.
	w.patrol(context.Background())
	w.patrol(context.Background())
	w.patrol(context.Background())
	w.patrol(context.Background())

	// The nudge itself fires every patrol (queued content, not gated).
	messages, err := nudge.Drain("sol-ember-Toast")
	if err != nil {
		t.Fatalf("nudge.Drain failed: %v", err)
	}
	if len(messages) != 3 {
		t.Errorf("expected 3 queued nudge messages (one per unchanged-output patrol), got %d", len(messages))
	}

	// But the informational mail should be deduped to once per streak.
	msgs, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected exactly 1 RECOVERY_NEEDED mail for a nudge streak, got %d", len(msgs))
	}
}
