package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// writeBGError maps service-layer sentinel errors to structured HTTP error responses.
// Covers all BaseGate sentinel errors across responses and sessions.
//
// IMPORTANT: This function calls c.JSON(), which is NOT safe after SSE headers
// have been written. For stream paths, callers MUST check !c.Writer.Written()
// before calling this function. If headers are already sent, log the error only.
func writeBGError(c *gin.Context, err error) {
	statusCode := http.StatusInternalServerError
	errCode := "internal_error"
	errType := "api_error"

	switch {
	case errors.Is(err, service.ErrCapabilityDenied):
		statusCode = http.StatusForbidden
		errCode = "capability_denied"
		errType = "permission_error"
	case errors.Is(err, service.ErrIdempotencyConflict):
		statusCode = http.StatusConflict
		errCode = "idempotency_mismatch"
		errType = "invalid_request_error"
	case errors.Is(err, service.ErrInsufficientQuota):
		statusCode = http.StatusPaymentRequired
		errCode = "insufficient_quota"
		errType = "invalid_request_error"
	case errors.Is(err, service.ErrSessionValidation):
		statusCode = http.StatusBadRequest
		errCode = "invalid_request"
		errType = "invalid_request_error"
	case errors.Is(err, service.ErrSessionNotFound):
		statusCode = http.StatusNotFound
		errCode = "not_found"
		errType = "invalid_request_error"
	case errors.Is(err, service.ErrSessionBusy):
		statusCode = http.StatusConflict
		errCode = "conflict"
		errType = "api_error"
	case errors.Is(err, service.ErrSessionTerminal):
		statusCode = http.StatusBadRequest
		errCode = "invalid_request"
		errType = "invalid_request_error"
	case errors.Is(err, service.ErrSessionAdapter):
		statusCode = http.StatusBadGateway
		errCode = "bad_gateway"
		errType = "api_error"
	}

	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"code":    errCode,
			"type":    errType,
			"message": err.Error(),
		},
	})
}
