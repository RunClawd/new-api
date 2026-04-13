package controller

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper: create a gin.Context with org/user id and optional X-Project-Id header.
func newResolveCtx(orgID int, header string) *gin.Context {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest("GET", "/", nil)
	ctx.Set("id", orgID)
	if header != "" {
		ctx.Request.Header.Set("X-Project-Id", header)
	}
	return ctx
}

func TestResolveProjectID_PublicIDOwned(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	project := &model.BgProject{ProjectID: "proj_resolve_own", OrgID: 1, Name: "Mine"}
	require.NoError(t, model.CreateBgProject(project))

	ctx := newResolveCtx(1, "proj_resolve_own")
	id, err := resolveProjectID(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int(project.ID), id)
}

func TestResolveProjectID_PublicIDOtherOrg(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	project := &model.BgProject{ProjectID: "proj_resolve_other", OrgID: 99, Name: "Other"}
	require.NoError(t, model.CreateBgProject(project))

	ctx := newResolveCtx(1, "proj_resolve_other")
	_, err := resolveProjectID(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to current org")
}

func TestResolveProjectID_InvalidString(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	ctx := newResolveCtx(1, "abc_invalid_xyz")
	_, err := resolveProjectID(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid X-Project-Id")
}

func TestResolveProjectID_HeaderAbsent(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	ctx := newResolveCtx(1, "")
	id, err := resolveProjectID(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 0, id)
}

func TestResolveProjectID_BoundToken_HeaderMismatch_AuditLog(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	projectA := &model.BgProject{ProjectID: "proj_bound_a", OrgID: 1, Name: "A"}
	require.NoError(t, model.CreateBgProject(projectA))

	projectB := &model.BgProject{ProjectID: "proj_bound_b", OrgID: 1, Name: "B"}
	require.NoError(t, model.CreateBgProject(projectB))

	ctx := newResolveCtx(1, "proj_bound_b")
	ctx.Set("bg_bound_project_id", projectA.ID) // token bound to A

	id, err := resolveProjectID(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int(projectA.ID), id, "should return bound project, not header")

	// RecordBgAuditLog writes asynchronously via goroutine; wait briefly
	time.Sleep(100 * time.Millisecond)

	var auditCount int64
	model.DB.Model(&model.BgAuditLog{}).Where("event_type = ?", "project_binding_mismatch").Count(&auditCount)
	assert.Equal(t, int64(1), auditCount)
}

func TestResolveProjectID_BoundToken_HeaderSameNumeric_NoAudit(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	project := &model.BgProject{ProjectID: "proj_bound_same", OrgID: 1, Name: "Same"}
	require.NoError(t, model.CreateBgProject(project))

	// Pass same project's internal numeric ID as header — should NOT trigger audit
	ctx := newResolveCtx(1, "")
	ctx.Request.Header.Set("X-Project-Id", fmt.Sprintf("%d", project.ID))
	ctx.Set("bg_bound_project_id", project.ID)

	id, err := resolveProjectID(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int(project.ID), id)

	// No audit log should be written
	var auditCount int64
	model.DB.Model(&model.BgAuditLog{}).Where("event_type = ?", "project_binding_mismatch").Count(&auditCount)
	assert.Equal(t, int64(0), auditCount)
}

func TestResolveProjectID_NumericFallback_Owned(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	project := &model.BgProject{ProjectID: "proj_numeric_ok", OrgID: 1, Name: "NumOK"}
	require.NoError(t, model.CreateBgProject(project))

	ctx := newResolveCtx(1, fmt.Sprintf("%d", project.ID))
	id, err := resolveProjectID(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int(project.ID), id)
}

func TestResolveProjectID_NumericFallback_OtherOrg(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	project := &model.BgProject{ProjectID: "proj_numeric_other", OrgID: 99, Name: "NumOther"}
	require.NoError(t, model.CreateBgProject(project))

	ctx := newResolveCtx(1, fmt.Sprintf("%d", project.ID))
	_, err := resolveProjectID(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found or does not belong")
}
