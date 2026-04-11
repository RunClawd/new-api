package service

import (
	"fmt"
	"math/rand"
	"path/filepath"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/basegate"
)

// ResolveRoute returns an ordered list of ProviderAdapters for a given capability,
// applying any matching routing policies for the tenant.
//
// Resolution:
//  1. Find the most specific matching routing policy (key > project > org > platform)
//  2. If found: get unordered adapters via ListAdaptersUnordered(), apply strategy
//  3. If not found: fallback to basegate.LookupAdapters() (original weighted shuffle)
func ResolveRoute(orgID, projectID, apiKeyID int, capabilityName string) ([]basegate.ProviderAdapter, error) {
	// rand.Int63() is goroutine-safe; per-request rng avoids data race on shared *rand.Rand
	rng := rand.New(rand.NewSource(rand.Int63()))
	return resolveRouteWithRand(rng, orgID, projectID, apiKeyID, capabilityName)
}

func resolveRouteWithRand(rng *rand.Rand, orgID, projectID, apiKeyID int, capabilityName string) ([]basegate.ProviderAdapter, error) {
	if !cacheInitialized.Load() {
		return nil, fmt.Errorf("policy cache not initialized")
	}

	policyMu.RLock()
	var winning *model.BgRoutingPolicy
	var maxPriority int
	var maxSpecificity int

	scopesToCheck := []struct {
		name string
		id   int
	}{
		{"key", apiKeyID},
		{"project", projectID},
		{"org", orgID},
		{"platform", 0},
	}

	for _, scope := range scopesToCheck {
		var matches []model.BgRoutingPolicy
		for i := range routingPolicies {
			p := routingPolicies[i]
			if p.Status != "active" {
				continue
			}
			if p.Scope == scope.name && p.ScopeID == scope.id {
				if matchCapabilityPattern(p.CapabilityPattern, capabilityName) {
					matches = append(matches, p)
				}
			}
		}

		if len(matches) > 0 {
			for i := range matches {
				m := matches[i]
				mSpec := capabilitySpecificity(m.CapabilityPattern)

				if winning == nil {
					winning = &matches[i]
					maxPriority = m.Priority
					maxSpecificity = mSpec
					continue
				}

				if m.Priority > maxPriority {
					winning = &matches[i]
					maxPriority = m.Priority
					maxSpecificity = mSpec
					continue
				} else if m.Priority < maxPriority {
					continue
				}

				if mSpec > maxSpecificity {
					winning = &matches[i]
					maxSpecificity = mSpec
					continue
				}
			}
			break
		}
	}

	var policy *model.BgRoutingPolicy
	if winning != nil {
		pCopy := *winning
		policy = &pCopy
	}
	policyMu.RUnlock()

	// If no policy found, fallback to original weighted shuffle behavior
	if policy == nil {
		return basegate.LookupAdapters(capabilityName), nil
	}

	// Fetch available adapters
	allAdapters := basegate.ListAdaptersUnordered(capabilityName)
	if len(allAdapters) == 0 {
		return nil, nil // No adapters locally registered
	}

	// Apply Strategy
	switch policy.Strategy {
	case "fixed":
		var r model.FixedRules
		if err := common.Unmarshal([]byte(policy.RulesJSON), &r); err != nil {
			common.SysError(fmt.Sprintf("router: failed to parse fixed rules for policy %d: %v", policy.ID, err))
			return basegate.LookupAdapters(capabilityName), nil
		}
		return applyFixedStrategy(r, allAdapters)

	case "weighted":
		var r model.WeightedRules
		if err := common.Unmarshal([]byte(policy.RulesJSON), &r); err != nil {
			common.SysError(fmt.Sprintf("router: failed to parse weighted rules for policy %d: %v", policy.ID, err))
			return basegate.LookupAdapters(capabilityName), nil
		}
		return applyWeightedStrategy(rng, r, allAdapters), nil

	case "primary_backup":
		var r model.PrimaryBackupRules
		if err := common.Unmarshal([]byte(policy.RulesJSON), &r); err != nil {
			common.SysError(fmt.Sprintf("router: failed to parse primary_backup rules for policy %d: %v", policy.ID, err))
			return basegate.LookupAdapters(capabilityName), nil
		}
		return applyPrimaryBackupStrategy(r, allAdapters)

	case "byo_first":
		var r model.BYOFirstRules
		if err := common.Unmarshal([]byte(policy.RulesJSON), &r); err != nil {
			common.SysError(fmt.Sprintf("router: failed to parse byo_first rules for policy %d: %v", policy.ID, err))
			return basegate.LookupAdapters(capabilityName), nil
		}
		return applyBYOFirstStrategy(rng, r, capabilityName, allAdapters), nil

	default:
		common.SysError(fmt.Sprintf("router: unknown strategy %s for policy %d", policy.Strategy, policy.ID))
		return basegate.LookupAdapters(capabilityName), nil
	}
}

func applyFixedStrategy(rules model.FixedRules, allAdapters []basegate.ProviderAdapter) ([]basegate.ProviderAdapter, error) {
	for _, a := range allAdapters {
		if a.Name() == rules.AdapterName {
			return []basegate.ProviderAdapter{a}, nil
		}
	}
	return nil, fmt.Errorf("fixed adapter %s not found", rules.AdapterName)
}

func applyWeightedStrategy(rng *rand.Rand, rules model.WeightedRules, allAdapters []basegate.ProviderAdapter) []basegate.ProviderAdapter {
	type cand struct {
		adapter basegate.ProviderAdapter
		weight  int
	}
	var candidates []cand

	for _, a := range allAdapters {
		if w, ok := rules.Weights[a.Name()]; ok && w > 0 {
			candidates = append(candidates, cand{adapter: a, weight: w})
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	result := make([]basegate.ProviderAdapter, 0, len(candidates))
	remaining := make([]cand, len(candidates))
	copy(remaining, candidates)

	for len(remaining) > 0 {
		sum := 0
		for _, e := range remaining {
			sum += e.weight
		}

		pick := rng.Intn(sum)
		cumulative := 0
		chosen := 0
		for i, e := range remaining {
			cumulative += e.weight
			if pick < cumulative {
				chosen = i
				break
			}
		}

		result = append(result, remaining[chosen].adapter)
		remaining = append(remaining[:chosen], remaining[chosen+1:]...)
	}

	return result
}

func applyPrimaryBackupStrategy(rules model.PrimaryBackupRules, allAdapters []basegate.ProviderAdapter) ([]basegate.ProviderAdapter, error) {
	adapterMap := make(map[string]basegate.ProviderAdapter)
	for _, a := range allAdapters {
		adapterMap[a.Name()] = a
	}

	primaryCand, ok := adapterMap[rules.Primary]
	if !ok {
		return nil, fmt.Errorf("primary adapter %s not found", rules.Primary)
	}
	finalResult := []basegate.ProviderAdapter{primaryCand}

	for _, fname := range rules.Fallback {
		if fbCand, ok := adapterMap[fname]; ok {
			// Ensure we don't duplicate primary if randomly included in fallback
			if fbCand.Name() != rules.Primary {
				finalResult = append(finalResult, fbCand)
			}
		}
	}

	return finalResult, nil
}

func applyBYOFirstStrategy(rng *rand.Rand, rules model.BYOFirstRules, capabilityName string, allAdapters []basegate.ProviderAdapter) []basegate.ProviderAdapter {
	var byo []basegate.ProviderAdapter
	byoSet := make(map[string]bool)

	for _, a := range allAdapters {
		matched, _ := filepath.Match(rules.BYOAdapterPattern, a.Name())
		if matched {
			byo = append(byo, a)
			byoSet[a.Name()] = true
		}
	}

	// Internal shuffling for BYO adapters if multiple match
	if len(byo) > 1 {
		rng.Shuffle(len(byo), func(i, j int) {
			byo[i], byo[j] = byo[j], byo[i]
		})
	}

	// Fallback is implicitly LookupAdapters() which provides weighted random sorting
	fallbackAdapters := basegate.LookupAdapters(capabilityName)
	var rest []basegate.ProviderAdapter

	for _, fa := range fallbackAdapters {
		if !byoSet[fa.Name()] {
			rest = append(rest, fa)
		}
	}

	return append(byo, rest...)
}
