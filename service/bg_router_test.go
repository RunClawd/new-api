package service

import (
	"math/rand"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/basegate"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockAdapter struct {
	name         string
	capabilities []relaycommon.CapabilityBinding
}

func (m *mockAdapter) Name() string { return m.name }
func (m *mockAdapter) DescribeCapabilities() []relaycommon.CapabilityBinding {
	return m.capabilities
}
func (m *mockAdapter) Validate(req *relaycommon.CanonicalRequest) *relaycommon.ValidationResult {
	return nil
}
func (m *mockAdapter) Invoke(req *relaycommon.CanonicalRequest) (*relaycommon.AdapterResult, error) {
	return nil, nil
}
func (m *mockAdapter) Poll(id string) (*relaycommon.AdapterResult, error) { return nil, nil }
func (m *mockAdapter) Cancel(id string) (*relaycommon.AdapterResult, error) { return nil, nil }
func (m *mockAdapter) Stream(req *relaycommon.CanonicalRequest) (<-chan relaycommon.SSEEvent, error) {
	return nil, nil
}

var mockAdaptersRegistered bool

func registerMockAdapters() {
	if mockAdaptersRegistered {
		return
	}
	mockAdaptersRegistered = true
	// Capabilities used for testing to avoid bleed over
	a1 := &mockAdapter{
		name: "ad-fixed-A",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "test.cap.fixed", Weight: 10},
		},
	}
	a2 := &mockAdapter{
		name: "ad-fixed-B",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "test.cap.fixed", Weight: 20},
		},
	}
	
	a3 := &mockAdapter{
		name: "ad-weight-A",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "test.cap.weight", Weight: 50},
		},
	}
	a4 := &mockAdapter{
		name: "ad-weight-B",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "test.cap.weight", Weight: 50},
		},
	}
	
	a5 := &mockAdapter{
		name: "ad-pb-primary",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "test.cap.pb", Weight: 10},
		},
	}
	a6 := &mockAdapter{
		name: "ad-pb-fallback",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "test.cap.pb", Weight: 10},
		},
	}

	a7 := &mockAdapter{
		name: "my-byo-adapter",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "test.cap.byo", Weight: 10, Provider: "my-byo-provider"},
		},
	}
	a8 := &mockAdapter{
		name: "default-adapter",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "test.cap.byo", Weight: 10},
		},
	}

	// Legacy matches setup
	a9 := &mockAdapter{
		name: "legacy_task_suno",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "test.cap.legacy", Weight: 10},
		},
	}
	a10 := &mockAdapter{
		name: "legacy_task_kling",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "test.cap.legacy", Weight: 10},
		},
	}
	
	a11 := &mockAdapter{
		name: "empty-cap-adapter",
		capabilities: []relaycommon.CapabilityBinding{
			{CapabilityPattern: "test.cap.empty", Weight: 10},
		},
	}

	adapters := []basegate.ProviderAdapter{a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11}
	for _, a := range adapters {
		// Register safely
		basegate.RegisterAdapter(a)
	}
}

func setupRoutingTest(policies []model.BgRoutingPolicy) {
	resetPolicyEngineState()
	policyMu.Lock()
	routingPolicies = policies
	policyMu.Unlock()
	cacheInitialized.Store(true)
}

func getAdapterNames(adapters []basegate.ProviderAdapter) []string {
	var names []string
	for _, a := range adapters {
		names = append(names, a.Name())
	}
	return names
}

func getResolvedAdapterNames(adapters []basegate.ResolvedAdapter) []string {
	var names []string
	for _, a := range adapters {
		names = append(names, a.Adapter.Name())
	}
	return names
}

func TestResolveRoute_FixedStrategy(t *testing.T) {
	registerMockAdapters()
	p := []model.BgRoutingPolicy{
		{Scope: "platform", CapabilityPattern: "test.cap.fixed", Strategy: "fixed", RulesJSON: `{"adapter_name": "ad-fixed-B"}`, Status: "active"},
	}
	setupRoutingTest(p)

	adapters, err := ResolveRoute(0, 0, 0, "test.cap.fixed")
	require.NoError(t, err)
	require.Len(t, adapters, 1)
	assert.Equal(t, "ad-fixed-B", adapters[0].Adapter.Name())

	// Test case 3: non-existent adapter
	pBad := []model.BgRoutingPolicy{
		{Scope: "platform", CapabilityPattern: "test.cap.fixed", Strategy: "fixed", RulesJSON: `{"adapter_name": "missing-adapter"}`, Status: "active"},
	}
	setupRoutingTest(pBad)
	adaptersBad, errBad := ResolveRoute(0, 0, 0, "test.cap.fixed")
	require.Error(t, errBad)
	assert.Contains(t, errBad.Error(), "fixed adapter missing-adapter not found")
	assert.Nil(t, adaptersBad)
}

func TestResolveRoute_WeightedStrategy(t *testing.T) {
	registerMockAdapters()
	p := []model.BgRoutingPolicy{
		{Scope: "platform", CapabilityPattern: "test.cap.weight", Strategy: "weighted", RulesJSON: `{"weights": {"ad-weight-A": 100, "ad-weight-B": 0}}`, Status: "active"},
	}
	setupRoutingTest(p)

	// Since weight B is 0, it should only return A
	adapters, err := ResolveRoute(0, 0, 0, "test.cap.weight")
	require.NoError(t, err)
	require.Len(t, adapters, 1)
	assert.Equal(t, "ad-weight-A", adapters[0].Adapter.Name())

	// Both weights > 0, deterministic test using fixed seed
	p2 := []model.BgRoutingPolicy{
		{Scope: "platform", CapabilityPattern: "test.cap.weight", Strategy: "weighted", RulesJSON: `{"weights": {"ad-weight-A": 50, "ad-weight-B": 50}}`, Status: "active"},
	}
	setupRoutingTest(p2)
	fixedRand := rand.New(rand.NewSource(1)) // deterministic setup
	adapters2, err2 := resolveRouteWithRand(fixedRand, 0, 0, 0, "test.cap.weight")
	require.NoError(t, err2)
	require.Len(t, adapters2, 2)
	// With seed=1, the weighted shuffle produces a deterministic order.
	// Capture and assert exact order to verify determinism across runs.
	firstRun := getResolvedAdapterNames(adapters2)

	// Run again with same seed — must produce identical order
	fixedRand2 := rand.New(rand.NewSource(1))
	adapters3, err3 := resolveRouteWithRand(fixedRand2, 0, 0, 0, "test.cap.weight")
	require.NoError(t, err3)
	require.Len(t, adapters3, 2)
	secondRun := getResolvedAdapterNames(adapters3)

	assert.Equal(t, firstRun, secondRun, "same seed must produce identical adapter ordering")
}

func TestResolveRoute_PrimaryBackupStrategy(t *testing.T) {
	registerMockAdapters()
	p := []model.BgRoutingPolicy{
		{Scope: "platform", CapabilityPattern: "test.cap.pb", Strategy: "primary_backup", RulesJSON: `{"primary": "ad-pb-primary", "fallback": ["ad-pb-fallback"]}`, Status: "active"},
	}
	setupRoutingTest(p)

	adapters, err := ResolveRoute(0, 0, 0, "test.cap.pb")
	require.NoError(t, err)
	require.Len(t, adapters, 2)
	assert.Equal(t, "ad-pb-primary", adapters[0].Adapter.Name())
	assert.Equal(t, "ad-pb-fallback", adapters[1].Adapter.Name())
}

func TestResolveRoute_BYOFirstStrategy(t *testing.T) {
	registerMockAdapters()
	p := []model.BgRoutingPolicy{
		{Scope: "platform", CapabilityPattern: "test.cap.byo", Strategy: "byo_first", RulesJSON: `{"provider": "my-byo-provider", "fallback_to_hosted": true}`, Status: "active"},
	}
	setupRoutingTest(p)

	fixedRand := rand.New(rand.NewSource(1))
	adapters, err := resolveRouteWithRand(fixedRand, 0, 0, 0, "test.cap.byo")
	require.NoError(t, err)

	// Without a stored BYO credential for org 0, the BYO adapter is skipped
	// to prevent billing mismatch (billing as BYO while using hosted credentials).
	// Only the hosted fallback adapter should be returned.
	require.Len(t, adapters, 1)
	assert.Equal(t, "hosted", adapters[0].BillingSource)
}

func TestResolveRoute_Scopes(t *testing.T) {
	registerMockAdapters()
	p := []model.BgRoutingPolicy{
		{Scope: "org", ScopeID: 1, CapabilityPattern: "test.cap.pb", Strategy: "primary_backup", RulesJSON: `{"primary": "ad-pb-primary", "fallback": []}`, Status: "active"},
		{Scope: "project", ScopeID: 10, CapabilityPattern: "test.cap.pb", Strategy: "primary_backup", RulesJSON: `{"primary": "ad-pb-fallback", "fallback": []}`, Status: "active"},
	}
	setupRoutingTest(p)

	// Since project is more specific, project policy wins: ad-pb-fallback
	adapters, err := ResolveRoute(1, 10, 0, "test.cap.pb")
	require.NoError(t, err)
	require.Len(t, adapters, 1)
	assert.Equal(t, "ad-pb-fallback", adapters[0].Adapter.Name())
}

func TestResolveRoute_NoPolicyFallback(t *testing.T) {
	registerMockAdapters()
	// Test case 1: no routing policy, falls back to LookupAdapters
	setupRoutingTest([]model.BgRoutingPolicy{})

	adapters, err := ResolveRoute(0, 0, 0, "test.cap.fixed")
	require.NoError(t, err)
	require.Len(t, adapters, 2)
	// Basegate's LookupAdapters uses completely random seeds, so we only verify elements exist
	names := getResolvedAdapterNames(adapters)
	assert.Contains(t, names, "ad-fixed-A")
	assert.Contains(t, names, "ad-fixed-B")
}

func TestResolveRoute_LegacyWrapperMatch(t *testing.T) {
	registerMockAdapters()
	p := []model.BgRoutingPolicy{
		{Scope: "platform", CapabilityPattern: "test.cap.legacy", Strategy: "fixed", RulesJSON: `{"adapter_name": "legacy_task_suno"}`, Status: "active"},
	}
	setupRoutingTest(p)

	adapters, err := ResolveRoute(0, 0, 0, "test.cap.legacy")
	require.NoError(t, err)
	require.Len(t, adapters, 1)
	assert.Equal(t, "legacy_task_suno", adapters[0].Adapter.Name())
}

func TestResolveRoute_BYOFirstNoBYOMatch(t *testing.T) {
	registerMockAdapters()
	// Pattern that won't match any adapter name → should fallback to LookupAdapters
	p := []model.BgRoutingPolicy{
		{Scope: "platform", CapabilityPattern: "test.cap.byo", Strategy: "byo_first", RulesJSON: `{"provider": "nonexistent-provider", "fallback_to_hosted": true}`, Status: "active"},
	}
	setupRoutingTest(p)

	fixedRand := rand.New(rand.NewSource(1))
	adapters, err := resolveRouteWithRand(fixedRand, 0, 0, 0, "test.cap.byo")
	require.NoError(t, err)
	// No BYO match → all adapters come from LookupAdapters fallback
	names := getResolvedAdapterNames(adapters)
	assert.Contains(t, names, "my-byo-adapter")
	assert.Contains(t, names, "default-adapter")
}

func TestResolveRoute_PrimaryBackupMissingPrimary(t *testing.T) {
	registerMockAdapters()
	p := []model.BgRoutingPolicy{
		{Scope: "platform", CapabilityPattern: "test.cap.pb", Strategy: "primary_backup", RulesJSON: `{"primary": "nonexistent-adapter", "fallback": ["ad-pb-fallback"]}`, Status: "active"},
	}
	setupRoutingTest(p)

	adapters, err := ResolveRoute(0, 0, 0, "test.cap.pb")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "primary adapter nonexistent-adapter not found")
	assert.Nil(t, adapters)
}

func TestListAdaptersUnordered(t *testing.T) {
	registerMockAdapters()
	adapters := basegate.ListAdaptersUnordered("test.cap.fixed")
	require.Len(t, adapters, 2)
	names := getAdapterNames(adapters)
	assert.Contains(t, names, "ad-fixed-A")
	assert.Contains(t, names, "ad-fixed-B")

	// Call twice to verify deterministic (not shuffled)
	adapters2 := basegate.ListAdaptersUnordered("test.cap.fixed")
	require.Len(t, adapters2, 2)
	for i := range adapters {
		assert.Equal(t, adapters[i].Name(), adapters2[i].Name(), "ListAdaptersUnordered should return same order on repeated calls")
	}
}
