package config

import (
	"fmt"
	"os"
	"sort"

	"github.com/BurntSushi/toml"
)

// PricingModel holds per-model token pricing in dollars per million tokens.
type PricingModel struct {
	Input         float64 `toml:"input"`          // dollars per million input tokens
	Output        float64 `toml:"output"`         // dollars per million output tokens
	CacheRead     float64 `toml:"cache_read"`     // dollars per million cache read tokens
	CacheCreation float64 `toml:"cache_creation"` // dollars per million cache creation tokens
}

// PricingConfig maps model names to their pricing. Keyed by model name
// as it appears in token_usage records.
type PricingConfig map[string]PricingModel

// TokenSummary holds aggregated token counts for a single model.
// This mirrors store.TokenSummary to avoid an import cycle between
// config and store packages.
type TokenSummary struct {
	Model               string
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

// solTOMLWithPricing is used to decode the [pricing] section from sol.toml.
type solTOMLWithPricing struct {
	Pricing PricingConfig `toml:"pricing"`
}

// LoadPricing reads the [pricing] section from $SOL_HOME/sol.toml.
// Returns an empty PricingConfig (not nil) if the file or section is missing.
func LoadPricing() (PricingConfig, error) {
	path := GlobalConfigPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return PricingConfig{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to check %s: %w", path, err)
	}

	var cfg solTOMLWithPricing
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}

	if cfg.Pricing == nil {
		return PricingConfig{}, nil
	}
	return cfg.Pricing, nil
}

// ComputeCost converts token summaries to a dollar amount using configured
// pricing. Returns the total cost and a sorted list of model names that had
// no pricing entry (unpriced). Models without pricing are not counted toward
// the total — they appear only in the unpriced slice.
//
// Cost formula per model:
//
//	(input * pricing.input + output * pricing.output +
//	 cache_read * pricing.cache_read + cache_creation * pricing.cache_creation) / 1_000_000
func (pc PricingConfig) ComputeCost(summaries []TokenSummary) (totalDollars float64, unpriced []string) {
	seen := make(map[string]bool)
	for _, ts := range summaries {
		pricing, ok := pc[ts.Model]
		if !ok {
			if !seen[ts.Model] {
				unpriced = append(unpriced, ts.Model)
				seen[ts.Model] = true
			}
			continue
		}
		totalDollars += (float64(ts.InputTokens)*pricing.Input +
			float64(ts.OutputTokens)*pricing.Output +
			float64(ts.CacheReadTokens)*pricing.CacheRead +
			float64(ts.CacheCreationTokens)*pricing.CacheCreation) / 1_000_000
	}
	sort.Strings(unpriced)
	return totalDollars, unpriced
}
