package controller

import (
	"errors"
	"net/http"
	"strconv"

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
		ResponseID: relaycommon.GenerateResponseID(),
		Model:      basegateReq.Model,
		Input:      basegateReq.Input,
		OrgID:      orgID,
		ProjectID:  projectID,
		ApiKeyID:   apiKeyID,
		EndUserID:  basegateReq.Metadata["user_id"],
		Metadata:   basegateReq.Metadata,
	}

	sessionResp, err := service.CreateSession(canonicalReq)
	if err != nil {
		code := "internal_error"
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrSessionValidation) {
			code = "invalid_request"
			status = http.StatusBadRequest
		}

		c.JSON(status, gin.H{
			"error": gin.H{
				"code":    code,
				"message": err.Error(),
			},
		})
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
		status := http.StatusInternalServerError
		code := "internal_error"
		if errors.Is(err, service.ErrSessionNotFound) {
			status = http.StatusNotFound
			code = "not_found"
		}
		c.JSON(status, gin.H{
			"error": gin.H{"code": code, "message": err.Error()},
		})
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
		// Differentiate busy vs not found vs internal
		status := http.StatusInternalServerError
		code := "internal_error"
		msg := err.Error()

		if errors.Is(err, service.ErrSessionNotFound) {
			status = http.StatusNotFound
			code = "not_found"
		} else if errors.Is(err, service.ErrSessionBusy) {
			status = http.StatusConflict
			code = "conflict"
		} else if errors.Is(err, service.ErrSessionTerminal) {
			status = http.StatusBadRequest
			code = "invalid_request"
		} else if errors.Is(err, service.ErrSessionAdapter) {
			status = http.StatusBadGateway
			code = "bad_gateway"
		}

		c.JSON(status, gin.H{
			"error": gin.H{"code": code, "message": msg},
		})
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
		status := http.StatusInternalServerError
		code := "internal_error"
		if errors.Is(err, service.ErrSessionNotFound) {
			status = http.StatusNotFound
			code = "not_found"
		}
		c.JSON(status, gin.H{
			"error": gin.H{"code": code, "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, sessionResp)
}
