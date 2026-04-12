package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/basegate"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBestEffortAdoptWinningAdapterMetadata_BYOUpdatesLastUsed(t *testing.T) {
	truncateBgTables(t)

	resp := &model.BgResponse{
		ResponseID:    "resp_meta_byo",
		Status:        model.BgResponseStatusQueued,
		StatusVersion: 1,
		Model:         "bg.llm.chat.standard",
		OrgID:         1,
	}
	require.NoError(t, resp.Insert())

	cred := &model.BgBYOCredential{
		OrgID:         1,
		Name:          "byo",
		Provider:      "anthropic",
		EncryptedData: []byte("x"),
		Nonce:         []byte("y"),
		Salt:          "salt",
	}
	require.NoError(t, cred.Insert())

	bestEffortAdoptWinningAdapterMetadata(resp.ID, basegate.ResolvedAdapter{
		BillingSource:   "byo",
		BYOCredentialID: cred.ID,
		FeeConfig: &relaycommon.BYOFeeConfig{
			FeeType:     "per_request",
			FixedAmount: 0.05,
		},
	})

	foundResp, err := model.GetBgResponseByResponseID(resp.ResponseID)
	require.NoError(t, err)
	assert.Equal(t, "byo", foundResp.BillingSource)
	assert.Equal(t, cred.ID, foundResp.BYOCredentialID)
	assert.NotEmpty(t, foundResp.FeeConfigJSON)

	foundCred, err := model.GetBgBYOCredentialByID(cred.ID)
	require.NoError(t, err)
	assert.True(t, foundCred.LastUsedAt >= time.Now().Unix()-2)
}

func TestBestEffortAdoptWinningAdapterMetadata_HostedClearsBYOFields(t *testing.T) {
	truncateBgTables(t)

	resp := &model.BgResponse{
		ResponseID:      "resp_meta_hosted",
		Status:          model.BgResponseStatusQueued,
		StatusVersion:   1,
		Model:           "bg.llm.chat.standard",
		OrgID:           1,
		BillingSource:   "byo",
		BYOCredentialID: 42,
		FeeConfigJSON:   `{"fee_type":"percentage"}`,
	}
	require.NoError(t, resp.Insert())

	bestEffortAdoptWinningAdapterMetadata(resp.ID, basegate.ResolvedAdapter{
		BillingSource: "hosted",
	})

	foundResp, err := model.GetBgResponseByResponseID(resp.ResponseID)
	require.NoError(t, err)
	assert.Equal(t, "hosted", foundResp.BillingSource)
	assert.Equal(t, int64(0), foundResp.BYOCredentialID)
	assert.Empty(t, foundResp.FeeConfigJSON)
}
