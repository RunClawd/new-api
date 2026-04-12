package service

import (
	"math"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// --- calculateDifferentiatedAmount tests ---

func TestCalculateDifferentiatedAmount_FullDifferentiated(t *testing.T) {
	// Scenario: 800 input + 200 cached + 100 cache_creation + 500 output
	// completionRatio=4, cacheRatio=0.1, cacheCreationRatio=1.25
	// unitPrice = 0.000002 (model_ratio=1)
	pricing := &relaycommon.PricingSnapshot{
		PricingMode:        "metered",
		BillableUnit:       "token",
		UnitPrice:          0.000002,
		Currency:           "usd",
		CompletionRatio:    4,
		CacheRatio:         0.1,
		CacheCreationRatio: 1.25,
	}

	raw := &relaycommon.ProviderUsage{
		PromptTokens:        1100, // 800 input + 200 cached + 100 cache_creation
		CompletionTokens:    500,
		TotalTokens:         1600,
		InputTokens:         800,
		CachedTokens:        200,
		CacheCreationTokens: 100,
	}

	canonical := buildCanonicalUsage(raw)

	amount := calculateDifferentiatedAmount(pricing, canonical)

	// Expected:
	// inputCost      = 800 * 0.000002 = 0.0016
	// outputCost     = 500 * 0.000002 * 4 = 0.004
	// cacheHitCost   = 200 * 0.000002 * 0.1 = 0.00004
	// cacheWriteCost = 100 * 0.000002 * 1.25 = 0.00025
	// total = 0.0016 + 0.004 + 0.00004 + 0.00025 = 0.00589
	expected := 0.00589
	if math.Abs(amount-expected) > 1e-10 {
		t.Errorf("expected amount %.10f, got %.10f", expected, amount)
	}
}

func TestCalculateDifferentiatedAmount_NoCacheTokens(t *testing.T) {
	// When no cache tokens, should degrade to: inputTokens*price + outputTokens*price*completionRatio
	pricing := &relaycommon.PricingSnapshot{
		PricingMode:     "metered",
		BillableUnit:    "token",
		UnitPrice:       0.000005, // gpt-4o: model_ratio=1.25
		Currency:        "usd",
		CompletionRatio: 4,
		CacheRatio:      0.5,
	}

	raw := &relaycommon.ProviderUsage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
		InputTokens:      1000,
	}

	canonical := buildCanonicalUsage(raw)
	amount := calculateDifferentiatedAmount(pricing, canonical)

	// inputCost  = 1000 * 0.000005 = 0.005
	// outputCost = 500 * 0.000005 * 4 = 0.01
	// total = 0.015
	expected := 0.015
	if math.Abs(amount-expected) > 1e-10 {
		t.Errorf("expected amount %.10f, got %.10f", expected, amount)
	}
}

func TestCalculateDifferentiatedAmount_PerCallModel(t *testing.T) {
	// per_call models (dall-e etc.) shouldn't use differentiated pricing
	pricing := &relaycommon.PricingSnapshot{
		PricingMode:     "per_call",
		BillableUnit:    "request",
		UnitPrice:       0.04,
		Currency:        "usd",
		CompletionRatio: 1,
		CacheRatio:      1,
	}

	raw := &relaycommon.ProviderUsage{
		BillableUnits: 1,
		BillableUnit:  "request",
	}

	canonical := buildCanonicalUsage(raw)
	amount := calculateDifferentiatedAmount(pricing, canonical)

	if math.Abs(amount-0.04) > 1e-10 {
		t.Errorf("expected 0.04, got %.10f", amount)
	}
}

func TestCalculateDifferentiatedAmount_ClaudeSplitCacheCreation(t *testing.T) {
	// Claude 5m/1h split cache creation pricing
	pricing := &relaycommon.PricingSnapshot{
		PricingMode:          "metered",
		BillableUnit:         "token",
		UnitPrice:            0.000003, // claude-sonnet input price
		Currency:             "usd",
		CompletionRatio:      5,
		CacheRatio:           0.1,
		CacheCreationRatio:   1.25,
		CacheCreation5mRatio: 1.25,
		CacheCreation1hRatio: 2.0,
	}

	raw := &relaycommon.ProviderUsage{
		PromptTokens:          2000,
		CompletionTokens:      300,
		TotalTokens:           2300,
		InputTokens:           1200,
		CachedTokens:          400,
		CacheCreationTokens:   400, // total = 150 (5m) + 250 (1h)
		CacheCreationTokens5m: 150,
		CacheCreationTokens1h: 250,
	}

	canonical := buildCanonicalUsage(raw)
	amount := calculateDifferentiatedAmount(pricing, canonical)

	// remaining cache creation = 400 - 150 - 250 = 0
	// inputCost       = 1200 * 0.000003 = 0.0036
	// outputCost      = 300 * 0.000003 * 5 = 0.0045
	// cacheHitCost    = 400 * 0.000003 * 0.1 = 0.00012
	// cacheWriteCost  = 0 * ... = 0
	// cacheWrite5m    = 150 * 0.000003 * 1.25 = 0.0005625
	// cacheWrite1h    = 250 * 0.000003 * 2.0 = 0.0015
	// total = 0.0036 + 0.0045 + 0.00012 + 0 + 0.0005625 + 0.0015 = 0.0102825
	expected := 0.0102825
	if math.Abs(amount-expected) > 1e-10 {
		t.Errorf("expected %.10f, got %.10f", expected, amount)
	}
}

func TestCalculateDifferentiatedAmount_BYOPercentageBase(t *testing.T) {
	// BYO percentage mode: the base amount should be the differentiated total
	pricing := &relaycommon.PricingSnapshot{
		PricingMode:     "metered",
		BillableUnit:    "token",
		UnitPrice:       0.000002,
		Currency:        "usd",
		CompletionRatio: 4,
		CacheRatio:      0.1,
	}

	raw := &relaycommon.ProviderUsage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
		InputTokens:      800,
		CachedTokens:     200,
	}

	canonical := buildCanonicalUsage(raw)
	baseAmount := calculateDifferentiatedAmount(pricing, canonical)

	// Applying 10% BYO fee
	byoFee := baseAmount * 0.10

	// inputCost     = 800 * 0.000002 = 0.0016
	// outputCost    = 500 * 0.000002 * 4 = 0.004
	// cacheHitCost  = 200 * 0.000002 * 0.1 = 0.00004
	// baseAmount    = 0.00564
	// byoFee        = 0.000564
	expectedBase := 0.00564
	expectedFee := 0.000564
	if math.Abs(baseAmount-expectedBase) > 1e-10 {
		t.Errorf("expected base %.10f, got %.10f", expectedBase, baseAmount)
	}
	if math.Abs(byoFee-expectedFee) > 1e-10 {
		t.Errorf("expected BYO fee %.10f, got %.10f", expectedFee, byoFee)
	}
}

// --- normalizePricingSnapshot tests ---

func TestNormalizePricingSnapshot_BackwardCompatibility(t *testing.T) {
	// Old snapshot with zero ratio fields should get safe defaults
	old := &relaycommon.PricingSnapshot{
		PricingMode:  "metered",
		BillableUnit: "token",
		UnitPrice:    0.000002,
		Currency:     "usd",
		// All ratio fields are zero (old JSON)
	}

	p := normalizePricingSnapshot(old)

	if p.CompletionRatio != 1 {
		t.Errorf("expected CompletionRatio=1, got %f", p.CompletionRatio)
	}
	if p.CacheRatio != 1 {
		t.Errorf("expected CacheRatio=1, got %f", p.CacheRatio)
	}
	if p.CacheCreationRatio != 1.25 {
		t.Errorf("expected CacheCreationRatio=1.25, got %f", p.CacheCreationRatio)
	}
	if p.CacheCreation5mRatio != 1.25 {
		t.Errorf("expected CacheCreation5mRatio=1.25, got %f", p.CacheCreation5mRatio)
	}
	if p.CacheCreation1hRatio != 1.25 {
		t.Errorf("expected CacheCreation1hRatio=1.25, got %f", p.CacheCreation1hRatio)
	}
}

func TestNormalizePricingSnapshot_ExplicitRatios(t *testing.T) {
	// Explicitly set ratios should pass through unchanged
	snap := &relaycommon.PricingSnapshot{
		PricingMode:          "metered",
		BillableUnit:         "token",
		UnitPrice:            0.000003,
		Currency:             "usd",
		CompletionRatio:      5,
		CacheRatio:           0.1,
		CacheCreationRatio:   1.5,
		CacheCreation5mRatio: 1.5,
		CacheCreation1hRatio: 2.5,
	}

	p := normalizePricingSnapshot(snap)

	if p.CompletionRatio != 5 {
		t.Errorf("expected 5, got %f", p.CompletionRatio)
	}
	if p.CacheRatio != 0.1 {
		t.Errorf("expected 0.1, got %f", p.CacheRatio)
	}
	if p.CacheCreation1hRatio != 2.5 {
		t.Errorf("expected 2.5, got %f", p.CacheCreation1hRatio)
	}
}

func TestCalculateDifferentiatedAmount_FallbackPromptTokens(t *testing.T) {
	// When InputTokens=0 but PromptTokens>0, should fall back to PromptTokens
	pricing := &relaycommon.PricingSnapshot{
		PricingMode:     "metered",
		BillableUnit:    "token",
		UnitPrice:       0.000002,
		Currency:        "usd",
		CompletionRatio: 4,
		CacheRatio:      1, // no special cache ratio
	}

	raw := &relaycommon.ProviderUsage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
		// InputTokens is 0 — old adapter that didn't set it
	}

	canonical := buildCanonicalUsage(raw)
	amount := calculateDifferentiatedAmount(pricing, canonical)

	// Falls back: inputTokens=1000 (from PromptTokens)
	// inputCost  = 1000 * 0.000002 = 0.002
	// outputCost = 500 * 0.000002 * 4 = 0.004
	// total = 0.006
	expected := 0.006
	if math.Abs(amount-expected) > 1e-10 {
		t.Errorf("expected %.10f, got %.10f", expected, amount)
	}
}

func TestCalculateDifferentiatedAmount_NilPricing(t *testing.T) {
	raw := &relaycommon.ProviderUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}
	canonical := buildCanonicalUsage(raw)

	if amount := calculateDifferentiatedAmount(nil, canonical); amount != 0 {
		t.Errorf("expected 0, got %f", amount)
	}
}

func TestCalculateDifferentiatedAmount_ZeroUnitPrice(t *testing.T) {
	pricing := &relaycommon.PricingSnapshot{
		PricingMode:  "metered",
		BillableUnit: "token",
		UnitPrice:    0,
		Currency:     "usd",
	}

	raw := &relaycommon.ProviderUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		InputTokens:      100,
	}
	canonical := buildCanonicalUsage(raw)

	if amount := calculateDifferentiatedAmount(pricing, canonical); amount != 0 {
		t.Errorf("expected 0, got %f", amount)
	}
}

func TestCalculateDifferentiatedAmount_NonTokenUsage(t *testing.T) {
	// Second-based billing — should use simple multiply
	pricing := &relaycommon.PricingSnapshot{
		PricingMode:  "metered",
		BillableUnit: "second",
		UnitPrice:    0.001,
		Currency:     "usd",
	}

	raw := &relaycommon.ProviderUsage{
		DurationSec: 120.5,
	}

	canonical := buildCanonicalUsage(raw)
	amount := calculateDifferentiatedAmount(pricing, canonical)

	expected := 120.5 * 0.001
	if math.Abs(amount-expected) > 1e-10 {
		t.Errorf("expected %.10f, got %.10f", expected, amount)
	}
}

func TestLookupPricing_CacheCreation1hRatio_NotEqualToBase(t *testing.T) {
	// LookupPricing must apply the 1h multiplier (6/3.75 = 1.6) to the base
	// cache creation ratio, not just copy it. This aligns with the legacy formula
	// in relay/helper/price.go:claudeCacheCreation1hMultiplier.
	ratio_setting.InitRatioSettings()
	snap := LookupPricing("claude-sonnet-4-5-20250929", "hosted")
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}

	// Claude base cache creation ratio = 1.25 (from ratio_setting defaults)
	// 5m ratio should equal base: 1.25
	// 1h ratio should equal base × 1.6 = 2.0
	const expected1h = 1.25 * (6.0 / 3.75) // = 2.0

	if snap.CacheCreation5mRatio != snap.CacheCreationRatio {
		t.Errorf("5m ratio should equal base creation ratio: got %f, want %f",
			snap.CacheCreation5mRatio, snap.CacheCreationRatio)
	}

	if math.Abs(snap.CacheCreation1hRatio-expected1h) > 1e-10 {
		t.Errorf("1h ratio should be base × 1.6: got %f, want %f",
			snap.CacheCreation1hRatio, expected1h)
	}

	if snap.CacheCreation1hRatio == snap.CacheCreationRatio {
		t.Errorf("1h ratio must NOT equal base ratio (was: %f); it should be %f",
			snap.CacheCreation1hRatio, expected1h)
	}
}
