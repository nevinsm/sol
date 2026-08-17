package giterr

import (
	"errors"
	"strings"
	"testing"
)

func TestIsAuthFailure(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "could not read username",
			output: "fatal: could not read Username for 'https://github.com': terminal prompts disabled",
			want:   true,
		},
		{
			name:   "could not read password",
			output: "fatal: could not read Password for 'https://github.com': terminal prompts disabled",
			want:   true,
		},
		{
			name:   "authentication failed",
			output: "remote: Invalid username or password.\nfatal: Authentication failed for 'https://github.com/x/y.git/'",
			want:   true,
		},
		{
			name:   "unrelated network error",
			output: "fatal: unable to access 'https://github.com/x/y.git/': Could not resolve host: github.com",
			want:   false,
		},
		{
			name:   "unrelated ref error",
			output: "fatal: couldn't find remote ref refs/heads/does-not-exist",
			want:   false,
		},
		{
			name:   "empty output",
			output: "",
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAuthFailure([]byte(tt.output)); got != tt.want {
				t.Errorf("IsAuthFailure(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

func TestWrapNilErr(t *testing.T) {
	if err := Wrap(nil, []byte("could not read Username")); err != nil {
		t.Errorf("Wrap(nil, ...) = %v, want nil", err)
	}
}

func TestWrapNonAuthFailurePassesThrough(t *testing.T) {
	base := errors.New("exit status 128")
	out := []byte("fatal: unable to access 'https://github.com/x/y.git/': Could not resolve host: github.com")

	got := Wrap(base, out)
	if got != base {
		t.Errorf("Wrap() = %v, want unchanged base error %v", got, base)
	}
}

func TestWrapAuthFailureAddsGuidance(t *testing.T) {
	base := errors.New("failed to fetch for world \"personal-site\": fatal: could not read Username for 'https://github.com': terminal prompts disabled: exit status 128")
	out := []byte("fatal: could not read Username for 'https://github.com': terminal prompts disabled")

	got := Wrap(base, out)
	if got == nil {
		t.Fatal("Wrap() = nil, want wrapped error")
	}
	if !strings.Contains(got.Error(), "docs/credentials.md") {
		t.Errorf("Wrap() error = %q, want mention of docs/credentials.md", got.Error())
	}
	if !errors.Is(got, base) {
		t.Errorf("Wrap() error does not unwrap to the original error via errors.Is")
	}
}
