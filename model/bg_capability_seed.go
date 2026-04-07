package model

import (
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
	}

	// Use GORM OnConflict for cross-DB upsert.
	// Conflict key: capability_name (unique index).
	// On conflict: update all descriptor fields but NOT created_at.
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "capability_name"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"domain", "action", "tier", "billable_unit",
			"supported_modes", "supports_cancel", "status", "description",
		}),
	}).Create(&caps).Error
}
