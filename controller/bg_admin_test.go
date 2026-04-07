package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupAdminRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.Default()

	// Mock AdminAuth middleware
	adminAuth := func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("role", common.RoleRootUser) // Root or Admin
		c.Next()
	}

	bgAdminRoute := router.Group("/api/bg")
	bgAdminRoute.Use(adminAuth)
	{
		bgAdminRoute.GET("/responses", AdminListBgResponses)
		bgAdminRoute.GET("/responses/:id", AdminGetBgResponse)
		bgAdminRoute.GET("/sessions", AdminListBgSessions)
		bgAdminRoute.GET("/sessions/:id", AdminGetBgSession)
		bgAdminRoute.GET("/capabilities", AdminListBgCapabilities)
		bgAdminRoute.GET("/usage/stats", AdminGetBgUsageStats)
	}

	return router
}

func TestAdminBgEndpoints(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()
	model.SeedBgCapabilities()

	router := setupAdminRouter()

	t.Run("Test List Capabilities", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/bg/capabilities?p=1&page_size=10", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)

		var result map[string]interface{}
		json.Unmarshal(resp.Body.Bytes(), &result)

		assert.True(t, result["success"].(bool))
		data := result["data"].(map[string]interface{})
		assert.Contains(t, data, "items")
		assert.Contains(t, data, "total")

		items := data["items"].([]interface{})
		assert.GreaterOrEqual(t, len(items), 7) // Seeding creates 7
	})

	t.Run("Test List Responses", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/bg/responses?p=1&page_size=10&status=running", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)

		var result map[string]interface{}
		json.Unmarshal(resp.Body.Bytes(), &result)

		assert.True(t, result["success"].(bool))
		data := result["data"].(map[string]interface{})
		assert.Contains(t, data, "items")
		assert.Contains(t, data, "total")
	})

	t.Run("Test Usage Stats", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/bg/usage/stats?org_id=0", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)

		var result map[string]interface{}
		json.Unmarshal(resp.Body.Bytes(), &result)

		assert.True(t, result["success"].(bool))
		data := result["data"].(map[string]interface{})
		
		// Map should contain fields directly since it's the AdminBgUsageStats struct
		assert.Contains(t, data, "total_requests")
		assert.Contains(t, data, "succeeded_count")
		assert.Contains(t, data, "total_cost")
		assert.Contains(t, data, "total_tokens")

		// With an empty DB, counts should be 0 
		assert.Equal(t, float64(0), data["total_requests"])
		assert.Equal(t, float64(0), data["succeeded_count"])
		assert.Equal(t, float64(0), data["total_cost"])
	})

	t.Run("Test List Sessions", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/bg/sessions?p=1&page_size=10", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)

		var result map[string]interface{}
		json.Unmarshal(resp.Body.Bytes(), &result)

		assert.True(t, result["success"].(bool))
		data := result["data"].(map[string]interface{})
		assert.Contains(t, data, "items")
		assert.Contains(t, data, "total")
	})

	t.Run("Test Detail Endpoints - Negative", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/bg/responses/nonexistent_id", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		assert.Equal(t, http.StatusOK, resp.Code)
		var result map[string]interface{}
		json.Unmarshal(resp.Body.Bytes(), &result)
		assert.False(t, result["success"].(bool))

		req2, _ := http.NewRequest("GET", "/api/bg/sessions/nonexistent_id", nil)
		resp2 := httptest.NewRecorder()
		router.ServeHTTP(resp2, req2)
		assert.Equal(t, http.StatusOK, resp2.Code)
		var result2 map[string]interface{}
		json.Unmarshal(resp2.Body.Bytes(), &result2)
		assert.False(t, result2["success"].(bool))
	})

	t.Run("Test Detail Endpoints - Positive", func(t *testing.T) {
		// Insert dummy data
		respObj := model.BgResponse{
			ResponseID: "resp-test-details-1",
			Model:      "gpt-4",
		}
		model.DB.Create(&respObj)

		attemptObj := model.BgResponseAttempt{
			AttemptID:   "attempt-test-1",
			ResponseID:  respObj.ResponseID,
			AdapterName: "openai",
		}
		model.DB.Create(&attemptObj)

		sessObj := model.BgSession{
			SessionID: "sess-test-details-1",
			Model:     "gpt-4",
		}
		model.DB.Create(&sessObj)

		// Test mapping response details
		req, _ := http.NewRequest("GET", "/api/bg/responses/"+respObj.ResponseID, nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		assert.Equal(t, http.StatusOK, resp.Code)

		var result map[string]interface{}
		json.Unmarshal(resp.Body.Bytes(), &result)

		assert.True(t, result["success"].(bool))
		data := result["data"].(map[string]interface{})
		assert.Equal(t, "resp-test-details-1", data["response_id"])
		assert.Contains(t, data, "attempts")
		assert.Contains(t, data, "usage_records")
		assert.Contains(t, data, "billing_records")
		
		attempts := data["attempts"].([]interface{})
		assert.Equal(t, 1, len(attempts)) // We created one attempt

		// Test mapping session details
		req2, _ := http.NewRequest("GET", "/api/bg/sessions/"+sessObj.SessionID, nil)
		resp2 := httptest.NewRecorder()
		router.ServeHTTP(resp2, req2)
		assert.Equal(t, http.StatusOK, resp2.Code)

		var result2 map[string]interface{}
		json.Unmarshal(resp2.Body.Bytes(), &result2)

		assert.True(t, result2["success"].(bool))
		data2 := result2["data"].(map[string]interface{})
		assert.Equal(t, "sess-test-details-1", data2["session_id"])
		assert.Contains(t, data2, "actions")
	})
}
