package service

import (
	"errors"
	"strings"
	"testing"
)

func TestComponents(t *testing.T) {
	if len(Components) != 5 {
		t.Fatalf("expected 5 components, got %d", len(Components))
	}
	expected := map[string]bool{
		"prefect":   true,
		"consul":    true,
		"chronicle": true,
		"ledger":    true,
		"broker":    true,
	}
	for _, c := range Components {
		if !expected[c] {
			t.Errorf("unexpected component %q", c)
		}
	}
}

func TestServiceLabel(t *testing.T) {
	tests := []struct {
		component string
		want      string
	}{
		{"prefect", "com.sol.prefect"},
		{"consul", "com.sol.consul"},
		{"chronicle", "com.sol.chronicle"},
		{"broker", "com.sol.broker"},
	}
	for _, tt := range tests {
		got := ServiceLabel(tt.component)
		if got != tt.want {
			t.Errorf("ServiceLabel(%q) = %q, want %q", tt.component, got, tt.want)
		}
	}
}

func TestGeneratePlist(t *testing.T) {
	content, err := GeneratePlist("consul", "/usr/local/bin/sol", "/Users/testuser/sol", "/usr/bin:/bin")
	if err != nil {
		t.Fatalf("GeneratePlist failed: %v", err)
	}

	checks := []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"`,
		`<plist version="1.0">`,
		`<key>Label</key>`,
		`<string>com.sol.consul</string>`,
		`<key>ProgramArguments</key>`,
		`<string>/usr/local/bin/sol</string>`,
		`<string>consul</string>`,
		`<string>run</string>`,
		`<key>KeepAlive</key>`,
		`<true/>`,
		`<key>SOL_HOME</key>`,
		`<string>/Users/testuser/sol</string>`,
		`<key>PATH</key>`,
		`<string>/usr/bin:/bin</string>`,
		`<key>StandardOutPath</key>`,
		`<string>/Users/testuser/sol/logs/consul.out.log</string>`,
		`<key>StandardErrorPath</key>`,
		`<string>/Users/testuser/sol/logs/consul.err.log</string>`,
		`</plist>`,
	}
	for _, want := range checks {
		if !strings.Contains(content, want) {
			t.Errorf("plist missing %q\ngot:\n%s", want, content)
		}
	}
}

func TestGeneratePlistAllComponents(t *testing.T) {
	for _, comp := range Components {
		content, err := GeneratePlist(comp, "/usr/local/bin/sol", "/Users/testuser/sol", "/usr/bin:/bin")
		if err != nil {
			t.Fatalf("GeneratePlist(%s) failed: %v", comp, err)
		}

		label := ServiceLabel(comp)
		if !strings.Contains(content, "<string>"+label+"</string>") {
			t.Errorf("plist for %s missing label %s", comp, label)
		}
		if !strings.Contains(content, "<string>"+comp+"</string>") {
			t.Errorf("plist for %s missing component argument", comp)
		}
		if !strings.Contains(content, "<true/>") {
			t.Errorf("plist for %s missing KeepAlive", comp)
		}
		if !strings.Contains(content, "<key>PATH</key>") {
			t.Errorf("plist for %s missing PATH key", comp)
		}
	}
}

func TestGeneratePlistPATHEscaping(t *testing.T) {
	// XML special characters in the PATH value must be escaped so the
	// generated plist remains well-formed.
	rawPath := `/opt/a&b/bin:/opt/<weird>/bin`
	content, err := GeneratePlist("consul", "/usr/local/bin/sol", "/Users/testuser/sol", rawPath)
	if err != nil {
		t.Fatalf("GeneratePlist failed: %v", err)
	}
	want := `/opt/a&amp;b/bin:/opt/&lt;weird&gt;/bin`
	if !strings.Contains(content, want) {
		t.Errorf("plist PATH not escaped as expected; want %q in:\n%s", want, content)
	}
	if strings.Contains(content, rawPath) {
		t.Errorf("plist contains unescaped raw PATH value:\n%s", content)
	}
}

func TestGeneratePlistEmptyPATH(t *testing.T) {
	_, err := GeneratePlist("consul", "/usr/local/bin/sol", "/Users/testuser/sol", "")
	if !errors.Is(err, ErrEmptyPATH) {
		t.Errorf("GeneratePlist with empty PATH = %v, want ErrEmptyPATH", err)
	}

	_, err = GeneratePlist("consul", "/usr/local/bin/sol", "/Users/testuser/sol", "   ")
	if !errors.Is(err, ErrEmptyPATH) {
		t.Errorf("GeneratePlist with whitespace-only PATH = %v, want ErrEmptyPATH", err)
	}
}
