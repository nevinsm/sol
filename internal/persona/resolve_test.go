package persona

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"planner", false},
		{"engineer", false},
		{"my-custom", false},
		{"my_custom_2", false},
		{"A1", false},
		{"", true},
		{".hidden", true},
		{"../traversal", true},
		{"has/slash", true},
		{"-leading-dash", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateName(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestResolveEmbedded(t *testing.T) {
	// Point SOL_HOME to a temp dir so no user-level files interfere.
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	tests := []struct {
		name     string
		wantTier Tier
	}{
		{"planner", TierEmbedded},
		{"engineer", TierEmbedded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Resolve(tt.name, "")
			if err != nil {
				t.Fatalf("Resolve(%q) error: %v", tt.name, err)
			}
			if res.Tier != tt.wantTier {
				t.Errorf("Resolve(%q) tier = %q, want %q", tt.name, res.Tier, tt.wantTier)
			}
			if len(res.Content) == 0 {
				t.Errorf("Resolve(%q) returned empty content", tt.name)
			}
		})
	}
}

func TestResolveUnknown(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	_, err := Resolve("nonexistent", "")
	if err == nil {
		t.Fatal("expected error for unknown persona, got nil")
	}
}

func TestResolveUserTier(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// Create a user-level persona file.
	personasDir := filepath.Join(tmp, "personas")
	if err := os.MkdirAll(personasDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("# Custom user persona\n")
	if err := os.WriteFile(filepath.Join(personasDir, "custom.md"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Resolve("custom", "")
	if err != nil {
		t.Fatalf("Resolve(custom) error: %v", err)
	}
	if res.Tier != TierUser {
		t.Errorf("Resolve(custom) tier = %q, want %q", res.Tier, TierUser)
	}
	if string(res.Content) != string(content) {
		t.Errorf("Resolve(custom) content = %q, want %q", res.Content, content)
	}
}

func TestResolveUserShadowsEmbedded(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// Create a user-level file that shadows the embedded "planner".
	personasDir := filepath.Join(tmp, "personas")
	if err := os.MkdirAll(personasDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("# My custom planner\n")
	if err := os.WriteFile(filepath.Join(personasDir, "planner.md"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Resolve("planner", "")
	if err != nil {
		t.Fatalf("Resolve(planner) error: %v", err)
	}
	if res.Tier != TierUser {
		t.Errorf("tier = %q, want %q", res.Tier, TierUser)
	}
	if string(res.Content) != string(content) {
		t.Errorf("content mismatch: got user-shadow content = %q", res.Content)
	}
}

func TestResolveProjectTier(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// Create a project-level persona file.
	repoPath := filepath.Join(tmp, "repo")
	projectDir := filepath.Join(repoPath, ".sol", "personas")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("# Project planner\n")
	if err := os.WriteFile(filepath.Join(projectDir, "planner.md"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Resolve("planner", repoPath)
	if err != nil {
		t.Fatalf("Resolve(planner) error: %v", err)
	}
	if res.Tier != TierProject {
		t.Errorf("tier = %q, want %q", res.Tier, TierProject)
	}
	if string(res.Content) != string(content) {
		t.Errorf("content mismatch")
	}
}

func TestResolveProjectShadowsUser(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// Create user-level persona.
	userDir := filepath.Join(tmp, "personas")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "custom.md"), []byte("# User custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create project-level persona that shadows it.
	repoPath := filepath.Join(tmp, "repo")
	projectDir := filepath.Join(repoPath, ".sol", "personas")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	projectContent := []byte("# Project custom\n")
	if err := os.WriteFile(filepath.Join(projectDir, "custom.md"), projectContent, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Resolve("custom", repoPath)
	if err != nil {
		t.Fatalf("Resolve(custom) error: %v", err)
	}
	if res.Tier != TierProject {
		t.Errorf("tier = %q, want %q", res.Tier, TierProject)
	}
	if string(res.Content) != string(projectContent) {
		t.Errorf("content mismatch — project should shadow user")
	}
}

// TestResolveLogsSoftFailOnUnreadable verifies that when the persona file
// path exists but cannot be read (here: it's a directory, which yields a
// non-IsNotExist error from os.ReadFile), Resolve emits a soft-failure
// warning via slog.Default and falls through to the next tier instead of
// silently dropping the persona. (CF-L2 / pattern P1.)
func TestResolveLogsSoftFailOnUnreadable(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// Capture slog.Default output for the duration of the test.
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// Create the project-tier path as a *directory* so os.ReadFile returns
	// an error that is not os.ErrNotExist.
	repoPath := filepath.Join(tmp, "repo")
	projectFile := ProjectPath(repoPath, "planner")
	if err := os.MkdirAll(projectFile, 0o755); err != nil {
		t.Fatal(err)
	}

	// Resolve should still succeed by falling through to the embedded tier.
	res, err := Resolve("planner", repoPath)
	if err != nil {
		t.Fatalf("Resolve(planner) error: %v", err)
	}
	if res.Tier != TierEmbedded {
		t.Errorf("expected fall-through to embedded tier, got %q", res.Tier)
	}

	out := buf.String()
	if !strings.Contains(out, "soft failure") {
		t.Errorf("expected soft-failure log, got: %s", out)
	}
	if !strings.Contains(out, "persona.Resolve") {
		t.Errorf("expected op identifier in log, got: %s", out)
	}
}
