package docvalidate

import (
	"strings"
	"testing"
)

const integrationTestFixture = `package integration

import "testing"

func TestHandoffBasic(t *testing.T)   {}
func TestHandoffComplex(t *testing.T) {}

// TestHandoffPretendUnused is a real test, it just isn't referenced in any
// acceptance doc — that's fine, the check only flags references that go
// nowhere, not unreferenced tests.
func TestHandoffPretendUnused(t *testing.T) {}

// notATest is excluded by the naming convention even though it's exported.
func NotATest() {}
`

const acceptanceDocFixture = `# Loop X Acceptance

## 1. Handoff
- [x] Handoff covers the basic path (` + "`TestHandoffBasic`" + `)
- [x] Handoff covers the workflow path (` + "`TestHandoffWithWorkflow`" + `)
- [x] Handoff covers two cases (` + "`TestHandoffComplex` and `TestHandoffMissing`" + `)
- [ ] Unchecked items don't matter (` + "`TestNonExistent`" + `)
- [x] Handoff narrative without test ref
`

func TestLoadIntegrationTests(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "test/integration/handoff_test.go", integrationTestFixture)

	got, err := loadIntegrationTests(root)
	if err != nil {
		t.Fatalf("loadIntegrationTests: %v", err)
	}
	wantTests := []string{"TestHandoffBasic", "TestHandoffComplex", "TestHandoffPretendUnused"}
	for _, name := range wantTests {
		if !got[name] {
			t.Errorf("expected %q in registry, got %v", name, got)
		}
	}
	if got["NotATest"] {
		t.Errorf("NotATest should be excluded by naming convention")
	}
}

func TestCheckAcceptanceTests_FlagsMissingFunctions(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "test/integration/handoff_test.go", integrationTestFixture)
	writeFile(t, root, "test/integration/LOOP9_ACCEPTANCE.md", acceptanceDocFixture)

	findings, err := CheckAcceptanceTests(root)
	if err != nil {
		t.Fatalf("CheckAcceptanceTests: %v", err)
	}
	// Expect findings for TestHandoffWithWorkflow and TestHandoffMissing.
	// TestNonExistent is on a [ ] (unchecked) line and must not be flagged.
	gotMsgs := make(map[string]bool)
	for _, f := range findings {
		gotMsgs[f.Message] = true
	}
	if !containsMessage(findings, "TestHandoffWithWorkflow") {
		t.Errorf("expected TestHandoffWithWorkflow finding, got %+v", findings)
	}
	if !containsMessage(findings, "TestHandoffMissing") {
		t.Errorf("expected TestHandoffMissing finding, got %+v", findings)
	}
	if containsMessage(findings, "TestNonExistent") {
		t.Errorf("TestNonExistent is on an unchecked line, should not be flagged: %+v", findings)
	}
	if containsMessage(findings, "TestHandoffBasic") {
		t.Errorf("TestHandoffBasic exists, should not be flagged: %+v", findings)
	}
}

func TestCheckAcceptanceTests_PassesWhenAllExist(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "test/integration/foo_test.go", integrationTestFixture)
	writeFile(t, root, "test/integration/LOOP1_ACCEPTANCE.md",
		"- [x] basic (`TestHandoffBasic`)\n- [x] complex (`TestHandoffComplex`)\n",
	)
	findings, err := CheckAcceptanceTests(root)
	if err != nil {
		t.Fatalf("CheckAcceptanceTests: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("expected no findings, got %+v", findings)
	}
}

func TestCheckAcceptanceTests_DedupsRepeatedReferences(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "test/integration/foo_test.go", integrationTestFixture)
	// One line, same missing test twice → one finding.
	writeFile(t, root, "test/integration/LOOP1_ACCEPTANCE.md",
		"- [x] foo (`TestMissing` and `TestMissing` again)\n",
	)
	findings, err := CheckAcceptanceTests(root)
	if err != nil {
		t.Fatalf("CheckAcceptanceTests: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (dedup), got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, "TestMissing") {
		t.Errorf("expected TestMissing in message, got %q", findings[0].Message)
	}
}
