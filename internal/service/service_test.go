package service

import (
	"strings"
	"testing"
)

func TestComponents(t *testing.T) {
	if len(Components) != 5 {
		t.Fatalf("expected 5 components, got %d", len(Components))
	}
	expected := map[string]bool{
		"prefect":      true,
		"consul":       true,
		"chronicle":    true,
		"ledger":       true,
		"token-broker": true,
	}
	for _, c := range Components {
		if !expected[c] {
			t.Errorf("unexpected component %q", c)
		}
	}
}

func TestUnitName(t *testing.T) {
	tests := []struct {
		component string
		want      string
	}{
		{"prefect", "sol-prefect.service"},
		{"consul", "sol-consul.service"},
		{"chronicle", "sol-chronicle.service"},
		{"token-broker", "sol-token-broker.service"},
	}
	for _, tt := range tests {
		got := UnitName(tt.component)
		if got != tt.want {
			t.Errorf("UnitName(%q) = %q, want %q", tt.component, got, tt.want)
		}
	}
}

func TestGenerateUnit(t *testing.T) {
	content, err := GenerateUnit("consul", "/usr/local/bin/sol", "/home/user/sol")
	if err != nil {
		t.Fatalf("GenerateUnit failed: %v", err)
	}

	checks := []string{
		"Description=Sol consul daemon",
		"Type=simple",
		"ExecStart=/usr/local/bin/sol consul run",
		"Restart=on-failure",
		"RestartSec=5",
		"Environment=SOL_HOME=/home/user/sol",
		"WantedBy=default.target",
	}
	for _, want := range checks {
		if !strings.Contains(content, want) {
			t.Errorf("unit missing %q\ngot:\n%s", want, content)
		}
	}
}

func TestGenerateUnitPrefectDependencies(t *testing.T) {
	content, err := GenerateUnit("prefect", "/usr/local/bin/sol", "/home/user/sol")
	if err != nil {
		t.Fatalf("GenerateUnit(prefect) failed: %v", err)
	}

	// Prefect must have After and Wants directives for all other components.
	depUnits := []string{
		"sol-consul.service",
		"sol-chronicle.service",
		"sol-ledger.service",
		"sol-token-broker.service",
	}
	for _, dep := range depUnits {
		if !strings.Contains(content, dep) {
			t.Errorf("prefect unit missing dependency on %s\ngot:\n%s", dep, content)
		}
	}

	// Verify both After= and Wants= lines exist (beyond the base After=network.target).
	lines := strings.Split(content, "\n")
	var afterCount, wantsCount int
	for _, line := range lines {
		if strings.HasPrefix(line, "After=") && line != "After=network.target" {
			afterCount++
		}
		if strings.HasPrefix(line, "Wants=") {
			wantsCount++
		}
	}
	if afterCount != 1 {
		t.Errorf("expected 1 dependency After= line, got %d\ngot:\n%s", afterCount, content)
	}
	if wantsCount != 1 {
		t.Errorf("expected 1 Wants= line, got %d\ngot:\n%s", wantsCount, content)
	}

	// Prefect must NOT list itself in dependencies.
	if strings.Contains(content, "sol-prefect.service") {
		t.Errorf("prefect unit should not depend on itself\ngot:\n%s", content)
	}
}

func TestGenerateUnitNonPrefectNoDependencies(t *testing.T) {
	nonPrefect := []string{"consul", "chronicle", "ledger", "token-broker"}
	for _, comp := range nonPrefect {
		content, err := GenerateUnit(comp, "/usr/local/bin/sol", "/home/user/sol")
		if err != nil {
			t.Fatalf("GenerateUnit(%s) failed: %v", comp, err)
		}

		// Non-prefect units should not have Wants= directives.
		if strings.Contains(content, "Wants=") {
			t.Errorf("%s unit should not have Wants= directive\ngot:\n%s", comp, content)
		}

		// Should only have the base After=network.target, not dependency After=.
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "After=") && line != "After=network.target" {
				t.Errorf("%s unit has unexpected After= directive: %s\ngot:\n%s", comp, line, content)
			}
		}
	}
}
