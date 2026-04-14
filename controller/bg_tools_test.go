package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/basegate"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupToolsRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	auth := func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("token_id", 10)
		c.Next()
	}
	bg := router.Group("/v1/bg")
	bg.Use(auth)
	{
		bg.GET("/tools", ListTools)
		bg.POST("/tools/execute", ExecuteTool)
	}
	return router
}

func TestListTools_WithSchemas(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	// Seed capabilities with schema
	model.DB.Create(&model.BgCapability{
		CapabilityName:  "bg.llm.chat.test_schema",
		Domain:          "llm",
		Action:          "chat",
		Tier:            "standard",
		SupportedModes:  "sync,stream",
		BillableUnit:    "token",
		Status:          "active",
		Description:     "Test capability with schema",
		InputSchemaJSON: `{"type":"object","properties":{"messages":{"type":"array"}},"required":["messages"]}`,
	})
	// Seed capability without schema
	model.DB.Create(&model.BgCapability{
		CapabilityName: "bg.llm.chat.no_schema",
		Domain:         "llm",
		Action:         "chat",
		Tier:           "fast",
		SupportedModes: "sync",
		BillableUnit:   "token",
		Status:         "active",
		Description:    "No schema capability",
	})

	router := setupToolsRouter()

	req, _ := http.NewRequest("GET", "/v1/bg/tools", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.Equal(t, "list", result["object"])

	tools := result["data"].([]interface{})
	assert.Equal(t, 1, len(tools), "only capability with schema should appear")

	tool := tools[0].(map[string]interface{})
	assert.Equal(t, "function", tool["type"])

	fn := tool["function"].(map[string]interface{})
	assert.Equal(t, "bg_llm_chat_test_schema", fn["name"])
	assert.Contains(t, fn["description"], "Test capability with schema")
	assert.NotNil(t, fn["parameters"])
}

func TestListTools_EmptyWhenNoSchemas(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	// Only seed capability without schema
	model.DB.Create(&model.BgCapability{
		CapabilityName: "bg.llm.chat.bare",
		Status:         "active",
	})

	router := setupToolsRouter()

	req, _ := http.NewRequest("GET", "/v1/bg/tools", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))

	// data should be null or empty array
	data := result["data"]
	if data != nil {
		tools := data.([]interface{})
		assert.Equal(t, 0, len(tools))
	}
}

func TestListTools_ToolNameFormat(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	model.DB.Create(&model.BgCapability{
		CapabilityName:  "bg.video.generate.pro",
		Domain:          "video",
		Action:          "generate",
		Tier:            "pro",
		SupportedModes:  "async",
		BillableUnit:    "second",
		Status:          "active",
		InputSchemaJSON: `{"type":"object","properties":{"prompt":{"type":"string"}},"required":["prompt"]}`,
	})

	router := setupToolsRouter()

	req, _ := http.NewRequest("GET", "/v1/bg/tools", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))

	tools := result["data"].([]interface{})
	require.Len(t, tools, 1)

	fn := tools[0].(map[string]interface{})["function"].(map[string]interface{})
	assert.Equal(t, "bg_video_generate_pro", fn["name"], "dots should be converted to underscores")
}

// ---------------------------------------------------------------------------
// POST /v1/bg/tools/execute tests
// ---------------------------------------------------------------------------

func TestExecuteTool_Sync(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	// Register a mock adapter that handles the capability
	basegate.ClearRegistry()
	service.InitPolicyCacheForTest()

	adapter := &mockBgAdapter{
		name: "mock_tool_llm",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "bg.llm.chat.standard", AdapterName: "mock_tool_llm", Weight: 1},
		},
		invokeResult: &relaycommon.AdapterResult{
			Status: "succeeded",
			Output: []relaycommon.OutputItem{{Type: "text", Content: "Tool response!"}},
			RawUsage: &relaycommon.ProviderUsage{
				PromptTokens:     10,
				CompletionTokens: 20,
				TotalTokens:      30,
			},
		},
	}
	basegate.RegisterAdapter(adapter)

	router := setupToolsRouter()

	body, _ := json.Marshal(map[string]interface{}{
		"name":      "bg_llm_chat_standard",
		"arguments": map[string]interface{}{"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}},
	})
	req, _ := http.NewRequest("POST", "/v1/bg/tools/execute", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.Equal(t, "succeeded", result["status"])
}

func TestExecuteTool_NonexistentTool(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	basegate.ClearRegistry()
	service.InitPolicyCacheForTest()

	router := setupToolsRouter()

	body, _ := json.Marshal(map[string]interface{}{
		"name":      "bg_nonexistent_tool",
		"arguments": map[string]interface{}{"prompt": "test"},
	})
	req, _ := http.NewRequest("POST", "/v1/bg/tools/execute", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	// Should fail because no adapter registered for bg.nonexistent.tool
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.NotNil(t, result["error"], "should return an error for nonexistent tool")
}

func TestExecuteTool_MissingName(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	router := setupToolsRouter()

	// Body without "name" field
	body, _ := json.Marshal(map[string]interface{}{
		"arguments": map[string]interface{}{"prompt": "test"},
	})
	req, _ := http.NewRequest("POST", "/v1/bg/tools/execute", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusBadRequest, resp.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	errObj := result["error"].(map[string]interface{})
	assert.Equal(t, "invalid_request", errObj["code"])
}

func TestExecuteTool_EmptyBody(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	router := setupToolsRouter()

	req, _ := http.NewRequest("POST", "/v1/bg/tools/execute", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusBadRequest, resp.Code)
}
