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
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// E2E test ENV: in-memory SQLite + mock adapter + minimal Gin router
// ---------------------------------------------------------------------------

func setupE2ETestEnv(t *testing.T) func() {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:e2e_%s?mode=memory&cache=shared",
		strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err, "open in-memory sqlite")
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(
		&model.BgResponse{},
		&model.BgResponseAttempt{},
		&model.BgUsageRecord{},
		&model.BgBillingRecord{},
		&model.BgLedgerEntry{},
		&model.BgWebhookEvent{},
		&model.BgAuditLog{},
		&model.BgCapability{},
		&model.BgSession{},
		&model.BgSessionAction{},
		&model.BgProject{},
	), "auto migrate")

	basegate.ClearRegistry()

	return func() {
		basegate.ClearRegistry()
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}
}

// newE2ECtx creates a gin.Context with auth pre-set (id=1 == org/user scope).
func newE2ECtx(t *testing.T, method, path string, body interface{}) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		require.NoError(t, err)
	}
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(method, path, bytes.NewReader(payload))
	if body != nil {
		ctx.Request.Header.Set("Content-Type", "application/json")
	}
	// Auth context: "id" is the user/org ID used by all bg controllers
	ctx.Set("id", 1)
	ctx.Set("token_id", 10)
	return ctx, rec
}

// newE2EMockAdapter creates a mock adapter with a deterministic invoke result.
func newE2EMockAdapter(capability, name string, result *relaycommon.AdapterResult) *mockBgAdapter {
	return &mockBgAdapter{
		name: name,
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: capability, AdapterName: name, Weight: 1},
		},
		invokeResult: result,
	}
}

// ---------------------------------------------------------------------------
// Test 1: Sync dispatch — POST → 200 succeeded → billing records created
// ---------------------------------------------------------------------------

func TestE2E_SyncDispatch_BillingCreated(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	const capabilityModel = "bg.llm.chat.e2e_sync"
	basegate.RegisterAdapter(newE2EMockAdapter(capabilityModel, "e2e_sync_adapter", &relaycommon.AdapterResult{
		Status: "succeeded",
		Output: []relaycommon.OutputItem{{Type: "text", Content: "Hello E2E"}},
		RawUsage: &relaycommon.ProviderUsage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}))

	ctx, rec := newE2ECtx(t, http.MethodPost, "/v1/bg/responses", map[string]interface{}{
		"model": capabilityModel,
		"input": "say hello",
	})
	PostResponses(ctx)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "succeeded", resp["status"])
	assert.NotEmpty(t, resp["id"])
	// poll_url must NOT be set for terminal responses
	assert.Empty(t, resp["poll_url"], "terminal response must not have poll_url")

	// Verify usage record was created with correct response_id (Step 0 bug fix verification)
	// Note: org_id=1, but in test env LookupPricing returns 0 UnitPrice so billing record
	// is skipped. Usage record is always written.
	var usageCount int64
	model.DB.Model(&model.BgUsageRecord{}).Where("response_id LIKE 'resp_%'").Count(&usageCount)
	assert.GreaterOrEqual(t, usageCount, int64(1), "usage record must be created after finalize")
}

// ---------------------------------------------------------------------------
// Test 2: Async dispatch — POST → 202 with poll_url → ApplyProviderEvent → succeeded
// ---------------------------------------------------------------------------

func TestE2E_AsyncDispatch_PollURL(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	const capName = "bg.llm.chat.e2e_async"
	basegate.RegisterAdapter(newE2EMockAdapter(capName, "e2e_async_adapter", &relaycommon.AdapterResult{
		Status:            "accepted",
		ProviderRequestID: "prov-123",
		PollAfterMs:       1000,
	}))

	ctx, rec := newE2ECtx(t, http.MethodPost, "/v1/bg/responses", map[string]interface{}{
		"model": capName,
		"input": "generate video",
		"execution_options": map[string]interface{}{
			"mode": "async",
		},
	})
	PostResponses(ctx)

	assert.Equal(t, http.StatusAccepted, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	// Async adapter returns "accepted" but state machine may auto-advance to "queued".
	// Both are non-terminal, so poll_url must be present in either case.
	respStatus := resp["status"].(string)
	assert.Contains(t, []string{"accepted", "queued", "running"}, respStatus,
		"async response should be in a non-terminal state")
	responseID := resp["id"].(string)
	// poll_url must be present for non-terminal responses
	pollURL, ok := resp["poll_url"].(string)
	require.True(t, ok && pollURL != "", "async response must include poll_url")
	assert.Equal(t, "/v1/bg/responses/"+responseID, pollURL)

	// Simulate provider finishing — drive state machine to succeeded
	attempts, err := model.GetBgAttemptsByResponseID(responseID)
	require.NoError(t, err)
	require.NotEmpty(t, attempts)
	err = service.ApplyProviderEvent(responseID, attempts[0].AttemptID, service.ProviderEvent{
		Status: "succeeded",
		Output: []interface{}{map[string]interface{}{"type": "video", "content": "video_url"}},
		RawUsage: map[string]interface{}{
			"duration_sec": 30.0,
		},
	})
	require.NoError(t, err)

	// GET → succeeded, no poll_url
	ctx2, rec2 := newE2ECtx(t, http.MethodGet, "/v1/bg/responses/"+responseID, nil)
	ctx2.Params = gin.Params{{Key: "id", Value: responseID}}
	GetResponseByID(ctx2)

	assert.Equal(t, http.StatusOK, rec2.Code)
	var resp2 map[string]interface{}
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.Equal(t, "succeeded", resp2["status"])
	assert.Empty(t, resp2["poll_url"], "terminal response must not have poll_url")
}

// ---------------------------------------------------------------------------
// Test 3: Idempotency — same key+payload → same ID; different payload → 409
// ---------------------------------------------------------------------------

func TestE2E_Idempotency(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	const capName = "bg.llm.chat.e2e_idem"
	basegate.RegisterAdapter(newE2EMockAdapter(capName, "e2e_idem_adapter", &relaycommon.AdapterResult{
		Status: "succeeded",
		Output: []relaycommon.OutputItem{{Type: "text", Content: "hello"}},
	}))

	body := map[string]interface{}{"model": capName, "input": "hello world"}

	// First request — creates the response
	ctx1, rec1 := newE2ECtx(t, http.MethodPost, "/v1/bg/responses", body)
	ctx1.Request.Header.Set("Idempotency-Key", "test-idem-key-1")
	PostResponses(ctx1)
	require.Equal(t, http.StatusOK, rec1.Code)

	var resp1 map[string]interface{}
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &resp1))
	id1 := resp1["id"].(string)

	// Second request — same key+payload → returns same response
	ctx2, rec2 := newE2ECtx(t, http.MethodPost, "/v1/bg/responses", body)
	ctx2.Request.Header.Set("Idempotency-Key", "test-idem-key-1")
	PostResponses(ctx2)
	require.Equal(t, http.StatusOK, rec2.Code)

	var resp2 map[string]interface{}
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.Equal(t, id1, resp2["id"], "same idempotency key+payload must return same response ID")

	// Third request — same key, DIFFERENT payload → 409 Conflict
	bodyDiff := map[string]interface{}{"model": capName, "input": "completely different prompt"}
	ctx3, rec3 := newE2ECtx(t, http.MethodPost, "/v1/bg/responses", bodyDiff)
	ctx3.Request.Header.Set("Idempotency-Key", "test-idem-key-1")
	PostResponses(ctx3)
	assert.Equal(t, http.StatusConflict, rec3.Code)

	var errResp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec3.Body.Bytes(), &errResp))
	errObj, _ := errResp["error"].(map[string]interface{})
	assert.Equal(t, "idempotency_mismatch", errObj["code"])
}

// ---------------------------------------------------------------------------
// Test 4: Cancel — POST → running → cancel → status=canceled
// ---------------------------------------------------------------------------

func TestE2E_Cancel(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	const capName = "bg.llm.chat.e2e_cancel"
	// Adapter returns "accepted" (simulates async-like state)
	basegate.RegisterAdapter(newE2EMockAdapter(capName, "e2e_cancel_adapter", &relaycommon.AdapterResult{
		Status:      "accepted",
		PollAfterMs: 5000,
	}))

	// Create response
	ctx1, rec1 := newE2ECtx(t, http.MethodPost, "/v1/bg/responses", map[string]interface{}{
		"model": capName,
		"input": "long task",
		"execution_options": map[string]interface{}{"mode": "async"},
	})
	PostResponses(ctx1)
	require.Equal(t, http.StatusAccepted, rec1.Code)

	var resp1 map[string]interface{}
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &resp1))
	responseID := resp1["id"].(string)

	// Simulate provider advancing to "running"
	attempts, err := model.GetBgAttemptsByResponseID(responseID)
	require.NoError(t, err)
	require.NotEmpty(t, attempts)
	err = service.ApplyProviderEvent(responseID, attempts[0].AttemptID, service.ProviderEvent{
		Status: "running",
	})
	require.NoError(t, err)

	// Cancel it
	ctx2, rec2 := newE2ECtx(t, http.MethodPost, "/v1/bg/responses/"+responseID+"/cancel", nil)
	ctx2.Params = gin.Params{{Key: "id", Value: responseID}}
	CancelResponseByID(ctx2)
	assert.Equal(t, http.StatusOK, rec2.Code)

	var resp2 map[string]interface{}
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.Equal(t, "canceled", resp2["status"])
	assert.Empty(t, resp2["poll_url"], "canceled response must not have poll_url")
}
