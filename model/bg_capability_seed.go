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

	// NOTE: Pricing seed is NOT called here because InitOptionMap() (which loads
	// ModelRatio/ModelPrice from the options table) runs AFTER migrateDB().
	// Calling seedBgDefaultPricing() here would be overwritten by InitOptionMap().
	// Instead, call SeedBgDefaultPricing() from main.go after InitOptionMap().

	// Phase 15: populate InputSchemaJSON/OutputSchemaJSON for Tool projection.
	updateSeedSchemas()

	return nil
}

// updateSeedSchemas fills InputSchemaJSON and OutputSchemaJSON for capabilities
// that don't have them yet. Called idempotently at every startup.
func updateSeedSchemas() {
	schemas := map[string][2]string{
		"bg.llm.chat.fast":              {llmChatInputSchema, llmChatOutputSchema},
		"bg.llm.chat.standard":          {llmChatInputSchema, llmChatOutputSchema},
		"bg.llm.chat.pro":               {llmChatInputSchema, llmChatOutputSchema},
		"bg.llm.reasoning.pro":          {llmReasoningInputSchema, llmReasoningOutputSchema},
		"bg.video.generate.standard":    {videoGenerateInputSchema, videoGenerateOutputSchema},
		"bg.video.generate.pro":         {videoGenerateInputSchema, videoGenerateOutputSchema},
		"bg.video.upscale.standard":     {videoUpscaleInputSchema, videoUpscaleOutputSchema},
		"bg.sandbox.python":             {sandboxPythonInputSchema, sandboxPythonOutputSchema},
		"bg.sandbox.session.standard":   {sandboxSessionInputSchema, sandboxSessionOutputSchema},
	}
	for name, s := range schemas {
		DB.Model(&BgCapability{}).
			Where("capability_name = ? AND (input_schema_json IS NULL OR input_schema_json = '')", name).
			Updates(map[string]interface{}{
				"input_schema_json":  s[0],
				"output_schema_json": s[1],
			})
	}
}

// --- JSON Schema constants for seed capabilities ---

const llmChatInputSchema = `{
  "type": "object",
  "properties": {
    "messages": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "role": {"type": "string", "enum": ["system", "user", "assistant"]},
          "content": {"type": "string"}
        },
        "required": ["role", "content"]
      },
      "minItems": 1,
      "description": "Conversation messages"
    },
    "temperature": {"type": "number", "minimum": 0, "maximum": 2, "description": "Sampling temperature"},
    "max_tokens": {"type": "integer", "minimum": 1, "description": "Maximum tokens to generate"}
  },
  "required": ["messages"]
}`

const llmChatOutputSchema = `{
  "type": "object",
  "properties": {
    "content": {"type": "string", "description": "Generated text response"}
  }
}`

const llmReasoningInputSchema = `{
  "type": "object",
  "properties": {
    "messages": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "role": {"type": "string", "enum": ["system", "user", "assistant"]},
          "content": {"type": "string"}
        },
        "required": ["role", "content"]
      },
      "minItems": 1,
      "description": "Conversation messages"
    },
    "reasoning_effort": {"type": "string", "enum": ["low", "medium", "high"], "description": "Reasoning depth control"},
    "max_tokens": {"type": "integer", "minimum": 1, "description": "Maximum tokens to generate"}
  },
  "required": ["messages"]
}`

const llmReasoningOutputSchema = `{
  "type": "object",
  "properties": {
    "content": {"type": "string", "description": "Generated text response"},
    "reasoning_content": {"type": "string", "description": "Chain-of-thought reasoning trace"}
  }
}`

const videoGenerateInputSchema = `{
  "type": "object",
  "properties": {
    "prompt": {"type": "string", "description": "Video generation prompt"},
    "duration": {"type": "integer", "minimum": 1, "maximum": 60, "description": "Duration in seconds"},
    "aspect_ratio": {"type": "string", "enum": ["16:9", "9:16", "1:1"], "description": "Output aspect ratio"}
  },
  "required": ["prompt"]
}`

const videoGenerateOutputSchema = `{
  "type": "object",
  "properties": {
    "video_url": {"type": "string", "description": "URL to generated video"},
    "duration": {"type": "number", "description": "Actual duration in seconds"}
  }
}`

const videoUpscaleInputSchema = `{
  "type": "object",
  "properties": {
    "video_url": {"type": "string", "description": "URL to source video"},
    "target_resolution": {"type": "string", "enum": ["1080p", "2k", "4k"], "description": "Target resolution"}
  },
  "required": ["video_url"]
}`

const videoUpscaleOutputSchema = `{
  "type": "object",
  "properties": {
    "video_url": {"type": "string", "description": "URL to upscaled video"},
    "resolution": {"type": "string", "description": "Actual output resolution"}
  }
}`

const sandboxPythonInputSchema = `{
  "type": "object",
  "properties": {
    "code": {"type": "string", "description": "Python code to execute"},
    "timeout_ms": {"type": "integer", "minimum": 100, "maximum": 300000, "description": "Execution timeout in milliseconds"}
  },
  "required": ["code"]
}`

const sandboxPythonOutputSchema = `{
  "type": "object",
  "properties": {
    "stdout": {"type": "string", "description": "Standard output"},
    "stderr": {"type": "string", "description": "Standard error"},
    "exit_code": {"type": "integer", "description": "Process exit code"}
  }
}`

const sandboxSessionInputSchema = `{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["execute", "upload", "download"], "description": "Session action type"},
    "code": {"type": "string", "description": "Code to execute (for execute action)"},
    "path": {"type": "string", "description": "File path (for upload/download actions)"}
  },
  "required": ["action"]
}`

const sandboxSessionOutputSchema = `{
  "type": "object",
  "properties": {
    "stdout": {"type": "string", "description": "Standard output"},
    "stderr": {"type": "string", "description": "Standard error"},
    "files": {"type": "array", "items": {"type": "string"}, "description": "List of file paths"}
  }
}`

// SeedBgDefaultPricing writes default prices for BaseGate capabilities into
// the in-memory ratio/price maps AND persists them to the options table.
// Must be called AFTER InitOptionMap() so that user-configured values are already loaded.
// Only sets values for models that don't already have a price or ratio configured.
func SeedBgDefaultPricing() {
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
		// Persist the updated maps to the options table so they survive restart.
		ratioJSON := ratio_setting.ModelRatio2JSONString()
		priceJSON := ratio_setting.ModelPrice2JSONString()
		UpdateOption("ModelRatio", ratioJSON)
		UpdateOption("ModelPrice", priceJSON)
		common.SysLog(fmt.Sprintf("bg_seed: seeded default pricing for %d capabilities (persisted to options)", seeded))
	}
}
