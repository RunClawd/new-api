package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetPolicyEngineState() {
	policyMu.Lock()
	capabilityPolicies = nil
	routingPolicies = nil
	policyMu.Unlock()
	cacheInitialized.Store(false)
}

func mockCacheInit(policies []model.BgCapabilityPolicy) {
	policyMu.Lock()
	capabilityPolicies = policies
	policyMu.Unlock()
	cacheInitialized.Store(true)
}

func TestMatchCapabilityPattern(t *testing.T) {
	assert.True(t, matchCapabilityPattern("bg.llm.*", "bg.llm.chat.standard"))
	assert.False(t, matchCapabilityPattern("bg.llm.*", "bg.video.generate"))
	assert.True(t, matchCapabilityPattern("bg.sandbox.python", "bg.sandbox.python"))
	assert.False(t, matchCapabilityPattern("bg.sandbox.python", "bg.sandbox.python2"))
	assert.True(t, matchCapabilityPattern("bg.*", "bg.llm.anthropic.haiku"))
}

func TestCapabilitySpecificity(t *testing.T) {
	assert.Equal(t, 4, capabilitySpecificity("bg.llm.chat.standard"))
	assert.Equal(t, 2, capabilitySpecificity("bg.llm.*"))
	assert.Equal(t, 1, capabilitySpecificity("bg.*"))
}

func TestEvaluateCapabilityAccess_Cases(t *testing.T) {
	tests := []struct {
		name       string
		orgID      int
		projectID  int
		apiKeyID   int
		capability string
		policies   []model.BgCapabilityPolicy
		wantAllow  bool
		wantErr    bool
	}{
		{
			name:       "Case 1: No policy -> allow (default)",
			capability: "bg.any.thing",
			wantAllow:  true,
		},
		{
			name:       "Case 2: Platform deny -> deny",
			capability: "bg.sandbox.python",
			policies: []model.BgCapabilityPolicy{
				{Scope: "platform", CapabilityPattern: "bg.sandbox.*", Action: "deny", Enforced: false, Status: "active"},
			},
			wantAllow: false,
		},
		{
			name:       "Case 3: Platform deny (enforced=false) + org allow -> allow",
			orgID:      1,
			capability: "bg.sandbox.python",
			policies: []model.BgCapabilityPolicy{
				{Scope: "platform", CapabilityPattern: "bg.sandbox.*", Action: "deny", Enforced: false, Status: "active"},
				{Scope: "org", ScopeID: 1, CapabilityPattern: "bg.sandbox.*", Action: "allow", Status: "active"},
			},
			wantAllow: true,
		},
		{
			name:       "Case 4: Platform deny (enforced=true) + org allow -> deny",
			orgID:      1,
			capability: "bg.sandbox.python",
			policies: []model.BgCapabilityPolicy{
				{Scope: "platform", CapabilityPattern: "bg.sandbox.*", Action: "deny", Enforced: true, Status: "active"},
				{Scope: "org", ScopeID: 1, CapabilityPattern: "bg.sandbox.*", Action: "allow", Status: "active"},
			},
			wantAllow: false,
		},
		{
			name:       "Case 5: Org deny + project allow (more specific) -> allow",
			orgID:      1,
			projectID:  10,
			capability: "bg.sandbox.session.standard",
			policies: []model.BgCapabilityPolicy{
				{Scope: "org", ScopeID: 1, CapabilityPattern: "bg.sandbox.*", Action: "deny", Status: "active"},
				{Scope: "project", ScopeID: 10, CapabilityPattern: "bg.sandbox.session.standard", Action: "allow", Status: "active"},
			},
			wantAllow: true,
		},
		{
			name:       "Case 6: Key deny -> deny",
			apiKeyID:   100,
			capability: "bg.llm.chat",
			policies: []model.BgCapabilityPolicy{
				{Scope: "key", ScopeID: 100, CapabilityPattern: "bg.llm.*", Action: "deny", Status: "active"},
				{Scope: "project", ScopeID: 10, CapabilityPattern: "bg.llm.*", Action: "allow", Status: "active"}, // Key is higher priority
			},
			wantAllow: false,
		},
		{
			name:       "Case 7: Same priority and specificity (deny vs allow) -> deny wins",
			orgID:      1,
			capability: "bg.llm.chat",
			policies: []model.BgCapabilityPolicy{
				{Scope: "org", ScopeID: 1, CapabilityPattern: "bg.llm.*", Action: "allow", Priority: 0, Status: "active"},
				{Scope: "org", ScopeID: 1, CapabilityPattern: "bg.llm.*", Action: "deny", Priority: 0, Status: "active"},
			},
			wantAllow: false,
		},
		{
			name:       "Case 7b: Same level, different priority",
			orgID:      1,
			capability: "bg.llm.chat",
			policies: []model.BgCapabilityPolicy{
				{Scope: "org", ScopeID: 1, CapabilityPattern: "bg.llm.*", Action: "allow", Priority: 10, Status: "active"},
				{Scope: "org", ScopeID: 1, CapabilityPattern: "bg.llm.*", Action: "deny", Priority: 0, Status: "active"},
			},
			wantAllow: true,
		},
		{
			name:       "Case 7c: Same level, same priority, different specificity",
			orgID:      1,
			capability: "bg.llm.chat",
			policies: []model.BgCapabilityPolicy{
				{Scope: "org", ScopeID: 1, CapabilityPattern: "bg.llm.chat", Action: "allow", Priority: 0, Status: "active"}, // 3 segments
				{Scope: "org", ScopeID: 1, CapabilityPattern: "bg.llm.*", Action: "deny", Priority: 0, Status: "active"},     // 2 segments
			},
			wantAllow: true,
		},
		{
			name:       "Case 7d: platform allow bg.* + platform deny bg.sandbox.* -> chat matched",
			capability: "bg.llm.chat",
			policies: []model.BgCapabilityPolicy{
				{Scope: "platform", CapabilityPattern: "bg.*", Action: "allow", Status: "active"},
				{Scope: "platform", CapabilityPattern: "bg.sandbox.*", Action: "deny", Status: "active"},
			},
			wantAllow: true, // Only bg.* matches, which is allow
		},
		{
			name:       "Case 7e: platform allow bg.* + platform deny bg.sandbox.* -> sandbox matched",
			capability: "bg.sandbox.python",
			policies: []model.BgCapabilityPolicy{
				{Scope: "platform", CapabilityPattern: "bg.*", Action: "allow", Status: "active"},
				{Scope: "platform", CapabilityPattern: "bg.sandbox.*", Action: "deny", Status: "active"}, // More specific
			},
			wantAllow: false,
		},
		{
			name:       "Case 9: disabled policy ignored",
			capability: "bg.sandbox.python",
			policies: []model.BgCapabilityPolicy{
				{Scope: "platform", CapabilityPattern: "bg.sandbox.*", Action: "deny", Status: "disabled"},
			},
			wantAllow: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockCacheInit(tc.policies)
			allow, _, err := EvaluateCapabilityAccess(tc.orgID, tc.projectID, tc.apiKeyID, tc.capability)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.wantAllow, allow)
			}
		})
	}
}

func TestEvaluateCapabilityAccess_CacheNotInitialized(t *testing.T) {
	resetPolicyEngineState()
	allow, _, err := EvaluateCapabilityAccess(1, 1, 1, "bg.llm.chat")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "policy cache not initialized")
	assert.False(t, allow) // Fail-open prevented
}

func TestModelValidate(t *testing.T) {
	// Enforced != deny
	p0 := model.BgCapabilityPolicy{Scope: "platform", ScopeID: 0, Action: "allow", Enforced: true, CapabilityPattern: "bg.*", Status: "active"}
	assert.ErrorContains(t, p0.Validate(), "enforced=true is only allowed with action='deny'")

	// Enforced != platform
	p1 := model.BgCapabilityPolicy{Scope: "org", ScopeID: 1, Action: "deny", Enforced: true, CapabilityPattern: "bg.*", Status: "active"}
	assert.ErrorContains(t, p1.Validate(), "enforced=true is only allowed on platform scope")

	// MaxConcurrency > 0
	p2 := model.BgCapabilityPolicy{Scope: "platform", Action: "allow", MaxConcurrency: 1, CapabilityPattern: "bg.*", Status: "active"}
	assert.ErrorContains(t, p2.Validate(), "max_concurrency is reserved")

	// Invalid wildcard
	p3 := model.BgCapabilityPolicy{Scope: "platform", Action: "allow", CapabilityPattern: "bg.*.test", Status: "active"}
	assert.ErrorContains(t, p3.Validate(), "wildcard '*' must be the last segment")

	// Invalid status
	p4 := model.BgCapabilityPolicy{Scope: "platform", Action: "allow", CapabilityPattern: "bg.*", Status: "hello"}
	assert.ErrorContains(t, p4.Validate(), "status must be 'active' or 'disabled'")
}

func TestCacheRetainsOldOnRefreshFailure(t *testing.T) {
	// 1. Manually inject initial cache
	mockCacheInit([]model.BgCapabilityPolicy{
		{Scope: "platform", CapabilityPattern: "bg.sandbox.*", Action: "deny", Status: "active"},
	})
	allow, _, _ := EvaluateCapabilityAccess(0, 0, 0, "bg.sandbox.python")
	assert.False(t, allow)

	// 2. RefreshPolicyCache fails (DB not initialized in test env)
	err := RefreshPolicyCache()
	assert.Error(t, err)

	// 3. Old cache retained, deny still applies
	assert.True(t, cacheInitialized.Load())
	allow2, _, _ := EvaluateCapabilityAccess(0, 0, 0, "bg.sandbox.python")
	assert.False(t, allow2)
}

// TestDeleteThenInvalidateCache requires a live DB to verify the full
// delete -> InvalidatePolicyCache -> re-query chain. Deferred to Day 5
// integration tests (see PHASE12_PLAN.md acceptance criteria #14).
