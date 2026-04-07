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

func setupProjectAdminRouter() *gin.Engine {
	router := setupAdminRouter()

	// The base setupAdminRouter already creates /api/bg with admin auth.
	// We need to add project routes to the same group. Since setupAdminRouter
	// returns a full engine, we add a new group with the same middleware.
	bgAdminRoute := router.Group("/api/bg")
	bgAdminRoute.Use(func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("role", 100) // admin
		c.Next()
	})
	{
		bgAdminRoute.GET("/projects", AdminListBgProjects)
		bgAdminRoute.POST("/projects", AdminCreateBgProject)
		bgAdminRoute.PUT("/projects/:id", AdminUpdateBgProject)
		bgAdminRoute.DELETE("/projects/:id", AdminDeleteBgProject)
	}
	return router
}

func TestAdminBgProjects_CRUD(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	router := setupProjectAdminRouter()

	// --- Create ---
	createBody, _ := json.Marshal(map[string]interface{}{
		"name":        "Test Project",
		"description": "A test project for CRUD",
		"org_id":      1,
	})
	req, _ := http.NewRequest("POST", "/api/bg/projects", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var createResult map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &createResult))
	assert.True(t, createResult["success"].(bool))

	data := createResult["data"].(map[string]interface{})
	projectID := data["project_id"].(string)
	assert.NotEmpty(t, projectID)
	assert.True(t, len(projectID) > 5, "project_id should have prefix + nanoid")
	assert.Equal(t, "Test Project", data["name"])
	assert.Equal(t, "active", data["status"])
	assert.Equal(t, float64(1), data["org_id"])

	// --- List ---
	req2, _ := http.NewRequest("GET", "/api/bg/projects?p=1&page_size=10&org_id=1", nil)
	resp2 := httptest.NewRecorder()
	router.ServeHTTP(resp2, req2)

	assert.Equal(t, http.StatusOK, resp2.Code)

	var listResult map[string]interface{}
	require.NoError(t, json.Unmarshal(resp2.Body.Bytes(), &listResult))
	assert.True(t, listResult["success"].(bool))

	listData := listResult["data"].(map[string]interface{})
	assert.Equal(t, float64(1), listData["total"])
	items := listData["items"].([]interface{})
	assert.Equal(t, 1, len(items))

	// --- List all (org_id=0) ---
	req2b, _ := http.NewRequest("GET", "/api/bg/projects?p=1&page_size=10", nil)
	resp2b := httptest.NewRecorder()
	router.ServeHTTP(resp2b, req2b)

	assert.Equal(t, http.StatusOK, resp2b.Code)
	var listAllResult map[string]interface{}
	require.NoError(t, json.Unmarshal(resp2b.Body.Bytes(), &listAllResult))
	listAllData := listAllResult["data"].(map[string]interface{})
	assert.Equal(t, float64(1), listAllData["total"])

	// --- Update ---
	updateBody, _ := json.Marshal(map[string]interface{}{
		"name":   "Updated Project",
		"status": "archived",
	})
	req3, _ := http.NewRequest("PUT", "/api/bg/projects/"+projectID, bytes.NewReader(updateBody))
	req3.Header.Set("Content-Type", "application/json")
	resp3 := httptest.NewRecorder()
	router.ServeHTTP(resp3, req3)

	assert.Equal(t, http.StatusOK, resp3.Code)

	var updateResult map[string]interface{}
	require.NoError(t, json.Unmarshal(resp3.Body.Bytes(), &updateResult))
	assert.True(t, updateResult["success"].(bool))

	updatedData := updateResult["data"].(map[string]interface{})
	assert.Equal(t, "Updated Project", updatedData["name"])
	assert.Equal(t, "archived", updatedData["status"])

	// --- Verify updated via DB ---
	dbProject, err := model.GetBgProjectByProjectID(projectID)
	require.NoError(t, err)
	assert.Equal(t, "Updated Project", dbProject.Name)
	assert.Equal(t, "archived", dbProject.Status)

	// --- Delete ---
	req4, _ := http.NewRequest("DELETE", "/api/bg/projects/"+projectID, nil)
	resp4 := httptest.NewRecorder()
	router.ServeHTTP(resp4, req4)

	assert.Equal(t, http.StatusOK, resp4.Code)

	var deleteResult map[string]interface{}
	require.NoError(t, json.Unmarshal(resp4.Body.Bytes(), &deleteResult))
	assert.True(t, deleteResult["success"].(bool))

	// --- Verify deleted ---
	_, err = model.GetBgProjectByProjectID(projectID)
	assert.Error(t, err, "project should be deleted")

	// --- List again should be empty ---
	req5, _ := http.NewRequest("GET", "/api/bg/projects?p=1&page_size=10&org_id=1", nil)
	resp5 := httptest.NewRecorder()
	router.ServeHTTP(resp5, req5)

	var emptyResult map[string]interface{}
	require.NoError(t, json.Unmarshal(resp5.Body.Bytes(), &emptyResult))
	emptyData := emptyResult["data"].(map[string]interface{})
	assert.Equal(t, float64(0), emptyData["total"])
}

func TestAdminBgProjects_CreateValidation(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	router := setupProjectAdminRouter()

	// Empty name should fail
	createBody, _ := json.Marshal(map[string]interface{}{
		"name":        "",
		"description": "Missing name",
		"org_id":      1,
	})
	req, _ := http.NewRequest("POST", "/api/bg/projects", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.False(t, result["success"].(bool))
	assert.Contains(t, result["message"], "name is required")
}

func TestAdminBgProjects_DeleteNotFound(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	router := setupProjectAdminRouter()

	req, _ := http.NewRequest("DELETE", "/api/bg/projects/proj_nonexistent_id_12345", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.False(t, result["success"].(bool))
	assert.Contains(t, result["message"], "Failed to delete project")
}
