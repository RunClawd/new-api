package controller

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common" // Contains CanonicalRequest
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// PostSessions handles POST /v1/bg/sessions
func PostSessions(c *gin.Context) {
	// Parse input
	var basegateReq dto.BaseGateRequest
	if err := c.ShouldBindJSON(&basegateReq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "invalid_request",
				"message": err.Error(),
			},
		})
		return
	}

	// Read auth bounds (would normally come from middleware check)
	orgID := c.GetInt("id") // Tenant ID
	projectID, _ := strconv.Atoi(c.GetHeader("X-Project-Id"))
	apiKeyID := c.GetInt("token_id")

	// Construct CanonicalRequest
	canonicalReq := &relaycommon.CanonicalRequest{
		RequestID:  relaycommon.GenerateResponseID(),
		ResponseID: relaycommon.GenerateResponseID(),
		Model:      basegateReq.Model,
		Input:      basegateReq.Input,
		OrgID:      orgID,
		ProjectID:  projectID,
		ApiKeyID:   apiKeyID,
		EndUserID:  basegateReq.Metadata["user_id"],
		Metadata:   basegateReq.Metadata,
	}

	// Capability policy check — error = engine failure (500), !allowed = policy deny (403)
	allowed, reason, policyErr := service.EvaluateCapabilityAccess(orgID, projectID, apiKeyID, basegateReq.Model)
	if policyErr != nil {
		common.SysError("PostSessions policy evaluation failed: " + policyErr.Error())
		writeBGError(c, policyErr) // no sentinel match -> defaults to 500 internal_error
		return
	}
	if !allowed {
		writeBGError(c, fmt.Errorf("%w: %s", service.ErrCapabilityDenied, reason))
		return
	}

	sessionResp, err := service.CreateSession(canonicalReq)
	if err != nil {
		writeBGError(c, err)
		return
	}

	c.JSON(http.StatusCreated, sessionResp)
}

// GetSessionByID handles GET /v1/bg/sessions/:id
func GetSessionByID(c *gin.Context) {
	sessionID := c.Param("id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": "missing session id"},
		})
		return
	}

	sessionResp, err := service.GetSession(sessionID)
	if err != nil {
		writeBGError(c, err)
		return
	}

	c.JSON(http.StatusOK, sessionResp)
}

// PostSessionAction handles POST /v1/bg/sessions/:id/action
func PostSessionAction(c *gin.Context) {
	sessionID := c.Param("id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": "missing session id"},
		})
		return
	}

	var actionReq dto.BGSessionActionRequest
	if err := c.ShouldBindJSON(&actionReq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": err.Error()},
		})
		return
	}

	actionResp, err := service.ExecuteSessionAction(sessionID, &actionReq)
	if err != nil {
		writeBGError(c, err)
		return
	}

	c.JSON(http.StatusOK, actionResp)
}

// CloseSessionByID handles POST /v1/bg/sessions/:id/close
func CloseSessionByID(c *gin.Context) {
	sessionID := c.Param("id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": "missing session id"},
		})
		return
	}

	sessionResp, err := service.CloseSession(sessionID)
	if err != nil {
		writeBGError(c, err)
		return
	}

	c.JSON(http.StatusOK, sessionResp)
}
