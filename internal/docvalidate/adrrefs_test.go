package docvalidate

import (
	"strings"
	"testing"
)

// adrIndexFixture builds a minimal docs/decisions/README.md with two
// superseded ADRs (0002, 0027) and one accepted ADR (0028).
const adrIndexFixture = `# Architecture Decision Records

| # | Title | Status | Summary |
|---|-------|--------|---------|
| 0001 | First | Accepted | … |
| 0002 | Forge as Go Process | Superseded by ADR-0005 | … |
| 0027 | Forge as Deterministic Go Process | Superseded by ADR-0028 | … |
| 0028 | Event-Driven Forge | Accepted | … |
`

func TestCheckADRReferences_FlagsSupersededCitations(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/decisions/README.md", adrIndexFixture)
	// CLAUDE.md cites a superseded ADR — should be flagged.
	writeFile(t, root, "CLAUDE.md", "Forge follows ADR-0027 today.\n")
	// docs/manifesto.md cites an accepted ADR — should not be flagged.
	writeFile(t, root, "docs/manifesto.md", "See ADR-0028 for forge.\n")
	// An ADR file under docs/decisions/ also cites 0027 — should NOT be
	// flagged (ADRs reference predecessors as part of their content).
	writeFile(t, root, "docs/decisions/0028-foo.md", "Supersedes ADR-0027.\n")

	findings, err := CheckADRReferences(root)
	if err != nil {
		t.Fatalf("CheckADRReferences: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	got := findings[0]
	if got.File != "CLAUDE.md" {
		t.Errorf("File = %q want CLAUDE.md", got.File)
	}
	if got.Line != 1 {
		t.Errorf("Line = %d want 1", got.Line)
	}
	if !strings.Contains(got.Message, "ADR-0027") || !strings.Contains(got.Message, "ADR-0028") {
		t.Errorf("Message should reference both ADR-0027 and replacement ADR-0028, got %q", got.Message)
	}
}

func TestCheckADRReferences_NoFalsePositiveOnFreshADR(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/decisions/README.md", adrIndexFixture)
	writeFile(t, root, "docs/principles.md", "We follow ADR-0001 and ADR-0028.\n")

	findings, err := CheckADRReferences(root)
	if err != nil {
		t.Fatalf("CheckADRReferences: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckADRReferences_DedupsRepeatedReferences(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/decisions/README.md", adrIndexFixture)
	// One line that mentions ADR-0027 twice — should produce one finding,
	// not two.
	writeFile(t, root, "docs/operations.md", "ADR-0027 and ADR-0027 again.\n")

	findings, err := CheckADRReferences(root)
	if err != nil {
		t.Fatalf("CheckADRReferences: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
}

func TestLoadSupersededADRs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/decisions/README.md", adrIndexFixture)

	got, err := loadSupersededADRs(root)
	if err != nil {
		t.Fatalf("loadSupersededADRs: %v", err)
	}
	want := map[string]string{"0002": "0005", "0027": "0028"}
	if len(got) != len(want) {
		t.Fatalf("got %d entries want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("got[%q] = %q want %q", k, got[k], v)
		}
	}
}
