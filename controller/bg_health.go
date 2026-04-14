package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/basegate"
	"github.com/gin-gonic/gin"
)

// HealthCheck handles GET /health
// Returns 200 if all dependencies are reachable, 503 otherwise.
// No authentication required — suitable for Kubernetes liveness/readiness probes.
func HealthCheck(c *gin.Context) {
	checks := make(map[string]string)
	healthy := true

	// Database check
	if sqlDB, err := model.DB.DB(); err != nil {
		checks["database"] = "error: " + err.Error()
		healthy = false
	} else if err := sqlDB.Ping(); err != nil {
		checks["database"] = "error: " + err.Error()
		healthy = false
	} else {
		checks["database"] = "ok"
	}

	// Redis check (optional)
	if common.RedisEnabled {
		if err := common.RDB.Ping(c.Request.Context()).Err(); err != nil {
			checks["redis"] = "error: " + err.Error()
			healthy = false
		} else {
			checks["redis"] = "ok"
		}
	} else {
		checks["redis"] = "disabled"
	}

	// Adapter registry check
	adapters := basegate.ListRegisteredCapabilities()
	checks["adapters"] = "ok"
	if len(adapters) == 0 {
		checks["adapters"] = "warning: no adapters registered"
	}

	status := http.StatusOK
	statusText := "healthy"
	if !healthy {
		status = http.StatusServiceUnavailable
		statusText = "unhealthy"
	}

	c.JSON(status, gin.H{
		"status": statusText,
		"checks": checks,
	})
}
