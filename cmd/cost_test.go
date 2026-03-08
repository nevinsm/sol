package cmd

import (
	"testing"
	"time"
)

func TestParseSinceFlagDuration(t *testing.T) {
	before := time.Now()
	result, err := parseSinceFlag("24h")
	after := time.Now()
	if err != nil {
		t.Fatal(err)
	}

	expected := before.Add(-24 * time.Hour)
	if result.Before(expected.Add(-time.Second)) || result.After(after.Add(-24*time.Hour).Add(time.Second)) {
		t.Fatalf("expected ~24h ago, got %v", result)
	}
}

func TestParseSinceFlagDays(t *testing.T) {
	before := time.Now()
	result, err := parseSinceFlag("7d")
	after := time.Now()
	if err != nil {
		t.Fatal(err)
	}

	expected := before.Add(-7 * 24 * time.Hour)
	if result.Before(expected.Add(-time.Second)) || result.After(after.Add(-7*24*time.Hour).Add(time.Second)) {
		t.Fatalf("expected ~7 days ago, got %v", result)
	}
}

func TestParseSinceFlagDate(t *testing.T) {
	result, err := parseSinceFlag("2026-03-01")
	if err != nil {
		t.Fatal(err)
	}

	expected := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, result)
	}
}

func TestParseSinceFlagRFC3339(t *testing.T) {
	result, err := parseSinceFlag("2026-03-01T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	expected := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, result)
	}
}

func TestParseSinceFlagInvalid(t *testing.T) {
	_, err := parseSinceFlag("not-a-date")
	if err == nil {
		t.Fatal("expected error for invalid since value")
	}
}

func TestFormatDollars(t *testing.T) {
	tests := []struct {
		amount   float64
		expected string
	}{
		{0, "$0.00"},
		{1.5, "$1.50"},
		{12.456, "$12.46"},
		{0.001, "$0.00"},
		{100.999, "$101.00"},
	}

	for _, tc := range tests {
		result := formatDollars(tc.amount)
		if result != tc.expected {
			t.Errorf("formatDollars(%f) = %q, want %q", tc.amount, result, tc.expected)
		}
	}
}

func TestStoreToConfigSummaries(t *testing.T) {
	// Import store.TokenSummary is same package scope via cmd package.
	// This test verifies the conversion helper doesn't panic on empty input.
	result := storeToConfigSummaries(nil)
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d", len(result))
	}
}
