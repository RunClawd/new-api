package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupDevProjectRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	devAuth := func(c *gin.Context) {
		c.Set("id", 1)
		c.Next()
	}
	bgDev := router.Group("/api/bg/dev")
	bgDev.Use(devAuth)
	{
		bgDev.GET("/projects", DevListBgProjects)
		bgDev.POST("/projects", DevCreateBgProject)
	}
	return router
}

func TestDevCreateBgProject_ExceedsLimit(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	router := setupDevProjectRouter()

	// Create maxProjectsPerOrg projects to fill the quota
	for i := 0; i < maxProjectsPerOrg; i++ {
		body, _ := json.Marshal(map[string]interface{}{
			"name":       "Project " + string(rune('A'+i)),
			"project_id": "proj_limit_" + string(rune('a'+i)),
		})
		req, _ := http.NewRequest("POST", "/api/bg/dev/projects", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		var result map[string]interface{}
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
		require.True(t, result["success"].(bool), "project %d should create OK", i)
	}

	// The 21st project should be rejected
	body, _ := json.Marshal(map[string]interface{}{
		"name":       "One Too Many",
		"project_id": "proj_limit_excess",
	})
	req, _ := http.NewRequest("POST", "/api/bg/dev/projects", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.False(t, result["success"].(bool))
	assert.Contains(t, result["message"], "project limit reached")
}

func TestDevListBgProjects_OrgScoped(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	// Seed projects for org=1 and org=99
	require.NoError(t, model.CreateBgProject(&model.BgProject{ProjectID: "proj_list_a", OrgID: 1, Name: "A"}))
	require.NoError(t, model.CreateBgProject(&model.BgProject{ProjectID: "proj_list_b", OrgID: 1, Name: "B"}))
	require.NoError(t, model.CreateBgProject(&model.BgProject{ProjectID: "proj_list_c", OrgID: 99, Name: "C (other)"}))

	router := setupDevProjectRouter()

	req, _ := http.NewRequest("GET", "/api/bg/dev/projects?size=100", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.True(t, result["success"].(bool))

	data := result["data"].(map[string]interface{})
	total := data["total"].(float64)
	assert.Equal(t, float64(2), total, "should only see org=1 projects, not org=99")
}
