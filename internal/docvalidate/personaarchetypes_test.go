package docvalidate

import (
	"strings"
	"testing"
)

const personaDefaultsFixture = `package persona

var knownDefaults = map[string]bool{
	"planner":  true,
	"engineer": true,
}
`

const personasDocFixture = `# Persona Templates

## Built-in templates

| Name       | Archetype | Description |
|------------|-----------|-------------|
| ` + "`planner`" + `  | Polaris   | Planning |
| ` + "`engineer`" + ` | Meridian  | Engineering |
| ` + "`mystery`" + `  | Spectre   | Unknown |

other text.
`

func TestLoadPersonaRegistry(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, personaDefaultsPath, personaDefaultsFixture)

	got, line, err := loadPersonaRegistry(root)
	if err != nil {
		t.Fatalf("loadPersonaRegistry: %v", err)
	}
	if !got["planner"] || !got["engineer"] {
		t.Errorf("expected planner+engineer, got %v", got)
	}
	if got["mystery"] {
		t.Errorf("mystery should not be in registry")
	}
	if line == 0 {
		t.Errorf("expected non-zero declaration line")
	}
}

func TestCheckPersonaArchetypes_FlagsUnregistered(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, personaDefaultsPath, personaDefaultsFixture)
	writeFile(t, root, "docs/personas.md", personasDocFixture)

	findings, err := CheckPersonaArchetypes(root)
	if err != nil {
		t.Fatalf("CheckPersonaArchetypes: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, "mystery") {
		t.Errorf("expected mystery in message, got %q", findings[0].Message)
	}
	if !strings.Contains(findings[0].Message, "Spectre") {
		t.Errorf("expected archetype label in message, got %q", findings[0].Message)
	}
}

func TestCheckPersonaArchetypes_PassesWhenAllRegistered(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, personaDefaultsPath, personaDefaultsFixture)
	writeFile(t, root, "docs/personas.md", `# Persona Templates

| Name | Archetype | Description |
|------|-----------|-------------|
| `+"`planner`"+` | Polaris | … |
| `+"`engineer`"+` | Meridian | … |
`)

	findings, err := CheckPersonaArchetypes(root)
	if err != nil {
		t.Fatalf("CheckPersonaArchetypes: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("expected no findings, got %+v", findings)
	}
}
