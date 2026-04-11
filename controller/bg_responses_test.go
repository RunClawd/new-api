package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/basegate"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Test DB & mock adapter setup
// ---------------------------------------------------------------------------

func setupBgControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	model.DB = db
	model.LOG_DB = db

	if err := db.AutoMigrate(
		&model.BgResponse{},
		&model.BgResponseAttempt{},
		&model.BgUsageRecord{},
		&model.BgBillingRecord{},
		&model.BgLedgerEntry{},
		&model.BgSession{},
		&model.BgSessionAction{},
		&model.BgWebhookEvent{},
	); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	t.Cleanup(func() {
		basegate.ClearRegistry()
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// mockBgAdapter implements basegate.ProviderAdapter for controller tests.
type mockBgAdapter struct {
	name          string
	capabilities  []relaycommon.CapabilityBinding
	invokeResult  *relaycommon.AdapterResult
	streamEvents  []relaycommon.SSEEvent
	streamError   error
}

func (m *mockBgAdapter) Name() string { return m.name }
func (m *mockBgAdapter) DescribeCapabilities() []relaycommon.CapabilityBinding {
	return m.capabilities
}
func (m *mockBgAdapter) Validate(req *relaycommon.CanonicalRequest) *relaycommon.ValidationResult {
	return &relaycommon.ValidationResult{Valid: true, ResolvedModel: req.Model}
}
func (m *mockBgAdapter) Invoke(req *relaycommon.CanonicalRequest) (*relaycommon.AdapterResult, error) {
	return m.invokeResult, nil
}
func (m *mockBgAdapter) Poll(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockBgAdapter) Cancel(providerRequestID string) (*relaycommon.AdapterResult, error) {
	return &relaycommon.AdapterResult{Status: "canceled"}, nil
}
func (m *mockBgAdapter) Stream(req *relaycommon.CanonicalRequest) (<-chan relaycommon.SSEEvent, error) {
	if m.streamError != nil {
		return nil, m.streamError
	}
	if len(m.streamEvents) == 0 {
		return nil, basegate.ErrStreamNotSupported
	}
	
	ch := make(chan relaycommon.SSEEvent)
	go func() {
		defer close(ch)
		for _, e := range m.streamEvents {
			ch <- e
		}
	}()
	return ch, nil
}

func newBgTestContext(t *testing.T, method, target string, body interface{}) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	var reqBody *bytes.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		require.NoError(t, err)
		reqBody = bytes.NewReader(payload)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, reqBody)
	if body != nil {
		ctx.Request.Header.Set("Content-Type", "application/json")
	}
	// Set default auth context
	ctx.Set("id", 1)        // matches c.GetInt("id")
	ctx.Set("project_id", 1)
	ctx.Set("token_id", 10) // matches c.GetInt("token_id")
	ctx.Set("end_user_id", "user_test")
	return ctx, recorder
}

// ---------------------------------------------------------------------------
// POST /v1/bg/responses — sync success
// ---------------------------------------------------------------------------

func TestPostResponses_SyncSuccess(t *testing.T) {
	setupBgControllerTestDB(t)
	basegate.ClearRegistry()

	basegate.RegisterAdapter(&mockBgAdapter{
		name: "ctrl_mock",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "bg.llm.chat.ctrl_test"},
		},
		invokeResult: &relaycommon.AdapterResult{
			Status: "succeeded",
			Output: []relaycommon.OutputItem{
				{Type: "text", Content: "Hello from controller test!"},
			},
		},
	})

	body := map[string]interface{}{
		"model": "bg.llm.chat.ctrl_test",
		"input": "Say hello",
	}

	ctx, recorder := newBgTestContext(t, http.MethodPost, "/v1/bg/responses", body)
	PostResponses(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.Equal(t, "succeeded", resp["status"])
	assert.Equal(t, "response", resp["object"])
	assert.NotEmpty(t, resp["id"])
}

// ---------------------------------------------------------------------------
// POST /v1/bg/responses — async returns 202
// ---------------------------------------------------------------------------

func TestPostResponses_AsyncReturns202(t *testing.T) {
	setupBgControllerTestDB(t)
	basegate.ClearRegistry()

	basegate.RegisterAdapter(&mockBgAdapter{
		name: "ctrl_async",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "bg.video.gen.ctrl_test"},
		},
		invokeResult: &relaycommon.AdapterResult{
			Status:            "accepted",
			ProviderRequestID: "prov_ctrl_001",
			PollAfterMs:       5000,
		},
	})

	body := map[string]interface{}{
		"model": "bg.video.gen.ctrl_test",
		"input": map[string]string{"prompt": "test video"},
		"execution_options": map[string]interface{}{
			"mode": "async",
		},
	}

	ctx, recorder := newBgTestContext(t, http.MethodPost, "/v1/bg/responses", body)
	PostResponses(ctx)

	assert.Equal(t, http.StatusAccepted, recorder.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.Equal(t, "queued", resp["status"])
}

// ---------------------------------------------------------------------------
// POST /v1/bg/responses — stream
// ---------------------------------------------------------------------------

func TestPostResponses_StreamSuccess(t *testing.T) {
	setupBgControllerTestDB(t)
	basegate.ClearRegistry()

	basegate.RegisterAdapter(&mockBgAdapter{
		name: "ctrl_stream",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "bg.llm.chat.stream_test"},
		},
		streamEvents: []relaycommon.SSEEvent{
			{Type: relaycommon.SSEEventResponseCreated, Data: "started"},
			{Type: relaycommon.SSEEventTextDelta, Data: map[string]interface{}{"delta": "hello "}},
			{Type: relaycommon.SSEEventTextDelta, Data: map[string]interface{}{"delta": "world"}},
			{Type: relaycommon.SSEEventTextDone},
		},
	})

	body := map[string]interface{}{
		"model": "bg.llm.chat.stream_test",
		"input": "streaming prompt",
		"execution_options": map[string]interface{}{
			"mode": "stream",
		},
	}

	ctx, recorder := newBgTestContext(t, http.MethodPost, "/v1/bg/responses", body)
	PostResponses(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Header().Get("Content-Type"), "text/event-stream")

	// Ensure chunks match Schema §3 format: "event: <type>\ndata: <json>\n\n"
	bodyStr := recorder.Body.String()
	assert.Contains(t, bodyStr, "event: response.created\ndata: ")
	assert.Contains(t, bodyStr, "event: response.output_text.delta\ndata: ")
	assert.Contains(t, bodyStr, "data: [DONE]")
}

// ---------------------------------------------------------------------------
// POST /v1/bg/responses — no adapter → 500
// ---------------------------------------------------------------------------

func TestPostResponses_NoAdapter(t *testing.T) {
	setupBgControllerTestDB(t)
	basegate.ClearRegistry()

	body := map[string]interface{}{
		"model": "bg.nonexistent.model",
		"input": "Hello",
	}

	ctx, recorder := newBgTestContext(t, http.MethodPost, "/v1/bg/responses", body)
	PostResponses(ctx)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	errObj := resp["error"].(map[string]interface{})
	assert.Equal(t, "internal_error", errObj["code"])
	assert.Contains(t, errObj["message"], "no adapter")
}

// ---------------------------------------------------------------------------
// POST /v1/bg/responses — invalid JSON → 400
// ---------------------------------------------------------------------------

func TestPostResponses_BadJSON(t *testing.T) {
	setupBgControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/bg/responses",
		bytes.NewReader([]byte(`{invalid json`)))
	ctx.Request.Header.Set("Content-Type", "application/json")

	PostResponses(ctx)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	errObj := resp["error"].(map[string]interface{})
	assert.Equal(t, "invalid_request", errObj["code"])
}

// ---------------------------------------------------------------------------
// POST /v1/bg/responses — Idempotency-Key header
// ---------------------------------------------------------------------------

func TestPostResponses_IdempotencyKey(t *testing.T) {
	setupBgControllerTestDB(t)
	basegate.ClearRegistry()

	basegate.RegisterAdapter(&mockBgAdapter{
		name: "ctrl_idem",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "bg.llm.chat.idem_ctrl"},
		},
		invokeResult: &relaycommon.AdapterResult{
			Status: "succeeded",
			Output: []relaycommon.OutputItem{
				{Type: "text", Content: "first"},
			},
		},
	})

	body := map[string]interface{}{
		"model": "bg.llm.chat.idem_ctrl",
		"input": "Say hello",
	}

	// First call
	ctx1, rec1 := newBgTestContext(t, http.MethodPost, "/v1/bg/responses", body)
	ctx1.Request.Header.Set("Idempotency-Key", "ctrl_idem_001")
	PostResponses(ctx1)
	require.Equal(t, http.StatusOK, rec1.Code)

	var resp1 map[string]interface{}
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &resp1))
	firstID := resp1["id"].(string)

	// Second call with same key
	ctx2, rec2 := newBgTestContext(t, http.MethodPost, "/v1/bg/responses", body)
	ctx2.Request.Header.Set("Idempotency-Key", "ctrl_idem_001")
	PostResponses(ctx2)
	require.Equal(t, http.StatusOK, rec2.Code)

	var resp2 map[string]interface{}
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.Equal(t, firstID, resp2["id"], "idempotent request should return same response ID")
}

// ---------------------------------------------------------------------------
// GET /v1/bg/responses/:id
// ---------------------------------------------------------------------------

func TestGetResponseByID_Found(t *testing.T) {
	setupBgControllerTestDB(t)

	// Seed a response
	resp := &model.BgResponse{
		ResponseID:    "resp_ctrl_get_1",
		Model:         "bg.llm.chat.test",
		Status:        model.BgResponseStatusSucceeded,
		StatusVersion: 1,
		OrgID:         1,
		OutputJSON:    `[{"type":"text","content":"Hello!"}]`,
	}
	require.NoError(t, resp.Insert())

	ctx, recorder := newBgTestContext(t, http.MethodGet, "/v1/bg/responses/resp_ctrl_get_1", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "resp_ctrl_get_1"}}
	GetResponseByID(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	assert.Equal(t, "resp_ctrl_get_1", result["id"])
	assert.Equal(t, "succeeded", result["status"])
}

func TestGetResponseByID_NotFound(t *testing.T) {
	setupBgControllerTestDB(t)

	ctx, recorder := newBgTestContext(t, http.MethodGet, "/v1/bg/responses/resp_nonexistent", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "resp_nonexistent"}}
	GetResponseByID(ctx)

	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestGetResponseByID_EmptyID(t *testing.T) {
	setupBgControllerTestDB(t)

	ctx, recorder := newBgTestContext(t, http.MethodGet, "/v1/bg/responses/", nil)
	ctx.Params = gin.Params{{Key: "id", Value: ""}}
	GetResponseByID(ctx)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

// ---------------------------------------------------------------------------
// POST /v1/bg/responses/:id/cancel
// ---------------------------------------------------------------------------

func TestCancelResponseByID_Success(t *testing.T) {
	setupBgControllerTestDB(t)

	resp := &model.BgResponse{
		ResponseID:    "resp_ctrl_cancel",
		Model:         "bg.video.gen.test",
		Status:        model.BgResponseStatusQueued,
		StatusVersion: 1,
		OrgID:         1,
	}
	require.NoError(t, resp.Insert())

	attempt := &model.BgResponseAttempt{
		AttemptID:     "att_ctrl_cancel",
		ResponseID:    "resp_ctrl_cancel",
		AttemptNo:     1,
		Status:        model.BgAttemptStatusRunning,
		StatusVersion: 1,
	}
	require.NoError(t, attempt.Insert())

	ctx, recorder := newBgTestContext(t, http.MethodPost, "/v1/bg/responses/resp_ctrl_cancel/cancel", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "resp_ctrl_cancel"}}
	CancelResponseByID(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	assert.Equal(t, "canceled", result["status"])
}

func TestCancelResponseByID_NotFound(t *testing.T) {
	setupBgControllerTestDB(t)

	ctx, recorder := newBgTestContext(t, http.MethodPost, "/v1/bg/responses/resp_ghost/cancel", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "resp_ghost"}}
	CancelResponseByID(ctx)

	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestCancelResponseByID_AlreadyTerminal(t *testing.T) {
	setupBgControllerTestDB(t)

	resp := &model.BgResponse{
		ResponseID:    "resp_ctrl_done",
		Model:         "bg.llm.chat.test",
		Status:        model.BgResponseStatusSucceeded,
		StatusVersion: 2,
		OrgID:         1,
		FinalizedAt:   1000,
	}
	require.NoError(t, resp.Insert())

	ctx, recorder := newBgTestContext(t, http.MethodPost, "/v1/bg/responses/resp_ctrl_done/cancel", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "resp_ctrl_done"}}
	CancelResponseByID(ctx)

	// Should succeed but status remains succeeded (cancel is a no-op on terminal)
	assert.Equal(t, http.StatusOK, recorder.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	assert.Equal(t, "succeeded", result["status"])
}
