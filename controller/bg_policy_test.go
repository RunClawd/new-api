package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupPolicyTestEnv(t *testing.T) func() {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true

	dsn := fmt.Sprintf("file:policy_%s?mode=memory&cache=shared",
		strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(
		&model.BgCapabilityPolicy{},
		&model.BgRoutingPolicy{},
		&model.BgAuditLog{},
	))

	service.InitPolicyCacheForTest()

	return func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}
}

func policyCtx(t *testing.T, method, path string, body interface{}) (*gin.Context, *httptest.ResponseRecorder) {
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
	ctx.Set("id", 1)
	ctx.Set("role", 100) // admin
	return ctx, rec
}

// ---------------------------------------------------------------------------
// Capability Policy CRUD
// ---------------------------------------------------------------------------

func TestAdminCapabilityPolicy_CRUD(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	// CREATE
	createBody := map[string]interface{}{
		"scope":              "platform",
		"scope_id":           0,
		"capability_pattern": "bg.llm.*",
		"action":             "deny",
		"priority":           10,
		"description":        "block all LLM",
	}
	ctx, rec := policyCtx(t, http.MethodPost, "/api/bg/policies/capabilities", createBody)
	AdminCreateBgCapabilityPolicy(ctx)
	require.Equal(t, http.StatusCreated, rec.Code)

	var created model.BgCapabilityPolicy
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, "platform", created.Scope)
	assert.Equal(t, "deny", created.Action)
	assert.Equal(t, "active", created.Status) // default
	assert.True(t, created.ID > 0)

	// LIST
	ctx2, rec2 := policyCtx(t, http.MethodGet, "/api/bg/policies/capabilities?p=1&page_size=10", nil)
	AdminListBgCapabilityPolicies(ctx2)
	assert.Equal(t, http.StatusOK, rec2.Code)

	// UPDATE
	updateBody := map[string]interface{}{
		"scope":              "platform",
		"scope_id":           0,
		"capability_pattern": "bg.llm.*",
		"action":             "allow",
		"priority":           20,
		"description":        "allow all LLM",
	}
	ctx3, rec3 := policyCtx(t, http.MethodPut, fmt.Sprintf("/api/bg/policies/capabilities/%d", created.ID), updateBody)
	ctx3.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", created.ID)}}
	AdminUpdateBgCapabilityPolicy(ctx3)
	assert.Equal(t, http.StatusOK, rec3.Code)

	// DELETE
	ctx4, rec4 := policyCtx(t, http.MethodDelete, fmt.Sprintf("/api/bg/policies/capabilities/%d", created.ID), nil)
	ctx4.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", created.ID)}}
	AdminDeleteBgCapabilityPolicy(ctx4)
	assert.Equal(t, http.StatusOK, rec4.Code)

	// DELETE again — 404
	ctx5, rec5 := policyCtx(t, http.MethodDelete, fmt.Sprintf("/api/bg/policies/capabilities/%d", created.ID), nil)
	ctx5.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", created.ID)}}
	AdminDeleteBgCapabilityPolicy(ctx5)
	assert.Equal(t, http.StatusNotFound, rec5.Code)

	// UPDATE nonexistent — 404
	ctx6, rec6 := policyCtx(t, http.MethodPut, "/api/bg/policies/capabilities/99999", updateBody)
	ctx6.Params = gin.Params{{Key: "id", Value: "99999"}}
	AdminUpdateBgCapabilityPolicy(ctx6)
	assert.Equal(t, http.StatusNotFound, rec6.Code)
}

func TestAdminCapabilityPolicy_ValidationError(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	// Invalid scope
	body := map[string]interface{}{
		"scope":              "invalid",
		"capability_pattern": "bg.llm.*",
		"action":             "deny",
	}
	ctx, rec := policyCtx(t, http.MethodPost, "/api/bg/policies/capabilities", body)
	AdminCreateBgCapabilityPolicy(ctx)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid scope")
}

// ---------------------------------------------------------------------------
// Routing Policy CRUD
// ---------------------------------------------------------------------------

func TestAdminRoutingPolicy_CRUD(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	// CREATE
	createBody := map[string]interface{}{
		"scope":              "org",
		"scope_id":           1,
		"capability_pattern": "bg.llm.*",
		"strategy":           "fixed",
		"rules_json":         `{"adapter_name": "my-adapter"}`,
		"priority":           5,
	}
	ctx, rec := policyCtx(t, http.MethodPost, "/api/bg/policies/routing", createBody)
	AdminCreateBgRoutingPolicy(ctx)
	require.Equal(t, http.StatusCreated, rec.Code)

	var created model.BgRoutingPolicy
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, "org", created.Scope)
	assert.Equal(t, "fixed", created.Strategy)
	assert.True(t, created.ID > 0)

	// LIST
	ctx2, rec2 := policyCtx(t, http.MethodGet, "/api/bg/policies/routing?p=1&page_size=10", nil)
	AdminListBgRoutingPolicies(ctx2)
	assert.Equal(t, http.StatusOK, rec2.Code)

	// UPDATE
	updateBody := map[string]interface{}{
		"scope":              "org",
		"scope_id":           1,
		"capability_pattern": "bg.llm.*",
		"strategy":           "weighted",
		"rules_json":         `{"weights": {"a": 50, "b": 50}}`,
		"priority":           10,
	}
	ctx3, rec3 := policyCtx(t, http.MethodPut, fmt.Sprintf("/api/bg/policies/routing/%d", created.ID), updateBody)
	ctx3.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", created.ID)}}
	AdminUpdateBgRoutingPolicy(ctx3)
	assert.Equal(t, http.StatusOK, rec3.Code)

	// DELETE
	ctx4, rec4 := policyCtx(t, http.MethodDelete, fmt.Sprintf("/api/bg/policies/routing/%d", created.ID), nil)
	ctx4.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", created.ID)}}
	AdminDeleteBgRoutingPolicy(ctx4)
	assert.Equal(t, http.StatusOK, rec4.Code)

	// DELETE again — 404
	ctx5, rec5 := policyCtx(t, http.MethodDelete, fmt.Sprintf("/api/bg/policies/routing/%d", created.ID), nil)
	ctx5.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", created.ID)}}
	AdminDeleteBgRoutingPolicy(ctx5)
	assert.Equal(t, http.StatusNotFound, rec5.Code)

	// UPDATE nonexistent — 404
	ctx6, rec6 := policyCtx(t, http.MethodPut, "/api/bg/policies/routing/99999", updateBody)
	ctx6.Params = gin.Params{{Key: "id", Value: "99999"}}
	AdminUpdateBgRoutingPolicy(ctx6)
	assert.Equal(t, http.StatusNotFound, rec6.Code)
}

func TestAdminRoutingPolicy_ValidationError(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	// Invalid strategy
	body := map[string]interface{}{
		"scope":              "platform",
		"capability_pattern": "bg.llm.*",
		"strategy":           "random",
		"rules_json":         `{}`,
	}
	ctx, rec := policyCtx(t, http.MethodPost, "/api/bg/policies/routing", body)
	AdminCreateBgRoutingPolicy(ctx)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid strategy")
}

// ---------------------------------------------------------------------------
// Audit trail verification
// ---------------------------------------------------------------------------

func TestAdminPolicy_AuditLogged(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	body := map[string]interface{}{
		"scope":              "platform",
		"scope_id":           0,
		"capability_pattern": "bg.video.*",
		"action":             "deny",
	}
	ctx, rec := policyCtx(t, http.MethodPost, "/api/bg/policies/capabilities", body)
	AdminCreateBgCapabilityPolicy(ctx)
	require.Equal(t, http.StatusCreated, rec.Code)

	// RecordBgAuditLog writes asynchronously in a goroutine — brief wait
	time.Sleep(100 * time.Millisecond)

	// Verify audit log was written
	var count int64
	model.DB.Model(&model.BgAuditLog{}).Where("event_type = ?", "capability_policy_created").Count(&count)
	assert.Equal(t, int64(1), count)
}
