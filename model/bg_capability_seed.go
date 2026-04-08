package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm/clause"
)

// SeedBgCapabilities performs an idempotent upsert of the initial capability definitions.
// Called once after AutoMigrate in InitDB() — safe to call on every startup.
// Uses GORM's OnConflict clause for cross-DB compatibility (SQLite, MySQL, PostgreSQL).
func SeedBgCapabilities() error {
	caps := []BgCapability{
		{
			CapabilityName: "bg.llm.chat.fast",
			Domain:         "llm",
			Action:         "chat",
			Tier:           "fast",
			BillableUnit:   "token",
			SupportedModes: "sync,stream",
			SupportsCancel: true,
			Status:         "active",
			Description:    "Low-latency LLM chat (fast tier). Maps to small, high-throughput models.",
		},
		{
			CapabilityName: "bg.llm.chat.standard",
			Domain:         "llm",
			Action:         "chat",
			Tier:           "standard",
			BillableUnit:   "token",
			SupportedModes: "sync,stream",
			SupportsCancel: true,
			Status:         "active",
			Description:    "Standard LLM chat. Balanced quality and cost.",
		},
		{
			CapabilityName: "bg.llm.chat.pro",
			Domain:         "llm",
			Action:         "chat",
			Tier:           "pro",
			BillableUnit:   "token",
			SupportedModes: "sync,stream",
			SupportsCancel: true,
			Status:         "active",
			Description:    "High-capability LLM chat for complex reasoning and long context.",
		},
		{
			CapabilityName: "bg.llm.reasoning.pro",
			Domain:         "llm",
			Action:         "reasoning",
			Tier:           "pro",
			BillableUnit:   "token",
			SupportedModes: "sync,stream",
			SupportsCancel: true,
			Status:         "active",
			Description:    "Extended thinking / reasoning models. Higher cost, best accuracy.",
		},
		{
			CapabilityName: "bg.video.upscale.standard",
			Domain:         "video",
			Action:         "upscale",
			Tier:           "standard",
			BillableUnit:   "second",
			SupportedModes: "async",
			SupportsCancel: false,
			Status:         "active",
			Description:    "Async video upscaling. Billed per second of processed video.",
		},
		{
			CapabilityName: "bg.sandbox.python",
			Domain:         "sandbox",
			Action:         "python",
			Tier:           "standard",
			BillableUnit:   "action",
			SupportedModes: "sync",
			SupportsCancel: false,
			Status:         "active",
			Description:    "Python code execution sandbox. Billed per execution action.",
		},
		{
			CapabilityName: "bg.sandbox.session.standard",
			Domain:         "sandbox",
			Action:         "session",
			Tier:           "standard",
			BillableUnit:   "minute",
			SupportedModes: "session",
			SupportsCancel: false,
			Status:         "active",
			Description:    "Interactive sandbox session (browser/code). Billed per minute.",
		},
		{
			CapabilityName: "bg.video.generate.standard",
			Domain:         "video",
			Action:         "generate",
			Tier:           "standard",
			BillableUnit:   "second",
			SupportedModes: "async",
			SupportsCancel: false,
			Status:         "active",
			Description:    "Async video generation (standard tier). Billed per second of output video.",
		},
		{
			CapabilityName: "bg.video.generate.pro",
			Domain:         "video",
			Action:         "generate",
			Tier:           "pro",
			BillableUnit:   "second",
			SupportedModes: "async",
			SupportsCancel: false,
			Status:         "active",
			Description:    "Async video generation (pro tier). Higher quality model, billed per second.",
		},
	}

	// Use GORM OnConflict for cross-DB upsert.
	// Conflict key: capability_name (unique index).
	// On conflict: update all descriptor fields but NOT created_at.
	if err := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "capability_name"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"domain", "action", "tier", "billable_unit",
			"supported_modes", "supports_cancel", "status", "description",
		}),
	}).Create(&caps).Error; err != nil {
		return err
	}

	// Seed default pricing into the existing ratio_setting system.
	// Only writes if the model name is NOT already configured (user overrides preserved).
	seedBgDefaultPricing()
	return nil
}

// seedBgDefaultPricing writes default prices for BaseGate capabilities into
// the in-memory ratio/price maps. These are picked up by LookupPricing().
// Only sets values for models that don't already have a price or ratio configured.
func seedBgDefaultPricing() {
	// Ratio-based models (LLM): value is the ratio multiplier (1.0 ≈ $0.002/1K tokens)
	ratioDefaults := map[string]float64{
		"bg.llm.chat.fast":      0.5,
		"bg.llm.chat.standard":  2.0,
		"bg.llm.chat.pro":       5.0,
		"bg.llm.reasoning.pro":  10.0,
	}

	// Price-based models (Video/Sandbox): value is direct $/request
	priceDefaults := map[string]float64{
		"bg.video.generate.standard": 0.05,
		"bg.video.generate.pro":      0.10,
		"bg.video.upscale.standard":  0.03,
		"bg.sandbox.session.standard": 0.01,
		"bg.sandbox.python":          0.005,
	}

	seeded := 0
	for name, ratio := range ratioDefaults {
		if _, _, exists := ratio_setting.GetModelRatioOrPrice(name); !exists {
			ratio_setting.SetModelRatioIfNotExists(name, ratio)
			seeded++
		}
	}
	for name, price := range priceDefaults {
		if _, _, exists := ratio_setting.GetModelRatioOrPrice(name); !exists {
			ratio_setting.SetModelPriceIfNotExists(name, price)
			seeded++
		}
	}
	if seeded > 0 {
		common.SysLog(fmt.Sprintf("bg_seed: seeded default pricing for %d capabilities", seeded))
	}
}
