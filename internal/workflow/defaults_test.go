package workflow

import (
	"testing"
)

func TestValidateFormulaName(t *testing.T) {
	valid := []string{
		"standard",
		"my-formula",
		"v2_build",
		"default-work",
		"A",
		"rule-of-five",
		"code-review",
		"thorough-work",
		"idea-to-plan",
		"deep-scan",
	}
	for _, name := range valid {
		if err := ValidateFormulaName(name); err != nil {
			t.Errorf("ValidateFormulaName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []struct {
		name string
		desc string
	}{
		{"../escape", "dot-dot traversal"},
		{"../../etc/passwd", "multi-level traversal"},
		{"foo/bar", "forward slash"},
		{"foo\\bar", "backslash"},
		{".hidden", "leading dot"},
		{"..sneaky", "leading double dot"},
		{"", "empty string"},
		{"-leading-hyphen", "leading hyphen"},
		{"_leading-underscore", "leading underscore"},
		{"hello world", "space in name"},
		{"name\ttab", "tab in name"},
		{"with.dot", "dot in middle"},
	}
	for _, tc := range invalid {
		t.Run(tc.desc, func(t *testing.T) {
			err := ValidateFormulaName(tc.name)
			if err == nil {
				t.Errorf("ValidateFormulaName(%q) = nil, want error (%s)", tc.name, tc.desc)
			}
		})
	}
}

func TestEnsureFormulaRejectsTraversal(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	cases := []string{
		"../escape",
		"../../etc/passwd",
		"foo/bar",
		".hidden",
		"foo\\bar",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := EnsureFormula(name, "")
			if err == nil {
				t.Errorf("EnsureFormula(%q, \"\") = nil error, want validation error", name)
			}
		})
	}
}
