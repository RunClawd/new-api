package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// DevListBgCapabilities handles GET /api/bg/dev/capabilities
// Returns ACTIVE capabilities enriched with live pricing.
// Difference from admin: uses GetActiveBgCapabilities (status='active' only), no pagination.
func DevListBgCapabilities(c *gin.Context) {
	caps, err := model.GetActiveBgCapabilities()
	if err != nil {
		common.ApiErrorMsg(c, "Failed to list capabilities: "+err.Error())
		return
	}
	enriched := enrichCapabilitiesWithPricing(caps)
	common.ApiSuccess(c, enriched)
}
