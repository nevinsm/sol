package service

import (
	"strings"
	"testing"
)

func TestGenerateUnit(t *testing.T) {
	content, err := GenerateUnit("prefect", "/usr/local/bin/sol", "/home/user/sol")
	if err != nil {
		t.Fatalf("GenerateUnit failed: %v", err)
	}

	checks := []string{
		"Description=Sol prefect daemon",
		"Type=simple",
		"ExecStart=/usr/local/bin/sol prefect run",
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

func TestUnitName(t *testing.T) {
	tests := []struct {
		component string
		want      string
	}{
		{"prefect", "sol-prefect.service"},
		{"consul", "sol-consul.service"},
		{"chronicle", "sol-chronicle.service"},
	}
	for _, tt := range tests {
		got := UnitName(tt.component)
		if got != tt.want {
			t.Errorf("UnitName(%q) = %q, want %q", tt.component, got, tt.want)
		}
	}
}

func TestComponents(t *testing.T) {
	if len(Components) != 4 {
		t.Fatalf("expected 4 components, got %d", len(Components))
	}
	expected := map[string]bool{"prefect": true, "consul": true, "chronicle": true, "ledger": true}
	for _, c := range Components {
		if !expected[c] {
			t.Errorf("unexpected component %q", c)
		}
	}
}
