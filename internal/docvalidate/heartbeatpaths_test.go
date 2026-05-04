package docvalidate

import (
	"strings"
	"testing"
)

const ledgerHeartbeatFixture = `package ledger

import (
	"path/filepath"
	"github.com/nevinsm/sol/internal/config"
)

func HeartbeatPath() string {
	return filepath.Join(config.RuntimeDir(), "ledger-heartbeat.json")
}
`

const sentinelHeartbeatFixture = `package sentinel

import (
	"path/filepath"
	"github.com/nevinsm/sol/internal/config"
)

func HeartbeatPath(world string) string {
	return filepath.Join(config.Home(), world, "sentinel.heartbeat")
}
`

const operationsDocFixture = `# Operations

## Monitoring health

### Heartbeat files

| Component | Heartbeat path | Stale after |
|-----------|----------------|-------------|
| Sentinel | ` + "`$SOL_HOME/{world}/sentinel.heartbeat`" + ` | 15 minutes |
| Ledger | ` + "`$SOL_HOME/.runtime/ledger.heartbeat`" + ` | 5 minutes |

text after table
`

func TestRenderPathExpr_AllShapes(t *testing.T) {
	const consulFixture = `package consul

import "path/filepath"

func HeartbeatPath(solHome string) string {
	return filepath.Join(solHome, "consul", "heartbeat.json")
}
`
	cases := []struct {
		name   string
		fixture string
		want    string
	}{
		{
			name: "ledger",
			fixture: ledgerHeartbeatFixture,
			want:    "$SOL_HOME/.runtime/ledger-heartbeat.json",
		},
		{
			name: "sentinel",
			fixture: sentinelHeartbeatFixture,
			want:    "$SOL_HOME/{world}/sentinel.heartbeat",
		},
		{
			name:    "consul",
			fixture: consulFixture,
			want:    "$SOL_HOME/consul/heartbeat.json",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "internal/"+tc.name+"/heartbeat.go", tc.fixture)
			paths, err := loadCodeHeartbeatPaths(root)
			if err != nil {
				t.Fatalf("loadCodeHeartbeatPaths: %v", err)
			}
			capName := strings.ToUpper(tc.name[:1]) + tc.name[1:]
			if got := paths[capName]; got != tc.want {
				t.Errorf("paths[%q] = %q want %q (all: %v)", capName, got, tc.want, paths)
			}
		})
	}
}

func TestCheckHeartbeatPaths_DriftDetected(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "internal/ledger/heartbeat.go", ledgerHeartbeatFixture)
	writeFile(t, root, "internal/sentinel/heartbeat.go", sentinelHeartbeatFixture)
	writeFile(t, root, "docs/operations.md", operationsDocFixture)

	findings, err := CheckHeartbeatPaths(root)
	if err != nil {
		t.Fatalf("CheckHeartbeatPaths: %v", err)
	}
	// Sentinel matches; Ledger doesn't (doc has ".heartbeat" not "-heartbeat.json").
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, "Ledger") {
		t.Errorf("expected Ledger in message, got %q", findings[0].Message)
	}
	if !strings.Contains(findings[0].Message, "ledger-heartbeat.json") {
		t.Errorf("expected actual code path in message, got %q", findings[0].Message)
	}
}

func TestCheckHeartbeatPaths_MissingDocRow(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "internal/broker/heartbeat.go", `package broker

import (
	"path/filepath"
	"github.com/nevinsm/sol/internal/config"
)

func heartbeatPath() string {
	return filepath.Join(config.RuntimeDir(), "broker-heartbeat.json")
}
`)
	// Doc has no Broker row at all.
	writeFile(t, root, "docs/operations.md", operationsDocFixture)

	findings, err := CheckHeartbeatPaths(root)
	if err != nil {
		t.Fatalf("CheckHeartbeatPaths: %v", err)
	}
	if !containsMessage(findings, "missing heartbeat-path row") {
		t.Errorf("expected missing-row finding, got %+v", findings)
	}
}

func TestCapitalizeASCII(t *testing.T) {
	cases := map[string]string{
		"":        "",
		"forge":   "Forge",
		"Broker":  "Broker",
		"123abc":  "123abc",
	}
	for in, want := range cases {
		if got := capitalizeASCII(in); got != want {
			t.Errorf("capitalizeASCII(%q) = %q want %q", in, got, want)
		}
	}
}
