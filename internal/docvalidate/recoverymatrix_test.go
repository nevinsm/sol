package docvalidate

import (
	"strings"
	"testing"
)

// servicePackageFixture mirrors the real internal/service/service.go shape:
// a package-level `var Components = []string{...}` declaration. The check
// must use go/parser, not regex, so the fixture intentionally splits the
// declaration across multiple lines.
const servicePackageFixture = `package service

// Components lists sphere daemons.
var Components = []string{
	"forge",
	"sentinel",
	"broker",
}
`

const failureModesFixture = `# Failure Modes

## Recovery Matrix

| Component | State Survives | State Lost | Recovery Action | Recovery Time |
|-----------|----------------|------------|-----------------|---------------|
| Forge | x | y | restart | <30s |
| Sentinel | x | y | restart | <3 min |

## Graceful Degradation

text after the table.
`

func TestLoadServiceComponents(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, servicePackagePath, servicePackageFixture)

	got, line, err := loadServiceComponents(root)
	if err != nil {
		t.Fatalf("loadServiceComponents: %v", err)
	}
	want := []string{"forge", "sentinel", "broker"}
	if len(got) != len(want) {
		t.Fatalf("got %d entries want %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q want %q", i, got[i], w)
		}
	}
	if line == 0 {
		t.Errorf("expected non-zero declaration line")
	}
}

func TestCheckRecoveryMatrix_FlagsMissingRow(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, servicePackagePath, servicePackageFixture)
	writeFile(t, root, recoveryMatrixDoc, failureModesFixture)

	findings, err := CheckRecoveryMatrix(root)
	if err != nil {
		t.Fatalf("CheckRecoveryMatrix: %v", err)
	}
	// "broker" has no row → 1 finding. Forge and Sentinel are present.
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, "broker") {
		t.Errorf("expected broker in message, got %q", findings[0].Message)
	}
	if !strings.Contains(findings[0].Message, servicePackagePath) {
		t.Errorf("expected source path in message, got %q", findings[0].Message)
	}
}

func TestCheckRecoveryMatrix_PassesWhenAllPresent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, servicePackagePath, `package service
var Components = []string{"forge"}
`)
	writeFile(t, root, recoveryMatrixDoc, failureModesFixture)

	findings, err := CheckRecoveryMatrix(root)
	if err != nil {
		t.Fatalf("CheckRecoveryMatrix: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("expected no findings, got %+v", findings)
	}
}

func TestLoadRecoveryMatrixRows_StopsAtNextH2(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, recoveryMatrixDoc, failureModesFixture)

	rows, err := loadRecoveryMatrixRows(root + "/" + recoveryMatrixDoc)
	if err != nil {
		t.Fatalf("loadRecoveryMatrixRows: %v", err)
	}
	if _, ok := rows["Forge"]; !ok {
		t.Errorf("Forge missing from rows: %v", rows)
	}
	if _, ok := rows["Sentinel"]; !ok {
		t.Errorf("Sentinel missing from rows: %v", rows)
	}
	// Header rows ("Component") and divider rows must not leak in.
	if _, ok := rows["Component"]; ok {
		t.Errorf("'Component' header leaked into rows: %v", rows)
	}
}

func TestIsMarkdownTableDivider(t *testing.T) {
	cases := map[string]bool{
		"|---|---|":   true,
		"|:--|--:|":   true,
		"|-|-|":       true,
		"| Forge | x":  false,
		"text":         false,
	}
	for in, want := range cases {
		if got := isMarkdownTableDivider(in); got != want {
			t.Errorf("isMarkdownTableDivider(%q) = %v want %v", in, got, want)
		}
	}
}
