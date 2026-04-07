package model

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func truncateBgTables(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		DB.Exec("DELETE FROM bg_responses")
		DB.Exec("DELETE FROM bg_response_attempts")
		DB.Exec("DELETE FROM bg_usage_records")
		DB.Exec("DELETE FROM bg_billing_records")
		DB.Exec("DELETE FROM bg_ledger_entries")
		DB.Exec("DELETE FROM bg_sessions")
		DB.Exec("DELETE FROM bg_session_actions")
		DB.Exec("DELETE FROM bg_webhook_events")
	})
}

// ---------------------------------------------------------------------------
// BgResponseStatus — pure logic tests
// ---------------------------------------------------------------------------

func TestBgResponseStatus_IsTerminal(t *testing.T) {
	terminals := []BgResponseStatus{
		BgResponseStatusSucceeded, BgResponseStatusFailed,
		BgResponseStatusCanceled, BgResponseStatusExpired,
	}
	for _, s := range terminals {
		assert.True(t, s.IsTerminal(), "expected %s to be terminal", s)
	}

	nonTerminals := []BgResponseStatus{
		BgResponseStatusAccepted, BgResponseStatusQueued,
		BgResponseStatusRunning, BgResponseStatusStreaming,
	}
	for _, s := range nonTerminals {
		assert.False(t, s.IsTerminal(), "expected %s to be non-terminal", s)
	}
}

func TestBgResponseStatus_CanTransitionTo(t *testing.T) {
	tests := []struct {
		from   BgResponseStatus
		to     BgResponseStatus
		expect bool
	}{
		// accepted → queued ✓
		{BgResponseStatusAccepted, BgResponseStatusQueued, true},
		// accepted → failed ✓
		{BgResponseStatusAccepted, BgResponseStatusFailed, true},
		// accepted → succeeded ✗ (must go through queued first)
		{BgResponseStatusAccepted, BgResponseStatusSucceeded, false},
		// queued → running ✓
		{BgResponseStatusQueued, BgResponseStatusRunning, true},
		// queued → streaming ✓
		{BgResponseStatusQueued, BgResponseStatusStreaming, true},
		// queued → succeeded ✓ (sync fast path)
		{BgResponseStatusQueued, BgResponseStatusSucceeded, true},
		// running → succeeded ✓
		{BgResponseStatusRunning, BgResponseStatusSucceeded, true},
		// running → canceled ✓
		{BgResponseStatusRunning, BgResponseStatusCanceled, true},
		// streaming → succeeded ✓
		{BgResponseStatusStreaming, BgResponseStatusSucceeded, true},
		// streaming → expired ✗ (streaming doesn't expire)
		{BgResponseStatusStreaming, BgResponseStatusExpired, false},
		// terminal → anything ✗
		{BgResponseStatusSucceeded, BgResponseStatusFailed, false},
		{BgResponseStatusFailed, BgResponseStatusRunning, false},
		{BgResponseStatusCanceled, BgResponseStatusQueued, false},
		{BgResponseStatusExpired, BgResponseStatusAccepted, false},
	}

	for _, tt := range tests {
		result := tt.from.CanTransitionTo(tt.to)
		assert.Equal(t, tt.expect, result,
			"CanTransitionTo(%s → %s) = %v, want %v", tt.from, tt.to, result, tt.expect)
	}
}

// ---------------------------------------------------------------------------
// BgAttemptStatus — pure logic tests
// ---------------------------------------------------------------------------

func TestBgAttemptStatus_IsTerminal(t *testing.T) {
	assert.True(t, BgAttemptStatusSucceeded.IsTerminal())
	assert.True(t, BgAttemptStatusFailed.IsTerminal())
	assert.True(t, BgAttemptStatusCanceled.IsTerminal())
	assert.True(t, BgAttemptStatusAbandoned.IsTerminal())
	assert.False(t, BgAttemptStatusDispatching.IsTerminal())
	assert.False(t, BgAttemptStatusRunning.IsTerminal())
	assert.False(t, BgAttemptStatusUnknown.IsTerminal())
}

// ---------------------------------------------------------------------------
// BgSessionStatus — pure logic tests
// ---------------------------------------------------------------------------

func TestBgSessionStatus_IsTerminal(t *testing.T) {
	assert.True(t, BgSessionStatusClosed.IsTerminal())
	assert.True(t, BgSessionStatusExpired.IsTerminal())
	assert.True(t, BgSessionStatusFailed.IsTerminal())
	assert.False(t, BgSessionStatusCreating.IsTerminal())
	assert.False(t, BgSessionStatusActive.IsTerminal())
	assert.False(t, BgSessionStatusIdle.IsTerminal())
}

// ---------------------------------------------------------------------------
// BgResponse — CRUD integration tests
// ---------------------------------------------------------------------------

func TestBgResponse_InsertAndGet(t *testing.T) {
	truncateBgTables(t)

	resp := &BgResponse{
		ResponseID:     "resp_test_001",
		RequestID:      "req_001",
		OrgID:          1,
		Model:          "bg.llm.chat.standard",
		Status:         BgResponseStatusAccepted,
		StatusVersion:  1,
		BillingMode:    "hosted",
		InputJSON:      `{"input":"hello"}`,
	}
	require.NoError(t, resp.Insert())
	assert.NotZero(t, resp.ID)

	// Retrieve by response_id
	found, err := GetBgResponseByResponseID("resp_test_001")
	require.NoError(t, err)
	assert.Equal(t, resp.ID, found.ID)
	assert.Equal(t, "bg.llm.chat.standard", found.Model)
	assert.Equal(t, BgResponseStatusAccepted, found.Status)
}

func TestBgResponse_IdempotencyKey(t *testing.T) {
	truncateBgTables(t)

	resp := &BgResponse{
		ResponseID:     "resp_idem_001",
		OrgID:          1,
		Model:          "bg.llm.chat.standard",
		Status:         BgResponseStatusAccepted,
		StatusVersion:  1,
		IdempotencyKey: "idem_key_123",
	}
	require.NoError(t, resp.Insert())

	// Should find by idempotency key
	found, err := GetBgResponseByIdempotencyKey(1, "idem_key_123")
	require.NoError(t, err)
	assert.Equal(t, "resp_idem_001", found.ResponseID)

	// Wrong org_id should not find
	_, err = GetBgResponseByIdempotencyKey(2, "idem_key_123")
	assert.Error(t, err)
}

func TestBgResponse_UniqueResponseID(t *testing.T) {
	truncateBgTables(t)

	resp1 := &BgResponse{
		ResponseID:    "resp_dup",
		Model:         "bg.llm.chat.standard",
		Status:        BgResponseStatusAccepted,
		StatusVersion: 1,
	}
	require.NoError(t, resp1.Insert())

	resp2 := &BgResponse{
		ResponseID:    "resp_dup",
		Model:         "bg.llm.chat.standard",
		Status:        BgResponseStatusAccepted,
		StatusVersion: 1,
	}
	err := resp2.Insert()
	assert.Error(t, err, "duplicate response_id should fail")
}

// ---------------------------------------------------------------------------
// BgResponse CAS — integration tests
// ---------------------------------------------------------------------------

func TestBgResponse_CASUpdateStatus_Win(t *testing.T) {
	truncateBgTables(t)

	resp := &BgResponse{
		ResponseID:    "resp_cas_win",
		Model:         "bg.llm.chat.standard",
		Status:        BgResponseStatusAccepted,
		StatusVersion: 1,
	}
	require.NoError(t, resp.Insert())

	// Transition accepted → queued
	resp.Status = BgResponseStatusQueued
	won, err := resp.CASUpdateStatus(BgResponseStatusAccepted, 1)
	require.NoError(t, err)
	assert.True(t, won)
	assert.Equal(t, 2, resp.StatusVersion)

	// Verify in DB
	found, err := GetBgResponseByResponseID("resp_cas_win")
	require.NoError(t, err)
	assert.Equal(t, BgResponseStatusQueued, found.Status)
	assert.Equal(t, 2, found.StatusVersion)
}

func TestBgResponse_CASUpdateStatus_Lose(t *testing.T) {
	truncateBgTables(t)

	resp := &BgResponse{
		ResponseID:    "resp_cas_lose",
		Model:         "bg.llm.chat.standard",
		Status:        BgResponseStatusFailed, // already terminal
		StatusVersion: 1,
	}
	require.NoError(t, resp.Insert())

	// Try CAS with wrong fromStatus
	resp.Status = BgResponseStatusSucceeded
	won, err := resp.CASUpdateStatus(BgResponseStatusRunning, 1) // wrong expected status
	require.NoError(t, err)
	assert.False(t, won)

	// DB should be unchanged
	found, err := GetBgResponseByResponseID("resp_cas_lose")
	require.NoError(t, err)
	assert.Equal(t, BgResponseStatusFailed, found.Status)
}

func TestBgResponse_CASUpdateStatus_ConcurrentWinner(t *testing.T) {
	truncateBgTables(t)

	resp := &BgResponse{
		ResponseID:    "resp_cas_race",
		Model:         "bg.video.generate.kling",
		Status:        BgResponseStatusRunning,
		StatusVersion: 1,
	}
	require.NoError(t, resp.Insert())

	const goroutines = 5
	wins := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			r := &BgResponse{
				ID:            resp.ID,
				Status:        BgResponseStatusSucceeded,
				OutputJSON:    `{"result":"done"}`,
				FinalizedAt:   time.Now().Unix(),
			}
			won, err := r.CASUpdateStatus(BgResponseStatusRunning, 1)
			if err == nil {
				wins[idx] = won
			}
		}(i)
	}
	wg.Wait()

	winCount := 0
	for _, w := range wins {
		if w {
			winCount++
		}
	}
	assert.Equal(t, 1, winCount, "exactly one goroutine should win the CAS")

	// Verify final state
	found, err := GetBgResponseByResponseID("resp_cas_race")
	require.NoError(t, err)
	assert.Equal(t, BgResponseStatusSucceeded, found.Status)
	assert.Equal(t, 2, found.StatusVersion)
}

// ---------------------------------------------------------------------------
// BgResponseAttempt — integration tests
// ---------------------------------------------------------------------------

func TestBgAttempt_InsertAndList(t *testing.T) {
	truncateBgTables(t)

	a1 := &BgResponseAttempt{
		AttemptID:  "att_001",
		ResponseID: "resp_att_test",
		AttemptNo:  1,
		AdapterName: "openai_gpt4",
		Status:     BgAttemptStatusDispatching,
	}
	a2 := &BgResponseAttempt{
		AttemptID:  "att_002",
		ResponseID: "resp_att_test",
		AttemptNo:  2,
		AdapterName: "anthropic_claude",
		Status:     BgAttemptStatusRunning,
	}
	require.NoError(t, a1.Insert())
	require.NoError(t, a2.Insert())

	attempts, err := GetBgAttemptsByResponseID("resp_att_test")
	require.NoError(t, err)
	assert.Len(t, attempts, 2)
	assert.Equal(t, 1, attempts[0].AttemptNo)
	assert.Equal(t, 2, attempts[1].AttemptNo)
}

func TestBgAttempt_GetPollable(t *testing.T) {
	truncateBgTables(t)

	now := time.Now().Unix()

	// Attempt 1: should be pollable (poll_after_at in the past)
	a1 := &BgResponseAttempt{
		AttemptID:   "att_poll_1",
		ResponseID:  "resp_poll",
		AttemptNo:   1,
		Status:      BgAttemptStatusRunning,
		PollAfterAt: now - 10,
	}
	// Attempt 2: not yet (poll_after_at in the future)
	a2 := &BgResponseAttempt{
		AttemptID:   "att_poll_2",
		ResponseID:  "resp_poll",
		AttemptNo:   2,
		Status:      BgAttemptStatusRunning,
		PollAfterAt: now + 1000,
	}
	// Attempt 3: terminal (should be excluded)
	a3 := &BgResponseAttempt{
		AttemptID:   "att_poll_3",
		ResponseID:  "resp_poll",
		AttemptNo:   3,
		Status:      BgAttemptStatusSucceeded,
		PollAfterAt: now - 10,
	}
	require.NoError(t, a1.Insert())
	require.NoError(t, a2.Insert())
	require.NoError(t, a3.Insert())

	pollable, err := GetPollableAttempts(now, 100)
	require.NoError(t, err)
	assert.Len(t, pollable, 1)
	assert.Equal(t, "att_poll_1", pollable[0].AttemptID)
}

func TestBgAttempt_CASUpdateStatus(t *testing.T) {
	truncateBgTables(t)

	a := &BgResponseAttempt{
		AttemptID:     "att_cas",
		ResponseID:    "resp_cas",
		AttemptNo:     1,
		Status:        BgAttemptStatusRunning,
		StatusVersion: 1,
	}
	require.NoError(t, a.Insert())

	a.Status = BgAttemptStatusSucceeded
	a.CompletedAt = time.Now().Unix()
	won, err := a.CASUpdateStatus(BgAttemptStatusRunning, 1)
	require.NoError(t, err)
	assert.True(t, won)
	assert.Equal(t, 2, a.StatusVersion)
}

// ---------------------------------------------------------------------------
// BgBillingRecord + BgUsageRecord + BgLedgerEntry — basic CRUD
// ---------------------------------------------------------------------------

func TestBgUsageRecord_Insert(t *testing.T) {
	truncateBgTables(t)

	u := &BgUsageRecord{
		UsageID:    "usg_001",
		ResponseID: "resp_001",
		OrgID:      1,
		Provider:   "openai",
		Model:      "bg.llm.chat.standard",
		Status:     BgUsageStatusPending,
	}
	require.NoError(t, u.Insert())
	assert.NotZero(t, u.ID)
}

func TestBgBillingRecord_Insert(t *testing.T) {
	truncateBgTables(t)

	b := &BgBillingRecord{
		BillingID:   "bill_001",
		ResponseID:  "resp_001",
		OrgID:       1,
		BillingMode: "hosted",
		TotalAmount: 2.034,
		ProviderCost: 1.48,
		PlatformMargin: 0.554,
		Currency:    "usd",
		Status:      BgBillingStatusPosted,
	}
	require.NoError(t, b.Insert())
	assert.NotZero(t, b.ID)
}

func TestBgLedgerEntry_Insert(t *testing.T) {
	truncateBgTables(t)

	l := &BgLedgerEntry{
		LedgerEntryID: "led_001",
		OrgID:         1,
		ResponseID:    "resp_001",
		EntryType:     "usage_charge",
		Direction:     "debit",
		Amount:        2.034,
		Currency:      "usd",
		BalanceAfter:  997.966,
	}
	require.NoError(t, l.Insert())
	assert.NotZero(t, l.ID)
}

// ---------------------------------------------------------------------------
// BgSession — integration tests
// ---------------------------------------------------------------------------

func TestBgSession_InsertAndGet(t *testing.T) {
	truncateBgTables(t)

	sess := &BgSession{
		SessionID:      "sess_001",
		ResponseID:     "resp_001",
		OrgID:          1,
		Model:          "bg.browser.session.standard",
		Status:         BgSessionStatusActive,
		IdleTimeoutSec: 300,
		MaxDurationSec: 3600,
		ExpiresAt:      time.Now().Unix() + 3600,
		LastActionAt:   time.Now().Unix(),
	}
	require.NoError(t, sess.Insert())

	found, err := GetBgSessionBySessionID("sess_001")
	require.NoError(t, err)
	assert.Equal(t, "bg.browser.session.standard", found.Model)
	assert.Equal(t, BgSessionStatusActive, found.Status)
}

func TestBgSession_GetIdleSessions(t *testing.T) {
	truncateBgTables(t)

	now := time.Now().Unix()

	// Session idle for 600s, timeout is 300s → should appear
	s1 := &BgSession{
		SessionID:      "sess_idle_1",
		ResponseID:     "resp_idle",
		OrgID:          1,
		Model:          "bg.browser.session.standard",
		Status:         BgSessionStatusActive,
		IdleTimeoutSec: 300,
		LastActionAt:   now - 600,
	}
	// Session idle for 100s, timeout is 300s → should NOT appear
	s2 := &BgSession{
		SessionID:      "sess_idle_2",
		ResponseID:     "resp_idle2",
		OrgID:          1,
		Model:          "bg.browser.session.standard",
		Status:         BgSessionStatusActive,
		IdleTimeoutSec: 300,
		LastActionAt:   now - 100,
	}
	require.NoError(t, s1.Insert())
	require.NoError(t, s2.Insert())

	idle, err := GetIdleSessions(now, 100)
	require.NoError(t, err)
	assert.Len(t, idle, 1)
	assert.Equal(t, "sess_idle_1", idle[0].SessionID)
}

func TestBgSession_GetExpiredSessions(t *testing.T) {
	truncateBgTables(t)

	now := time.Now().Unix()

	// Expired session
	s1 := &BgSession{
		SessionID:  "sess_exp_1",
		ResponseID: "resp_exp",
		OrgID:      1,
		Model:      "bg.browser.session.standard",
		Status:     BgSessionStatusActive,
		ExpiresAt:  now - 100,
	}
	// Not expired
	s2 := &BgSession{
		SessionID:  "sess_exp_2",
		ResponseID: "resp_exp2",
		OrgID:      1,
		Model:      "bg.browser.session.standard",
		Status:     BgSessionStatusActive,
		ExpiresAt:  now + 1000,
	}
	require.NoError(t, s1.Insert())
	require.NoError(t, s2.Insert())

	expired, err := GetExpiredSessions(now, 100)
	require.NoError(t, err)
	assert.Len(t, expired, 1)
	assert.Equal(t, "sess_exp_1", expired[0].SessionID)
}

// ---------------------------------------------------------------------------
// BgSessionAction — basic CRUD
// ---------------------------------------------------------------------------

func TestBgSessionAction_Insert(t *testing.T) {
	truncateBgTables(t)

	action := &BgSessionAction{
		ActionID:   "act_001",
		SessionID:  "sess_001",
		ActionType: "navigate",
		InputJSON:  `{"url":"https://example.com"}`,
		Status:     "running",
		StartedAt:  time.Now().Unix(),
	}
	require.NoError(t, action.Insert())
	assert.NotZero(t, action.ID)
}

// ---------------------------------------------------------------------------
// BgWebhookEvent — integration tests
// ---------------------------------------------------------------------------

func TestBgWebhookEvent_InsertAndGetPending(t *testing.T) {
	truncateBgTables(t)

	now := time.Now().Unix()

	e1 := &BgWebhookEvent{
		EventID:        "evt_001",
		ResponseID:     "resp_001",
		OrgID:          1,
		EventType:      "response.succeeded",
		PayloadJSON:    `{"id":"resp_001"}`,
		DeliveryStatus: "pending",
	}
	e2 := &BgWebhookEvent{
		EventID:        "evt_002",
		ResponseID:     "resp_002",
		OrgID:          1,
		EventType:      "response.failed",
		PayloadJSON:    `{"id":"resp_002"}`,
		DeliveryStatus: "delivered", // already delivered
	}
	require.NoError(t, e1.Insert())
	require.NoError(t, e2.Insert())

	pending, err := GetPendingWebhookEvents(now, 100)
	require.NoError(t, err)
	assert.Len(t, pending, 1)
	assert.Equal(t, "evt_001", pending[0].EventID)
}
