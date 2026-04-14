package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// ListTools handles GET /v1/bg/tools
// Returns OpenAI-compatible tool definitions for all active capabilities with schema.
func ListTools(c *gin.Context) {
	caps, err := model.GetActiveBgCapabilities()
	if err != nil {
		writeBGError(c, err)
		return
	}
	tools := service.ProjectCapabilitiesToTools(caps)
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   tools,
	})
}

// ExecuteTool handles POST /v1/bg/tools/execute
// Converts a tool call into a BaseGate response dispatch.
func ExecuteTool(c *gin.Context) {
	var req dto.ToolExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "invalid_request",
				"message": "Invalid request body: " + err.Error(),
			},
		})
		return
	}

	capabilityName := service.ToolNameToCapabilityName(req.Name)

	mode := req.Mode
	if mode == "" {
		mode = "sync"
	}

	bgReq := &dto.BaseGateRequest{
		Model: capabilityName,
		Input: req.Arguments,
		ExecutionOptions: &dto.BGExecutionOptions{
			Mode: mode,
		},
		Metadata: req.Metadata,
	}

	dispatchBaseGateRequest(c, bgReq)
}
