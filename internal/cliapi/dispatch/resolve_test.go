package dispatch

import (
	"encoding/json"
	"testing"

	"github.com/nevinsm/sol/internal/dispatch"
)

func TestFromResolveResultCodeWrit(t *testing.T) {
	dr := &dispatch.ResolveResult{
		WritID:         "sol-a1b2c3d4e5f6a7b8",
		Title:          "Add feature X",
		AgentName:      "Nova",
		BranchName:     "outpost/Nova/sol-a1b2c3d4e5f6a7b8",
		MergeRequestID: "mr-0000000000000001",
		SessionKept:    false,
	}

	res := FromResolveResult(dr, "code", "main")

	if res.WritID != dr.WritID {
		t.Errorf("WritID = %q, want %q", res.WritID, dr.WritID)
	}
	if res.Agent != "Nova" {
		t.Errorf("Agent = %q, want %q", res.Agent, "Nova")
	}
	if res.Kind != "code" {
		t.Errorf("Kind = %q, want %q", res.Kind, "code")
	}
	if res.Branch != dr.BranchName {
		t.Errorf("Branch = %q, want %q", res.Branch, dr.BranchName)
	}
	if res.TargetBranch != "main" {
		t.Errorf("TargetBranch = %q, want %q", res.TargetBranch, "main")
	}
	if res.MRID != dr.MergeRequestID {
		t.Errorf("MRID = %q, want %q", res.MRID, dr.MergeRequestID)
	}
	if res.Closed {
		t.Error("Closed = true, want false for code writ")
	}
}

func TestFromResolveResultEmptyKindDefaultsToCode(t *testing.T) {
	dr := &dispatch.ResolveResult{
		WritID:         "sol-a1b2c3d4e5f6a7b8",
		AgentName:      "Nova",
		BranchName:     "outpost/Nova/sol-a1b2c3d4e5f6a7b8",
		MergeRequestID: "mr-0000000000000001",
	}

	res := FromResolveResult(dr, "", "main")

	if res.Kind != "code" {
		t.Errorf("Kind = %q, want %q (empty kind should default to code)", res.Kind, "code")
	}
	if res.Branch != dr.BranchName {
		t.Errorf("Branch = %q, want %q", res.Branch, dr.BranchName)
	}
	if res.Closed {
		t.Error("Closed = true, want false for defaulted code writ")
	}
}

func TestFromResolveResultNonCodeWrit(t *testing.T) {
	dr := &dispatch.ResolveResult{
		WritID:    "sol-b1b2c3d4e5f6a7b8",
		AgentName: "Toast",
	}

	res := FromResolveResult(dr, "analysis", "main")

	if res.WritID != dr.WritID {
		t.Errorf("WritID = %q, want %q", res.WritID, dr.WritID)
	}
	if res.Agent != "Toast" {
		t.Errorf("Agent = %q, want %q", res.Agent, "Toast")
	}
	if res.Kind != "analysis" {
		t.Errorf("Kind = %q, want %q", res.Kind, "analysis")
	}
	if res.Branch != "" {
		t.Errorf("Branch = %q, want empty for non-code writ", res.Branch)
	}
	if res.TargetBranch != "" {
		t.Errorf("TargetBranch = %q, want empty for non-code writ", res.TargetBranch)
	}
	if res.MRID != "" {
		t.Errorf("MRID = %q, want empty for non-code writ", res.MRID)
	}
	if !res.Closed {
		t.Error("Closed = false, want true for non-code writ")
	}
}

func TestResolveResultJSONTags(t *testing.T) {
	res := ResolveResult{
		WritID:       "sol-a1b2c3d4e5f6a7b8",
		Agent:        "Nova",
		Kind:         "code",
		Branch:       "outpost/Nova/sol-a1b2c3d4e5f6a7b8",
		TargetBranch: "main",
		MRID:         "mr-0000000000000001",
	}

	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	expected := map[string]string{
		"writ_id":       "sol-a1b2c3d4e5f6a7b8",
		"agent":         "Nova",
		"kind":          "code",
		"branch":        "outpost/Nova/sol-a1b2c3d4e5f6a7b8",
		"target_branch": "main",
		"mr_id":         "mr-0000000000000001",
	}

	for key, want := range expected {
		got, ok := m[key]
		if !ok {
			t.Errorf("missing JSON key %q", key)
			continue
		}
		if got != want {
			t.Errorf("JSON key %q = %v, want %q", key, got, want)
		}
	}

	// closed should be omitted for code writs (zero value with omitempty)
	if _, ok := m["closed"]; ok {
		t.Error("closed should be omitted for code writs")
	}
}

func TestResolveResultNonCodeJSONOmitsEmptyFields(t *testing.T) {
	res := ResolveResult{
		WritID: "sol-b1b2c3d4e5f6a7b8",
		Agent:  "Toast",
		Kind:   "analysis",
		Closed: true,
	}

	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	// branch, target_branch, mr_id should be omitted
	for _, key := range []string{"branch", "target_branch", "mr_id"} {
		if _, ok := m[key]; ok {
			t.Errorf("key %q should be omitted for non-code writ", key)
		}
	}

	// closed should be present and true
	if closed, ok := m["closed"]; !ok || closed != true {
		t.Errorf("closed = %v, want true", closed)
	}
}
