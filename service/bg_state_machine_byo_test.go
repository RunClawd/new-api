package service

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedBYOUser creates a user with a unique aff_code to avoid unique constraint conflicts.
func seedBYOUser(t *testing.T, id int, quota int) {
	t.Helper()
	// Clean up any existing user with this ID first
	model.DB.Exec("DELETE FROM users WHERE id = ?", id)
	user := &model.User{
		Id:       id,
		Username: fmt.Sprintf("byo_test_%d", id),
		Quota:    quota,
		Status:   common.UserStatusEnabled,
		AffCode:  fmt.Sprintf("byo%d", id),
	}
	require.NoError(t, model.DB.Create(user).Error)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM users WHERE id = ?", id)
	})
}

// ---------------------------------------------------------------------------
// BYO terminal billing: state machine reads metadata from winning attempt
// ---------------------------------------------------------------------------

func TestStateMachineTerminal_BYO_FromAttempt(t *testing.T) {
	truncateBgTables(t)
	seedBYOUser(t, 900, 1000000)

	// BYO pricing snapshot + fee config
	byoPricing := &relaycommon.PricingSnapshot{
		PricingMode:  "metered",
		BillableUnit: "token",
		UnitPrice:    0.00003,
		Currency:     "usd",
	}
	byoPricingJSON, _ := common.Marshal(byoPricing)
	byoFeeConfig := &relaycommon.BYOFeeConfig{
		FeeType:     "per_request",
		FixedAmount: 0.05,
	}
	byoFeeJSON, _ := common.Marshal(byoFeeConfig)

	// Response-level metadata shows BYO (primary adapter), but attempt may differ
	resp := &model.BgResponse{
		ResponseID:          "resp_byo_sm",
		Model:               "bg.llm.chat.standard",
		Status:              model.BgResponseStatusQueued,
		StatusVersion:       1,
		OrgID:               900,
		BillingSource:       "byo",
		PricingSnapshotJSON: string(byoPricingJSON),
		FeeConfigJSON:       string(byoFeeJSON),
	}
	require.NoError(t, resp.Insert())

	// Winning attempt is BYO with its own metadata baked in at insert time
	attempt := &model.BgResponseAttempt{
		AttemptID:           "att_byo_sm",
		ResponseID:          "resp_byo_sm",
		AttemptNo:           1,
		AdapterName:         "anthropic_native_ch3",
		Status:              model.BgAttemptStatusRunning,
		StatusVersion:       1,
		BillingSource:       "byo",
		BYOCredentialID:     42,
		FeeConfigJSON:       string(byoFeeJSON),
		PricingSnapshotJSON: string(byoPricingJSON),
	}
	require.NoError(t, attempt.Insert())

	event := ProviderEvent{
		Status: "succeeded",
		Output: []interface{}{"hello from BYO"},
		RawUsage: map[string]interface{}{
			"prompt_tokens":     500,
			"completion_tokens": 1500,
			"total_tokens":      2000,
		},
	}
	err := ApplyProviderEvent("resp_byo_sm", "att_byo_sm", event)
	require.NoError(t, err)

	// Verify response finalized
	found, err := model.GetBgResponseByResponseID("resp_byo_sm")
	require.NoError(t, err)
	assert.Equal(t, model.BgResponseStatusSucceeded, found.Status)
	assert.Equal(t, "completed", found.BillingStatus)

	// Verify billing used BYO per_request fee ($0.05), not hosted metered
	var billing model.BgBillingRecord
	err = model.DB.Where("response_id = ? AND status != ?", "resp_byo_sm", "estimated").First(&billing).Error
	require.NoError(t, err)
	assert.Equal(t, "byo", billing.BillingSource)
	assert.InDelta(t, 0.05, billing.Amount, 0.0001)
}

func TestStateMachineTerminal_BYOFallbackToHosted(t *testing.T) {
	truncateBgTables(t)
	seedBYOUser(t, 901, 1000000)

	hostedPricing := &relaycommon.PricingSnapshot{
		PricingMode:  "metered",
		BillableUnit: "token",
		UnitPrice:    0.00003,
		Currency:     "usd",
	}
	hostedPricingJSON, _ := common.Marshal(hostedPricing)

	// Response-level still says "byo" from the primary adapter
	resp := &model.BgResponse{
		ResponseID:          "resp_fallback_sm",
		Model:               "bg.llm.chat.standard",
		Status:              model.BgResponseStatusQueued,
		StatusVersion:       1,
		OrgID:               901,
		BillingSource:       "byo",
		PricingSnapshotJSON: string(hostedPricingJSON),
	}
	require.NoError(t, resp.Insert())

	// Attempt 1: BYO failed
	att1 := &model.BgResponseAttempt{
		AttemptID:     "att_fb_1",
		ResponseID:    "resp_fallback_sm",
		AttemptNo:     1,
		AdapterName:   "anthropic_native_ch3",
		Status:        model.BgAttemptStatusFailed,
		StatusVersion: 2,
		BillingSource: "byo",
	}
	require.NoError(t, att1.Insert())

	// Attempt 2: hosted succeeded — this is the winning attempt
	att2 := &model.BgResponseAttempt{
		AttemptID:           "att_fb_2",
		ResponseID:          "resp_fallback_sm",
		AttemptNo:           2,
		AdapterName:         "openai_native_ch5",
		Status:              model.BgAttemptStatusRunning,
		StatusVersion:       1,
		BillingSource:       "hosted",
		PricingSnapshotJSON: string(hostedPricingJSON),
	}
	require.NoError(t, att2.Insert())

	resp.ActiveAttemptID = att2.ID
	model.DB.Save(resp)

	event := ProviderEvent{
		Status: "succeeded",
		Output: []interface{}{"hello from hosted fallback"},
		RawUsage: map[string]interface{}{
			"prompt_tokens":     1000,
			"completion_tokens": 2000,
			"total_tokens":      3000,
		},
	}
	err := ApplyProviderEvent("resp_fallback_sm", "att_fb_2", event)
	require.NoError(t, err)

	found, _ := model.GetBgResponseByResponseID("resp_fallback_sm")
	assert.Equal(t, model.BgResponseStatusSucceeded, found.Status)

	// Billing should be "hosted", NOT "byo" — state machine reads from winning attempt
	var billing model.BgBillingRecord
	err = model.DB.Where("response_id = ? AND status != ?", "resp_fallback_sm", "estimated").First(&billing).Error
	require.NoError(t, err)
	assert.Equal(t, "hosted", billing.BillingSource)
	// 3000 tokens * $0.00003/token = $0.09
	assert.InDelta(t, 0.09, billing.Amount, 0.001)
}

func TestStateMachineTerminal_CrashRecovery_AttemptMetadata(t *testing.T) {
	truncateBgTables(t)
	seedBYOUser(t, 902, 1000000)

	byoFeeConfig := &relaycommon.BYOFeeConfig{
		FeeType:        "percentage",
		PercentageRate: 0.10,
	}
	byoFeeJSON, _ := common.Marshal(byoFeeConfig)

	byoPricing := &relaycommon.PricingSnapshot{
		PricingMode:  "metered",
		BillableUnit: "token",
		UnitPrice:    0.00003,
		Currency:     "usd",
	}
	byoPricingJSON, _ := common.Marshal(byoPricing)

	// Simulate crash gap: BgResponse was NOT updated with final metadata
	// (e.g. best-effort update failed after fallback)
	// BgResponse still says "hosted" from initial creation, no pricing snapshot
	resp := &model.BgResponse{
		ResponseID:    "resp_crash_sm",
		Model:         "bg.llm.chat.standard",
		Status:        model.BgResponseStatusQueued,
		StatusVersion: 1,
		OrgID:         902,
		BillingSource: "hosted", // stale/wrong — simulates crash gap
		// Deliberately leave PricingSnapshotJSON empty to simulate crash
	}
	require.NoError(t, resp.Insert())

	// But the attempt has ALL metadata baked in (crash-safe)
	attempt := &model.BgResponseAttempt{
		AttemptID:           "att_crash_sm",
		ResponseID:          "resp_crash_sm",
		AttemptNo:           1,
		AdapterName:         "anthropic_native_ch3",
		Status:              model.BgAttemptStatusRunning,
		StatusVersion:       1,
		BillingSource:       "byo",
		BYOCredentialID:     99,
		FeeConfigJSON:       string(byoFeeJSON),
		PricingSnapshotJSON: string(byoPricingJSON),
	}
	require.NoError(t, attempt.Insert())

	event := ProviderEvent{
		Status: "succeeded",
		Output: []interface{}{"crash recovery output"},
		RawUsage: map[string]interface{}{
			"prompt_tokens":     400,
			"completion_tokens": 600,
			"total_tokens":      1000,
		},
	}
	err := ApplyProviderEvent("resp_crash_sm", "att_crash_sm", event)
	require.NoError(t, err)

	// Billing should come from attempt metadata, NOT from stale BgResponse
	var billing model.BgBillingRecord
	err = model.DB.Where("response_id = ? AND status != ?", "resp_crash_sm", "estimated").First(&billing).Error
	require.NoError(t, err)
	assert.Equal(t, "byo", billing.BillingSource) // from attempt, not response

	// 1000 tokens * $0.00003 = $0.03 base, 10% = $0.003
	assert.InDelta(t, 0.003, billing.Amount, 0.0001)
}

func TestSettleReservation_BYOEstimate100_HostedActual3500(t *testing.T) {
	truncateBgTables(t)
	seedBYOUser(t, 903, 1000000)

	// Pre-auth reserved 100 quota (BYO estimate)
	estimatedQuota := 100
	err := ReserveQuota(903, estimatedQuota)
	require.NoError(t, err)

	// Actual hosted billing cost = $0.007 = 3500 quota
	actualQuota := 3500

	beforeQuota, _ := model.GetUserQuota(903, false)

	// Settle should charge extra 3400
	SettleReservation(903, estimatedQuota, actualQuota)

	afterQuota, _ := model.GetUserQuota(903, false)
	// beforeQuota - 3400 = afterQuota
	assert.Equal(t, beforeQuota-(actualQuota-estimatedQuota), afterQuota)
}
