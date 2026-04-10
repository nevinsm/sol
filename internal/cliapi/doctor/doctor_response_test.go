package doctor

import (
	"encoding/json"
	"testing"

	"github.com/nevinsm/sol/internal/doctor"
)

func TestFromReport_Empty(t *testing.T) {
	report := &doctor.Report{Checks: []doctor.CheckResult{}}
	resp := FromReport(report)

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := `{"checks":[]}`
	if string(data) != want {
		t.Errorf("got %s, want %s", data, want)
	}
}

func TestFromReport_SingleCheckPassed(t *testing.T) {
	report := &doctor.Report{
		Checks: []doctor.CheckResult{
			{
				Name:    "tmux",
				Passed:  true,
				Message: "tmux 3.5a (/usr/bin/tmux)",
				Fix:     "",
			},
		},
	}
	resp := FromReport(report)

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks, ok := got["checks"].([]any)
	if !ok || len(checks) != 1 {
		t.Fatalf("expected 1 check, got %v", got["checks"])
	}

	check := checks[0].(map[string]any)
	if check["name"] != "tmux" {
		t.Errorf("name = %v, want tmux", check["name"])
	}
	if check["passed"] != true {
		t.Errorf("passed = %v, want true", check["passed"])
	}
	if _, ok := check["warning"]; ok {
		t.Error("warning should be omitted when false")
	}
	if check["fix"] != "" {
		t.Errorf("fix = %v, want empty string", check["fix"])
	}
}

func TestFromReport_FailedCheckWithFix(t *testing.T) {
	report := &doctor.Report{
		Checks: []doctor.CheckResult{
			{
				Name:    "git",
				Passed:  false,
				Message: "git not found in PATH",
				Fix:     "Install git",
			},
		},
	}
	resp := FromReport(report)

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := got["checks"].([]any)
	check := checks[0].(map[string]any)
	if check["passed"] != false {
		t.Errorf("passed = %v, want false", check["passed"])
	}
	if check["fix"] != "Install git" {
		t.Errorf("fix = %v, want 'Install git'", check["fix"])
	}
}

func TestFromReport_WarningCheck(t *testing.T) {
	report := &doctor.Report{
		Checks: []doctor.CheckResult{
			{
				Name:    "migrations",
				Passed:  true,
				Warning: true,
				Message: "2 pending migrations",
				Fix:     "",
			},
		},
	}
	resp := FromReport(report)

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := got["checks"].([]any)
	check := checks[0].(map[string]any)
	if check["warning"] != true {
		t.Errorf("warning = %v, want true", check["warning"])
	}
}

func TestFromReport_ByteEquivalence(t *testing.T) {
	// Verify that the cliapi type produces identical JSON to the raw doctor.Report.
	report := &doctor.Report{
		Checks: []doctor.CheckResult{
			{Name: "tmux", Passed: true, Message: "tmux 3.5a (/usr/bin/tmux)", Fix: ""},
			{Name: "git", Passed: false, Message: "git not found", Fix: "Install git"},
			{Name: "migrations", Passed: true, Warning: true, Message: "1 pending", Fix: ""},
		},
	}

	originalJSON, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal original: %v", err)
	}

	resp := FromReport(report)
	migratedJSON, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal migrated: %v", err)
	}

	if string(originalJSON) != string(migratedJSON) {
		t.Errorf("JSON output differs:\n  original: %s\n  migrated: %s", originalJSON, migratedJSON)
	}
}

func TestFromReport_MultipleChecks(t *testing.T) {
	report := &doctor.Report{
		Checks: []doctor.CheckResult{
			{Name: "tmux", Passed: true, Message: "ok", Fix: ""},
			{Name: "git", Passed: true, Message: "ok", Fix: ""},
			{Name: "claude", Passed: true, Message: "ok", Fix: ""},
		},
	}
	resp := FromReport(report)

	if len(resp.Checks) != 3 {
		t.Errorf("got %d checks, want 3", len(resp.Checks))
	}

	for i, check := range resp.Checks {
		if check.Name != report.Checks[i].Name {
			t.Errorf("check[%d].Name = %s, want %s", i, check.Name, report.Checks[i].Name)
		}
	}
}
