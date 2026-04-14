package controller

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// PostResponses handles POST /v1/responses
func PostResponses(c *gin.Context) {
	var req dto.BaseGateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "invalid_request",
				"message": "Invalid request body: " + err.Error(),
			},
		})
		return
	}

	if req.Input == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "invalid_request",
				"message": "Key: 'BaseGateRequest.Input' Error:Field validation for 'Input' failed on the 'required' tag",
			},
		})
		return
	}

	dispatchBaseGateRequest(c, &req)
}

// dispatchBaseGateRequest is the shared dispatch logic used by both
// PostResponses (HTTP JSON body) and ExecuteTool (tool call conversion).
func dispatchBaseGateRequest(c *gin.Context, req *dto.BaseGateRequest) {
	projectID, projErr := resolveProjectID(c)
	if projErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "invalid_project",
				"message": projErr.Error(),
			},
		})
		return
	}

	// Build canonical request
	canonicalReq := &relaycommon.CanonicalRequest{
		RequestID:  relaycommon.GenerateResponseID(),
		ResponseID: relaycommon.GenerateResponseID(),
		Model:      req.Model,
		OrgID:      c.GetInt("id"),
		ProjectID:  projectID,
		ApiKeyID:   c.GetInt("token_id"),
		EndUserID:  req.Metadata["user_id"],
		Input:      req.Input,
		Metadata:   req.Metadata,
	}

	// Idempotency-Key from header
	if idemKey := c.GetHeader("Idempotency-Key"); idemKey != "" {
		canonicalReq.IdempotencyKey = idemKey
	}

	// Execution options
	if req.ExecutionOptions != nil {
		canonicalReq.ExecutionOptions = relaycommon.ExecutionOptions{
			Mode:       req.ExecutionOptions.Mode,
			WebhookURL: req.ExecutionOptions.WebhookURL,
			TimeoutMs:  req.ExecutionOptions.TimeoutMs,
		}
	}

	// Default billing context
	canonicalReq.BillingContext = relaycommon.BillingContext{
		BillingSource: "hosted",
	}

	// Capability policy check
	allowed, reason, policyErr := service.EvaluateCapabilityAccess(
		canonicalReq.OrgID, canonicalReq.ProjectID, canonicalReq.ApiKeyID, canonicalReq.Model)
	if policyErr != nil {
		common.SysError("dispatchBaseGateRequest policy evaluation failed: " + policyErr.Error())
		writeBGError(c, policyErr)
		return
	}
	if !allowed {
		_ = model.RecordBgAuditLog(canonicalReq.OrgID, canonicalReq.RequestID, "", "capability_denied", map[string]interface{}{
			"model":  req.Model,
			"reason": reason,
		})
		writeBGError(c, fmt.Errorf("%w: %s", service.ErrCapabilityDenied, reason))
		return
	}

	// Dispatch based on mode
	mode := "sync"
	if canonicalReq.ExecutionOptions.Mode != "" {
		mode = canonicalReq.ExecutionOptions.Mode
	}

	var resp *dto.BaseGateResponse
	var err error

	switch mode {
	case "async":
		resp, err = service.DispatchAsync(canonicalReq)
	case "stream":
		err = service.DispatchStream(canonicalReq, c)
		if err != nil {
			common.SysError("dispatchBaseGateRequest stream error: " + err.Error())
			if !c.Writer.Written() {
				writeBGError(c, err)
			}
		}
		return
	default:
		resp, err = service.DispatchSync(canonicalReq)
	}

	if err != nil {
		common.SysError("dispatchBaseGateRequest error: " + err.Error())
		writeBGError(c, err)
		return
	}

	statusCode := http.StatusOK
	if !model.BgResponseStatus(resp.Status).IsTerminal() {
		statusCode = http.StatusAccepted
	}

	c.JSON(statusCode, resp)
}

// GetResponseByID handles GET /v1/responses/:id
func GetResponseByID(c *gin.Context) {
	responseID := c.Param("id")
	if responseID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "invalid_request",
				"message": "response_id is required",
			},
		})
		return
	}

	orgID := c.GetInt("id")
	resp, err := service.GetResponse(responseID, orgID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"code":    "not_found",
				"message": "Response not found: " + responseID,
			},
		})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// CancelResponseByID handles POST /v1/responses/:id/cancel
func CancelResponseByID(c *gin.Context) {
	responseID := c.Param("id")
	if responseID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "invalid_request",
				"message": "response_id is required",
			},
		})
		return
	}

	orgID := c.GetInt("id")
	resp, err := service.CancelResponse(responseID, orgID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"code":    "not_found",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, resp)
}
