package maildeliver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// setupMaildeliverTestEnv creates an isolated SOL_HOME with a sphere.db,
// mirroring cmd/mail_test.go's setupMailTestEnv — these gating tests never
// touch tmux/session state (envoyWakeEligible's priority gate short-circuits
// before any lookup, and the store-only tests never call Deliver's session
// branch), so no tmux isolation is required here.
func setupMaildeliverTestEnv(t *testing.T) *store.SphereStore {
	t.Helper()
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// --- TruncatePreview ---

func TestTruncatePreviewMultiByte(t *testing.T) {
	// Every rune below is 4 bytes, so a byte budget of previewMaxBytes
	// (500) lands well inside a rune when cut naively.
	body := strings.Repeat("🚀", 200) // 800 bytes, well over the 500 budget
	got := TruncatePreview(body)

	if !utf8.ValidString(got) {
		t.Fatalf("TruncatePreview produced invalid UTF-8: %q", got)
	}
	if len(got) > previewMaxBytes {
		t.Fatalf("TruncatePreview exceeded budget: len=%d, want <= %d", len(got), previewMaxBytes)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncated preview should end with ellipsis, got %q", got)
	}
}

func TestTruncatePreviewASCIIUnchanged(t *testing.T) {
	short := "just a short mail body"
	if got := TruncatePreview(short); got != short {
		t.Errorf("TruncatePreview(%q) = %q, want unchanged", short, got)
	}

	long := strings.Repeat("a", 600)
	got := TruncatePreview(long)
	want := strings.Repeat("a", 497) + "..."
	if got != want {
		t.Errorf("TruncatePreview long ASCII body: len=%d, want len=%d", len(got), len(want))
	}
	if len(got) != previewMaxBytes {
		t.Errorf("TruncatePreview long ASCII body length = %d, want %d", len(got), previewMaxBytes)
	}
}

// --- Deliver: malformed recipient ---

// TestDeliverMalformedRecipientReturnsError verifies that Deliver does not
// panic on a malformed (non-canonical) recipient and instead returns a
// descriptive error for the caller to log.
func TestDeliverMalformedRecipientReturnsError(t *testing.T) {
	cases := []string{"foo", "", "/agent", "world/"}
	for _, to := range cases {
		t.Run(to, func(t *testing.T) {
			err := Deliver(Opts{Recipient: to, Subject: "subj", Body: "body", Priority: 2})
			if err == nil {
				t.Fatalf("expected error for malformed recipient %q, got nil", to)
			}
			if !strings.Contains(err.Error(), "non-canonical recipient") {
				t.Errorf("expected non-canonical recipient error for %q, got: %v", to, err)
			}
		})
	}
}

// TestDeliverSuppressIsNoOp verifies Suppress short-circuits before any
// recipient validation — even a malformed recipient produces no error.
func TestDeliverSuppressIsNoOp(t *testing.T) {
	if err := Deliver(Opts{Recipient: "not-canonical", Suppress: true}); err != nil {
		t.Errorf("expected Suppress to no-op regardless of recipient shape, got: %v", err)
	}
}

// TestDeliverAutarchRecipientIsNoOp verifies the autarch-recipient
// exemption: no error, and (implicitly, since it returns before any
// session/store lookup) no attempt to nudge or wake.
func TestDeliverAutarchRecipientIsNoOp(t *testing.T) {
	if err := Deliver(Opts{Recipient: config.Autarch, Subject: "s", Body: "b", Priority: 2}); err != nil {
		t.Errorf("expected autarch recipient to no-op, got: %v", err)
	}
}

// --- envoyWakeEligible (wake-on-mail gating) ---

// TestEnvoyWakeEligiblePriorityGate verifies priority 3 (low) is rejected
// before any sphere store lookup happens — no SOL_HOME/.store is set up
// here, so a store open attempt would fail loudly if the priority gate
// didn't short-circuit first.
func TestEnvoyWakeEligiblePriorityGate(t *testing.T) {
	eligible, err := envoyWakeEligible("world", "agent", 3)
	if eligible {
		t.Error("expected priority 3 (low) to never be wake-eligible")
	}
	if err != nil {
		t.Errorf("expected no error from the priority short-circuit, got: %v", err)
	}
}

func TestEnvoyWakeEligibleEnvoyRolePriority1And2(t *testing.T) {
	s := setupMaildeliverTestEnv(t)
	if _, err := s.CreateAgent("Envoy1", "world", "envoy"); err != nil {
		t.Fatal(err)
	}

	for _, p := range []int{1, 2} {
		eligible, err := envoyWakeEligible("world", "Envoy1", p)
		if err != nil {
			t.Fatalf("unexpected error at priority %d: %v", p, err)
		}
		if !eligible {
			t.Errorf("expected envoy recipient to be wake-eligible at priority %d", p)
		}
	}
}

// TestEnvoyWakeEligibleOutpostRoleRejected verifies outposts are never
// wake-eligible regardless of priority — outpost lifecycle is exclusively
// cast/dispatch-owned.
func TestEnvoyWakeEligibleOutpostRoleRejected(t *testing.T) {
	s := setupMaildeliverTestEnv(t)
	if _, err := s.CreateAgent("Out1", "world", "outpost"); err != nil {
		t.Fatal(err)
	}

	eligible, err := envoyWakeEligible("world", "Out1", 1)
	if eligible {
		t.Error("expected outpost recipient to never be wake-eligible")
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestEnvoyWakeEligibleUnknownAgentRejected verifies an unresolvable
// recipient (not registered in the sphere store) is treated as "do not
// wake" rather than erroring.
func TestEnvoyWakeEligibleUnknownAgentRejected(t *testing.T) {
	setupMaildeliverTestEnv(t)

	eligible, err := envoyWakeEligible("world", "Ghost", 1)
	if eligible {
		t.Error("expected unknown recipient to never be wake-eligible")
	}
	if err != nil {
		t.Errorf("unexpected error for unknown recipient: %v", err)
	}
}
