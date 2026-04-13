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

func setupDevApiKeyRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	devAuth := func(c *gin.Context) {
		c.Set("id", 1)
		c.Next()
	}
	bgDev := router.Group("/api/bg/dev")
	bgDev.Use(devAuth)
	{
		bgDev.GET("/apikeys", DevListBgApiKeys)
		bgDev.POST("/apikeys", DevCreateBgApiKey)
		bgDev.DELETE("/apikeys/:id", DevDeleteBgApiKey)
		bgDev.POST("/apikeys/:id/reveal", DevRevealBgApiKey)
	}
	return router
}

func TestDevCreateBgApiKey_WithOwnProject(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	// Seed a project owned by user 1
	project := &model.BgProject{ProjectID: "proj_key_own", OrgID: 1, Name: "Own Project"}
	require.NoError(t, model.CreateBgProject(project))

	router := setupDevApiKeyRouter()

	body, _ := json.Marshal(map[string]string{
		"name":       "test-key",
		"project_id": "proj_key_own",
	})
	req, _ := http.NewRequest("POST", "/api/bg/dev/apikeys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.True(t, result["success"].(bool))

	data := result["data"].(map[string]interface{})
	assert.NotEmpty(t, data["key"], "plaintext key should be returned on create")
	assert.Equal(t, "test-key", data["name"])
}

func TestDevCreateBgApiKey_WithOtherOrgProject(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	// Seed a project owned by org 99 (not the test user=1)
	project := &model.BgProject{ProjectID: "proj_key_other", OrgID: 99, Name: "Other Project"}
	require.NoError(t, model.CreateBgProject(project))

	router := setupDevApiKeyRouter()

	body, _ := json.Marshal(map[string]string{
		"name":       "should-fail",
		"project_id": "proj_key_other",
	})
	req, _ := http.NewRequest("POST", "/api/bg/dev/apikeys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.False(t, result["success"].(bool), "cross-tenant project binding should fail")
}

func TestDevListBgApiKeys_NoFilter(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	// Create a token for user 1
	token := &model.Token{UserId: 1, Name: "list-test", Key: "sk-test-list-12345678901234567890", Status: 1, ExpiredTime: -1}
	require.NoError(t, token.Insert())

	router := setupDevApiKeyRouter()

	req, _ := http.NewRequest("GET", "/api/bg/dev/apikeys?p=0&size=10", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.True(t, result["success"].(bool))
}

func TestDevListBgApiKeys_WithProjectFilter(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	// Seed project
	project := &model.BgProject{ProjectID: "proj_filter_key", OrgID: 1, Name: "Filter"}
	require.NoError(t, model.CreateBgProject(project))

	// Create token bound to the project
	token := &model.Token{UserId: 1, Name: "bound", Key: "sk-bound-key-12345678901234567890", Status: 1, ExpiredTime: -1, BgProjectID: project.ID}
	require.NoError(t, token.Insert())
	// Create an unbound token
	token2 := &model.Token{UserId: 1, Name: "unbound", Key: "sk-unbound-key12345678901234567890", Status: 1, ExpiredTime: -1}
	require.NoError(t, token2.Insert())

	router := setupDevApiKeyRouter()

	req, _ := http.NewRequest("GET", "/api/bg/dev/apikeys?p=0&size=10&project_id=proj_filter_key", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.True(t, result["success"].(bool))

	data := result["data"].(map[string]interface{})
	total := data["total"].(float64)
	assert.Equal(t, float64(1), total, "should only return bound token")
}

func TestDevListBgApiKeys_WithOtherOrgProjectFilter(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	project := &model.BgProject{ProjectID: "proj_filter_other", OrgID: 99, Name: "Other Org"}
	require.NoError(t, model.CreateBgProject(project))

	router := setupDevApiKeyRouter()

	req, _ := http.NewRequest("GET", "/api/bg/dev/apikeys?p=0&size=10&project_id=proj_filter_other", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.False(t, result["success"].(bool), "should reject filter with other org's project")
}
