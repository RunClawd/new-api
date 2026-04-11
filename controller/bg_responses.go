package controller

import (
	"fmt"
	"net/http"
	"strconv"

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

	projectID, _ := strconv.Atoi(c.GetHeader("X-Project-Id"))

	// Build canonical request
	canonicalReq := &relaycommon.CanonicalRequest{
		RequestID:  relaycommon.GenerateResponseID(), // reuse generator for request IDs
		ResponseID: relaycommon.GenerateResponseID(),
		Model:      req.Model,
		OrgID:      c.GetInt("id"), // Context "id" is the user/tenant ID set by TokenAuth
		ProjectID:  projectID,
		ApiKeyID:   c.GetInt("token_id"),
		EndUserID:  req.Metadata["user_id"], // Optional user tracking alias
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
		BillingMode: "hosted",
	}

	// Capability policy check — before mode dispatch, covers sync/async/stream
	allowed, reason, policyErr := service.EvaluateCapabilityAccess(
		canonicalReq.OrgID, canonicalReq.ProjectID, canonicalReq.ApiKeyID, canonicalReq.Model)
	if policyErr != nil {
		common.SysError("PostResponses policy evaluation failed: " + policyErr.Error())
		writeBGError(c, policyErr) // no sentinel match -> defaults to 500 internal_error
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
			common.SysError("PostResponses stream error: " + err.Error())
			// DispatchStream already writes header if it started streaming, but if it errored before, we return JSON.
			if !c.Writer.Written() {
				writeBGError(c, err)
			}
		}
		return // Stream handled entirely within DispatchStream
	default: // sync
		resp, err = service.DispatchSync(canonicalReq)
	}

	if err != nil {
		common.SysError("PostResponses error: " + err.Error())
		writeBGError(c, err)
		return
	}

	// Set appropriate HTTP status
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

	resp, err := service.GetResponse(responseID)
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

	resp, err := service.CancelResponse(responseID)
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
