package config

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPricingValid(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	solToml := `
[pricing.claude-sonnet-4-6]
input = 3.0
output = 15.0
cache_read = 0.30
cache_creation = 3.75

[pricing.claude-opus-4-6]
input = 15.0
output = 75.0
cache_read = 1.50
cache_creation = 18.75
`
	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte(solToml), 0o644); err != nil {
		t.Fatal(err)
	}

	pc, err := LoadPricing()
	if err != nil {
		t.Fatal(err)
	}
	if len(pc) != 2 {
		t.Fatalf("expected 2 pricing models, got %d", len(pc))
	}

	sonnet, ok := pc["claude-sonnet-4-6"]
	if !ok {
		t.Fatal("expected claude-sonnet-4-6 in pricing")
	}
	if sonnet.Input != 3.0 {
		t.Fatalf("sonnet input = %f, want 3.0", sonnet.Input)
	}
	if sonnet.Output != 15.0 {
		t.Fatalf("sonnet output = %f, want 15.0", sonnet.Output)
	}
	if sonnet.CacheRead != 0.30 {
		t.Fatalf("sonnet cache_read = %f, want 0.30", sonnet.CacheRead)
	}
	if sonnet.CacheCreation != 3.75 {
		t.Fatalf("sonnet cache_creation = %f, want 3.75", sonnet.CacheCreation)
	}

	opus, ok := pc["claude-opus-4-6"]
	if !ok {
		t.Fatal("expected claude-opus-4-6 in pricing")
	}
	if opus.Input != 15.0 {
		t.Fatalf("opus input = %f, want 15.0", opus.Input)
	}
}

func TestLoadPricingMissingSection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// sol.toml with no [pricing] section.
	solToml := `
[agents]
capacity = 4
`
	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte(solToml), 0o644); err != nil {
		t.Fatal(err)
	}

	pc, err := LoadPricing()
	if err != nil {
		t.Fatal(err)
	}
	if pc == nil {
		t.Fatal("expected non-nil PricingConfig")
	}
	if len(pc) != 0 {
		t.Fatalf("expected empty PricingConfig, got %d entries", len(pc))
	}
}

func TestLoadPricingMissingFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// No sol.toml at all.
	pc, err := LoadPricing()
	if err != nil {
		t.Fatal(err)
	}
	if pc == nil {
		t.Fatal("expected non-nil PricingConfig")
	}
	if len(pc) != 0 {
		t.Fatalf("expected empty PricingConfig, got %d entries", len(pc))
	}
}

func TestLoadPricingInvalidTOML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte("not valid { toml"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadPricing()
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

func TestComputeCostNormal(t *testing.T) {
	pc := PricingConfig{
		"claude-sonnet-4-6": {
			Input:         3.0,
			Output:        15.0,
			CacheRead:     0.30,
			CacheCreation: 3.75,
		},
	}

	summaries := []TokenSummary{
		{
			Model:               "claude-sonnet-4-6",
			InputTokens:         1_000_000,
			OutputTokens:        500_000,
			CacheReadTokens:     200_000,
			CacheCreationTokens: 100_000,
		},
	}

	// Expected: (1M * 3.0 + 500K * 15.0 + 200K * 0.30 + 100K * 3.75) / 1M
	//         = (3000000 + 7500000 + 60000 + 375000) / 1000000
	//         = 10935000 / 1000000
	//         = 10.935
	total, unpriced := pc.ComputeCost(summaries)

	if len(unpriced) != 0 {
		t.Fatalf("expected 0 unpriced, got %v", unpriced)
	}
	expected := 10.935
	if math.Abs(total-expected) > 0.0001 {
		t.Fatalf("total = %f, want %f", total, expected)
	}
}

func TestComputeCostUnpricedModels(t *testing.T) {
	pc := PricingConfig{
		"claude-sonnet-4-6": {
			Input:  3.0,
			Output: 15.0,
		},
	}

	summaries := []TokenSummary{
		{Model: "claude-sonnet-4-6", InputTokens: 1000, OutputTokens: 500},
		{Model: "claude-opus-4-6", InputTokens: 2000, OutputTokens: 1000},
		{Model: "unknown-model", InputTokens: 500, OutputTokens: 200},
	}

	total, unpriced := pc.ComputeCost(summaries)

	// Only sonnet should be priced.
	expectedTotal := (1000*3.0 + 500*15.0) / 1_000_000
	if math.Abs(total-expectedTotal) > 0.0001 {
		t.Fatalf("total = %f, want %f", total, expectedTotal)
	}

	if len(unpriced) != 2 {
		t.Fatalf("expected 2 unpriced models, got %v", unpriced)
	}
	// Sorted alphabetically.
	if unpriced[0] != "claude-opus-4-6" {
		t.Fatalf("unpriced[0] = %q, want claude-opus-4-6", unpriced[0])
	}
	if unpriced[1] != "unknown-model" {
		t.Fatalf("unpriced[1] = %q, want unknown-model", unpriced[1])
	}
}

func TestComputeCostEmptyInput(t *testing.T) {
	pc := PricingConfig{
		"claude-sonnet-4-6": {Input: 3.0, Output: 15.0},
	}

	total, unpriced := pc.ComputeCost(nil)
	if total != 0 {
		t.Fatalf("total = %f, want 0", total)
	}
	if len(unpriced) != 0 {
		t.Fatalf("expected 0 unpriced, got %v", unpriced)
	}

	// Also with empty slice.
	total, unpriced = pc.ComputeCost([]TokenSummary{})
	if total != 0 {
		t.Fatalf("total = %f, want 0", total)
	}
	if len(unpriced) != 0 {
		t.Fatalf("expected 0 unpriced, got %v", unpriced)
	}
}

func TestComputeCostEmptyPricing(t *testing.T) {
	pc := PricingConfig{}

	summaries := []TokenSummary{
		{Model: "claude-sonnet-4-6", InputTokens: 1000, OutputTokens: 500},
		{Model: "claude-opus-4-6", InputTokens: 2000, OutputTokens: 1000},
	}

	total, unpriced := pc.ComputeCost(summaries)
	if total != 0 {
		t.Fatalf("total = %f, want 0", total)
	}
	if len(unpriced) != 2 {
		t.Fatalf("expected 2 unpriced, got %v", unpriced)
	}
}

func TestComputeCostDeduplicatesUnpriced(t *testing.T) {
	pc := PricingConfig{}

	// Same model appears in multiple summaries — should only appear once in unpriced.
	summaries := []TokenSummary{
		{Model: "unknown", InputTokens: 100},
		{Model: "unknown", InputTokens: 200},
	}

	_, unpriced := pc.ComputeCost(summaries)
	if len(unpriced) != 1 {
		t.Fatalf("expected 1 unique unpriced model, got %v", unpriced)
	}
	if unpriced[0] != "unknown" {
		t.Fatalf("unpriced[0] = %q, want unknown", unpriced[0])
	}
}
