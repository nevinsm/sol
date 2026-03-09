package workflow

import (
	"testing"
)

func TestValidateName(t *testing.T) {
	valid := []string{
		"standard",
		"my-workflow",
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
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", name, err)
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
			err := ValidateName(tc.name)
			if err == nil {
				t.Errorf("ValidateName(%q) = nil, want error (%s)", tc.name, tc.desc)
			}
		})
	}
}

func TestResolveRejectsTraversal(t *testing.T) {
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
			_, err := Resolve(name, "")
			if err == nil {
				t.Errorf("Resolve(%q, \"\") = nil error, want validation error", name)
			}
		})
	}
}
