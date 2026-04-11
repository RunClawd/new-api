package service

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// ErrCapabilityDenied is a sentinel error for capability policy denial.
// Use errors.Is(err, ErrCapabilityDenied) in controller to map to HTTP 403.
var ErrCapabilityDenied = fmt.Errorf("capability_denied")

var (
	policyMu           sync.RWMutex
	capabilityPolicies []model.BgCapabilityPolicy
	routingPolicies    []model.BgRoutingPolicy
	cacheInitialized   atomic.Bool // Prevents fail-open if initial DB load fails
)

// matchCapabilityPattern matches a capability name against a pattern.
// Pattern rules:
//   - Exact match: "bg.llm.chat.standard" matches "bg.llm.chat.standard"
//   - Wildcard suffix: "bg.llm.*" matches "bg.llm.chat.standard", "bg.llm.reasoning.pro", etc.
//   - The wildcard "*" must appear as the last segment and crosses all boundaries.
func matchCapabilityPattern(pattern, capabilityName string) bool {
	patternParts := strings.Split(pattern, ".")
	nameParts := strings.Split(capabilityName, ".")

	for i, pp := range patternParts {
		if pp == "*" {
			return true // wildcard matches all remaining segments
		}
		if i >= len(nameParts) {
			return false // pattern has more non-wildcard segments than name
		}
		if pp != nameParts[i] {
			return false // segment mismatch
		}
	}
	return len(patternParts) == len(nameParts)
}

// capabilitySpecificity returns the number of non-wildcard segments.
// Higher = more specific. "bg.llm.chat.standard" → 4, "bg.llm.*" → 2, "bg.*" → 1
func capabilitySpecificity(pattern string) int {
	parts := strings.Split(pattern, ".")
	count := 0
	for _, p := range parts {
		if p != "*" {
			count++
		}
	}
	return count
}

// EvaluateCapabilityAccess checks whether the given identity is allowed to use a capability.
// Called in controller layer BEFORE mode dispatch (one check covers sync/async/stream).
// Returns (allowed bool, reason string, err error).
//
// Evaluation algorithm:
//
//   Phase 0 — Initialization check:
//     If cacheInitialized.Load() is false, return (false, "", errorEngineNotReady).
//     This prevents fail-open behavior if the DB is unavailable during early startup.
//
//   Phase A — Enforced platform deny (short-circuit):
//     Scan all platform-level policies with enforced=true and action=deny.
//     If any matches the capability: return deny immediately. No override possible.
//
//   Phase B — Hierarchical evaluation (from highest to lowest priority):
//     1. Key-level policies (scope="key", scopeID=apiKeyID)
//     2. Project-level policies (scope="project", scopeID=projectID)
//     3. Org-level policies (scope="org", scopeID=orgID)
//     4. Platform-level policies (scope="platform", scopeID=0, enforced=false)
//
//   Within the same scope level:
//     - Higher Priority value takes precedence
//     - More specific pattern > wildcard pattern (specificity score)
//     - deny wins over allow at same priority + specificity
//
//   If no policy matches in any scope: default ALLOW (backward compatible).
func EvaluateCapabilityAccess(orgID, projectID, apiKeyID int, capabilityName string) (bool, string, error) {
	if !cacheInitialized.Load() {
		return false, "", fmt.Errorf("policy cache not initialized")
	}

	policyMu.RLock()
	defer policyMu.RUnlock()

	// Phase A: Enforced platform deny
	for _, p := range capabilityPolicies {
		if p.Status != "active" {
			continue
		}
		if p.Scope == "platform" && p.Enforced && p.Action == "deny" {
			if matchCapabilityPattern(p.CapabilityPattern, capabilityName) {
				return false, fmt.Sprintf("enforced platform deny matches: %s", p.CapabilityPattern), nil
			}
		}
	}

	// Phase B: Hierarchical Evaluation
	// We want to check key -> project -> org -> platform
	// At each level, collect matching policies
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
		var matches []model.BgCapabilityPolicy
		for _, p := range capabilityPolicies {
			if p.Status != "active" {
				continue
			}
			if p.Scope == scope.name && p.ScopeID == scope.id && !p.Enforced {
				if matchCapabilityPattern(p.CapabilityPattern, capabilityName) {
					matches = append(matches, p)
				}
			}
		}

		if len(matches) > 0 {
			// Find winning policy in this scope
			var winning *model.BgCapabilityPolicy
			var maxPriority int
			var maxSpecificity int

			for i := range matches {
				m := matches[i]
				mSpec := capabilitySpecificity(m.CapabilityPattern)
				
				if winning == nil {
					winning = &matches[i]
					maxPriority = m.Priority
					maxSpecificity = mSpec
					continue
				}

				// Priority
				if m.Priority > maxPriority {
					winning = &matches[i]
					maxPriority = m.Priority
					maxSpecificity = mSpec
					continue
				} else if m.Priority < maxPriority {
					continue
				}

				// Priority equal -> Specificity
				if mSpec > maxSpecificity {
					winning = &matches[i]
					maxSpecificity = mSpec
					continue
				} else if mSpec < maxSpecificity {
					continue
				}

				// Priority equal, Specificity equal -> Deny wins over allow
				if m.Action == "deny" && winning.Action == "allow" {
					winning = &matches[i]
				}
			}

			// Decide based on winning policy
			if winning.Action == "deny" {
				return false, fmt.Sprintf("%s deny matches: %s", winning.Scope, winning.CapabilityPattern), nil
			}
			return true, "", nil
		}
	}

	// If no policy matches in any scope: default ALLOW
	return true, "", nil
}

// RefreshPolicyCache does a full reload from DB.
// On failure: retains old cache + logs error.
func RefreshPolicyCache() error {
	capPolicies, err := model.GetActiveBgCapabilityPolicies()
	if err != nil {
		return fmt.Errorf("failed to load capability policies: %w", err)
	}
	rtPolicies, err := model.GetActiveBgRoutingPolicies()
	if err != nil {
		return fmt.Errorf("failed to load routing policies: %w", err)
	}

	// Note: Policies are NOT pre-sorted here.
	// The evaluation logic in EvaluateCapabilityAccess dynamically finds the
	// highest priority/specificity match during its hierarchical scan.
	// We accept this O(N) evaluation behavior because N is small.
	
	policyMu.Lock()
	capabilityPolicies = capPolicies
	routingPolicies = rtPolicies
	policyMu.Unlock()
	cacheInitialized.Store(true)
	return nil
}

// InvalidatePolicyCache is called after any CRUD operation — synchronous reload.
// Returns an error if the reload fails. Admin API should return cache_sync_failed.
func InvalidatePolicyCache() error {
	if err := RefreshPolicyCache(); err != nil {
		common.SysError("policy cache refresh failed: " + err.Error())
		return err
	}
	return nil
}

// StartPolicyCacheRefresher starts a background goroutine that periodically
// refreshes the policy cache as a fallback (covers distributed deployments).
func StartPolicyCacheRefresher(interval time.Duration) {
	// Synchronous initial load to ensure cache is hot before handling requests
	if err := RefreshPolicyCache(); err != nil {
		common.SysError("policy_cache: initial load failed: " + err.Error())
	}

	go func() {
		common.SysLog(fmt.Sprintf("policy_cache: refresher started (interval: %s)", interval))
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if err := RefreshPolicyCache(); err != nil {
				common.SysError("policy_cache: periodic refresh failed: " + err.Error())
			}
		}
	}()
}
