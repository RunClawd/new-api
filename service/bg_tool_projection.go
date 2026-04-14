package service

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// CapabilityNameToToolName converts dot-notation capability to underscore tool name.
//
//	"bg.llm.chat.standard" → "bg_llm_chat_standard"
func CapabilityNameToToolName(capName string) string {
	return strings.ReplaceAll(capName, ".", "_")
}

// ToolNameToCapabilityName converts underscore tool name back to dot-notation.
//
//	"bg_llm_chat_standard" → "bg.llm.chat.standard"
func ToolNameToCapabilityName(toolName string) string {
	return strings.ReplaceAll(toolName, "_", ".")
}

// ProjectCapabilitiesToTools converts active capabilities (with schema) into
// OpenAI function-calling compatible tool definitions.
// Capabilities without InputSchemaJSON are silently skipped.
func ProjectCapabilitiesToTools(caps []*model.BgCapability) []dto.ToolDefinition {
	var tools []dto.ToolDefinition
	for _, cap := range caps {
		if cap.InputSchemaJSON == "" {
			continue
		}
		var params interface{}
		if err := common.Unmarshal([]byte(cap.InputSchemaJSON), &params); err != nil {
			continue
		}

		desc := cap.Description
		if desc == "" {
			desc = fmt.Sprintf("%s %s (%s tier)", cap.Domain, cap.Action, cap.Tier)
		}
		desc += fmt.Sprintf(" [modes: %s, billing: %s]", cap.SupportedModes, cap.BillableUnit)

		tools = append(tools, dto.ToolDefinition{
			Type: "function",
			Function: dto.ToolFunctionSchema{
				Name:        CapabilityNameToToolName(cap.CapabilityName),
				Description: desc,
				Parameters:  params,
			},
		})
	}
	return tools
}
