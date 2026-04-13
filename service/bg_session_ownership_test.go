package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedSession(t *testing.T, sessionID string, orgID int, status model.BgSessionStatus) {
	t.Helper()
	sess := &model.BgSession{
		SessionID:     sessionID,
		Model:         "bg.llm.chat.standard",
		AdapterName:   "mock_adapter",
		OrgID:         orgID,
		Status:        status,
		StatusVersion: 1,
	}
	require.NoError(t, sess.Insert())
}

// ---------------------------------------------------------------------------
// GetSession ownership tests
// ---------------------------------------------------------------------------

func TestGetSession_OwnerAccess(t *testing.T) {
	truncateBgTables(t)
	seedSession(t, "sess_owner_ok", 1, model.BgSessionStatusActive)

	result, err := GetSession("sess_owner_ok", 1)
	require.NoError(t, err)
	assert.Equal(t, "sess_owner_ok", result.ID)
}

func TestGetSession_CrossTenantDenied(t *testing.T) {
	truncateBgTables(t)
	seedSession(t, "sess_cross_deny", 1, model.BgSessionStatusActive)

	_, err := GetSession("sess_cross_deny", 2)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestGetSession_AdminBypass(t *testing.T) {
	truncateBgTables(t)
	seedSession(t, "sess_admin_0", 1, model.BgSessionStatusActive)

	result, err := GetSession("sess_admin_0", 0)
	require.NoError(t, err)
	assert.Equal(t, "sess_admin_0", result.ID)
}

// ---------------------------------------------------------------------------
// CloseSession ownership tests
// ---------------------------------------------------------------------------

func TestCloseSession_CrossTenantDenied(t *testing.T) {
	truncateBgTables(t)
	seedSession(t, "sess_close_cross", 1, model.BgSessionStatusActive)

	_, err := CloseSession("sess_close_cross", 2)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

// ---------------------------------------------------------------------------
// ExecuteSessionAction ownership tests
// ---------------------------------------------------------------------------

func TestExecuteSessionAction_CrossTenantDenied(t *testing.T) {
	truncateBgTables(t)
	seedSession(t, "sess_action_cross", 1, model.BgSessionStatusActive)

	req := &dto.BGSessionActionRequest{Action: "test"}
	_, err := ExecuteSessionAction("sess_action_cross", 2, req)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrSessionNotFound)
}
