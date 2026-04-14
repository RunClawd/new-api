package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Name conversion tests
// ---------------------------------------------------------------------------

func TestCapabilityNameToToolName(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"bg.llm.chat.standard", "bg_llm_chat_standard"},
		{"bg.video.generate.pro", "bg_video_generate_pro"},
		{"bg.sandbox.python", "bg_sandbox_python"},
		{"", ""},
		{"nodots", "nodots"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, CapabilityNameToToolName(tt.input))
	}
}

func TestToolNameToCapabilityName(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"bg_llm_chat_standard", "bg.llm.chat.standard"},
		{"bg_video_generate_pro", "bg.video.generate.pro"},
		{"", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, ToolNameToCapabilityName(tt.input))
	}
}

func TestNameConversionRoundTrip(t *testing.T) {
	names := []string{
		"bg.llm.chat.standard",
		"bg.video.generate.pro",
		"bg.sandbox.session.standard",
	}
	for _, name := range names {
		tool := CapabilityNameToToolName(name)
		back := ToolNameToCapabilityName(tool)
		assert.Equal(t, name, back, "round-trip should be lossless for %s", name)
	}
}

// ---------------------------------------------------------------------------
// Projection tests
// ---------------------------------------------------------------------------

func TestProjectCapabilitiesToTools_WithSchema(t *testing.T) {
	caps := []*model.BgCapability{
		{
			CapabilityName:  "bg.llm.chat.standard",
			Domain:          "llm",
			Action:          "chat",
			Tier:            "standard",
			SupportedModes:  "sync,stream",
			BillableUnit:    "token",
			Description:     "Standard LLM chat",
			InputSchemaJSON: `{"type":"object","properties":{"messages":{"type":"array"}},"required":["messages"]}`,
		},
	}

	tools := ProjectCapabilitiesToTools(caps)
	require.Len(t, tools, 1)
	assert.Equal(t, "function", tools[0].Type)
	assert.Equal(t, "bg_llm_chat_standard", tools[0].Function.Name)
	assert.Contains(t, tools[0].Function.Description, "Standard LLM chat")
	assert.Contains(t, tools[0].Function.Description, "sync,stream")
	assert.NotNil(t, tools[0].Function.Parameters)
}

func TestProjectCapabilitiesToTools_WithoutSchema(t *testing.T) {
	caps := []*model.BgCapability{
		{
			CapabilityName:  "bg.llm.chat.fast",
			InputSchemaJSON: "", // no schema
		},
	}

	tools := ProjectCapabilitiesToTools(caps)
	assert.Len(t, tools, 0, "capabilities without schema should be skipped")
}

func TestProjectCapabilitiesToTools_InvalidSchema(t *testing.T) {
	caps := []*model.BgCapability{
		{
			CapabilityName:  "bg.llm.chat.fast",
			InputSchemaJSON: "{invalid json",
		},
	}

	tools := ProjectCapabilitiesToTools(caps)
	assert.Len(t, tools, 0, "capabilities with invalid schema should be skipped")
}

func TestProjectCapabilitiesToTools_FallbackDescription(t *testing.T) {
	caps := []*model.BgCapability{
		{
			CapabilityName:  "bg.video.generate.standard",
			Domain:          "video",
			Action:          "generate",
			Tier:            "standard",
			SupportedModes:  "async",
			BillableUnit:    "second",
			Description:     "", // empty description
			InputSchemaJSON: `{"type":"object","properties":{"prompt":{"type":"string"}},"required":["prompt"]}`,
		},
	}

	tools := ProjectCapabilitiesToTools(caps)
	require.Len(t, tools, 1)
	assert.Contains(t, tools[0].Function.Description, "video generate (standard tier)")
}
