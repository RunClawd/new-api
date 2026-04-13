package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupDevCapabilitiesRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	devAuth := func(c *gin.Context) {
		c.Set("id", 1)
		c.Next()
	}
	bgDev := router.Group("/api/bg/dev")
	bgDev.Use(devAuth)
	{
		bgDev.GET("/capabilities", DevListBgCapabilities)
	}
	return router
}

func TestDevListBgCapabilities_ActiveOnly(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	// Seed one active and one deprecated capability
	model.DB.Create(&model.BgCapability{
		CapabilityName: "bg.llm.chat.active_one",
		Domain:         "llm",
		Action:         "chat",
		Status:         "active",
		SupportedModes: "sync,stream",
		BillableUnit:   "token",
	})
	model.DB.Create(&model.BgCapability{
		CapabilityName: "bg.llm.chat.deprecated_one",
		Domain:         "llm",
		Action:         "chat",
		Status:         "deprecated",
		SupportedModes: "sync",
		BillableUnit:   "token",
	})

	router := setupDevCapabilitiesRouter()

	req, _ := http.NewRequest("GET", "/api/bg/dev/capabilities", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.True(t, result["success"].(bool))

	items := result["data"].([]interface{})
	assert.Equal(t, 1, len(items), "should only return active capabilities")

	item := items[0].(map[string]interface{})
	assert.Equal(t, "bg.llm.chat.active_one", item["capability_name"])
}

func TestDevListBgCapabilities_HasPricingFields(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	model.DB.Create(&model.BgCapability{
		CapabilityName: "bg.llm.chat.pricing_check",
		Domain:         "llm",
		Action:         "chat",
		Status:         "active",
		SupportedModes: "sync",
		BillableUnit:   "token",
	})

	router := setupDevCapabilitiesRouter()

	req, _ := http.NewRequest("GET", "/api/bg/dev/capabilities", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.True(t, result["success"].(bool))

	items := result["data"].([]interface{})
	require.NotEmpty(t, items)

	item := items[0].(map[string]interface{})
	// The enrichCapabilitiesWithPricing helper should add pricing_mode and unit_price
	_, hasPricingMode := item["pricing_mode"]
	_, hasUnitPrice := item["unit_price"]
	assert.True(t, hasPricingMode, "response should include pricing_mode field")
	assert.True(t, hasUnitPrice, "response should include unit_price field")
}
