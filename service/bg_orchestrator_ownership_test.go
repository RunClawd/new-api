package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// GetResponse ownership tests
// ---------------------------------------------------------------------------

func TestGetResponse_OwnerAccess(t *testing.T) {
	truncateBgTables(t)

	resp := &model.BgResponse{
		ResponseID:    "resp_owner_ok",
		Model:         "bg.llm.chat.standard",
		Status:        model.BgResponseStatusSucceeded,
		StatusVersion: 1,
		OrgID:         1,
		OutputJSON:    `[{"type":"text","content":"Hello"}]`,
	}
	require.NoError(t, resp.Insert())

	result, err := GetResponse("resp_owner_ok", 1)
	require.NoError(t, err)
	assert.Equal(t, "resp_owner_ok", result.ID)
	assert.Equal(t, "succeeded", result.Status)
}

func TestGetResponse_CrossTenantDenied(t *testing.T) {
	truncateBgTables(t)

	resp := &model.BgResponse{
		ResponseID:    "resp_cross_deny",
		Model:         "bg.llm.chat.standard",
		Status:        model.BgResponseStatusSucceeded,
		StatusVersion: 1,
		OrgID:         1,
		OutputJSON:    `[{"type":"text","content":"Secret"}]`,
	}
	require.NoError(t, resp.Insert())

	// org=2 should NOT be able to see org=1's response
	_, err := GetResponse("resp_cross_deny", 2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found", "should return 'not found', not 'forbidden'")
}

func TestGetResponse_AdminBypass(t *testing.T) {
	truncateBgTables(t)

	resp := &model.BgResponse{
		ResponseID:    "resp_admin_0",
		Model:         "bg.llm.chat.standard",
		Status:        model.BgResponseStatusSucceeded,
		StatusVersion: 1,
		OrgID:         1,
		OutputJSON:    `[{"type":"text","content":"Admin"}]`,
	}
	require.NoError(t, resp.Insert())

	// orgID=0 skips ownership check (admin context)
	result, err := GetResponse("resp_admin_0", 0)
	require.NoError(t, err)
	assert.Equal(t, "resp_admin_0", result.ID)
}

// ---------------------------------------------------------------------------
// CancelResponse ownership tests
// ---------------------------------------------------------------------------

func TestCancelResponse_CrossTenantDenied(t *testing.T) {
	truncateBgTables(t)

	resp := &model.BgResponse{
		ResponseID:    "resp_cancel_cross",
		Model:         "bg.llm.chat.standard",
		Status:        model.BgResponseStatusRunning,
		StatusVersion: 1,
		OrgID:         1,
	}
	require.NoError(t, resp.Insert())

	// org=2 should NOT be able to cancel org=1's response
	_, err := CancelResponse("resp_cancel_cross", 2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
