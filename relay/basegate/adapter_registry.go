package basegate

import (
	"math/rand"
	"sort"
	"sync"
)

// weightedAdapter pairs a ProviderAdapter with its routing weight for a specific capability.
// Weight comes from CapabilityBinding.Weight at registration time, NOT from the adapter itself.
// This preserves the invariant that the same adapter can have different weights for different
// capabilities (e.g. openai-llm is preferred for chat.standard but less preferred for reasoning.pro).
type weightedAdapter struct {
	Adapter ProviderAdapter
	Weight  int
}

var (
	adapterRegistryMu sync.RWMutex
	// capabilityMap stores weighted adapters per capability name.
	// The weight is taken from CapabilityBinding.Weight at registration time.
	capabilityMap = make(map[string][]weightedAdapter)
	nameMap       = make(map[string]ProviderAdapter)
)

// RegisterAdapter registers an adapter and all its capability bindings.
// Weights are read from each CapabilityBinding — the same adapter can have different
// weights for different capabilities.
func RegisterAdapter(adapter ProviderAdapter) {
	adapterRegistryMu.Lock()
	defer adapterRegistryMu.Unlock()
	nameMap[adapter.Name()] = adapter
	for _, b := range adapter.DescribeCapabilities() {
		capabilityMap[b.CapabilityPattern] = append(capabilityMap[b.CapabilityPattern], weightedAdapter{
			Adapter: adapter,
			Weight:  b.Weight, // from binding, not from adapter level
		})
	}
}

// LookupAdapters returns adapters for a capability name, sorted by weighted random selection.
// The "preferred" adapter is first; remaining adapters serve as fallback in the orchestrator loop.
//
// Selection algorithm:
//   - If all weights are 0 or equal: random shuffle (uniform distribution).
//   - Otherwise: weighted random permutation — each position drawn proportional to remaining weight.
//
// Return type is []ProviderAdapter (callers do not need to know about weights).
func LookupAdapters(modelName string) []ProviderAdapter {
	adapterRegistryMu.RLock()
	weighted := capabilityMap[modelName]
	adapterRegistryMu.RUnlock()

	if len(weighted) == 0 {
		return nil
	}
	if len(weighted) == 1 {
		return []ProviderAdapter{weighted[0].Adapter}
	}

	return weightedRandomSort(weighted)
}

// weightedRandomSort performs a weighted random permutation of the adapters.
// Adapters with higher weight are more likely to appear earlier (preferred).
func weightedRandomSort(entries []weightedAdapter) []ProviderAdapter {
	// Check if all weights are zero or identical (degenerate case → random shuffle)
	totalWeight := 0
	for _, e := range entries {
		totalWeight += e.Weight
	}

	result := make([]ProviderAdapter, 0, len(entries))

	if totalWeight == 0 {
		// Uniform shuffle
		perm := rand.Perm(len(entries))
		for _, i := range perm {
			result = append(result, entries[i].Adapter)
		}
		return result
	}

	// Weighted random permutation: pick each position by weight, remove, repeat
	remaining := make([]weightedAdapter, len(entries))
	copy(remaining, entries)

	for len(remaining) > 0 {
		sum := 0
		for _, e := range remaining {
			sum += e.Weight
		}
		if sum == 0 {
			// Remaining all have weight 0 — shuffle them randomly
			rand.Shuffle(len(remaining), func(i, j int) { remaining[i], remaining[j] = remaining[j], remaining[i] })
			for _, e := range remaining {
				result = append(result, e.Adapter)
			}
			break
		}

		pick := rand.Intn(sum)
		cumulative := 0
		chosen := 0
		for i, e := range remaining {
			cumulative += e.Weight
			if pick < cumulative {
				chosen = i
				break
			}
		}

		result = append(result, remaining[chosen].Adapter)
		// Remove chosen from remaining (order-preserving)
		remaining = append(remaining[:chosen], remaining[chosen+1:]...)
	}

	return result
}

// ListAdaptersUnordered returns all registered adapters for a capability without any sorting.
// Used by the routing policy engine (bg_router.go) which applies its own ordering strategy.
// For the original weighted-random behavior, use LookupAdapters().
//
// Phase 13: consider returning weight/provider metadata if BYO fallback needs original weights.
func ListAdaptersUnordered(capabilityName string) []ProviderAdapter {
	adapterRegistryMu.RLock()
	weighted := capabilityMap[capabilityName]
	// Copy under lock — RegisterAdapter may replace the slice via append
	result := make([]ProviderAdapter, len(weighted))
	for i, wa := range weighted {
		result[i] = wa.Adapter
	}
	adapterRegistryMu.RUnlock()
	return result
}

func LookupAdapterByName(adapterName string) ProviderAdapter {
	adapterRegistryMu.RLock()
	defer adapterRegistryMu.RUnlock()
	return nameMap[adapterName]
}

func ListRegisteredCapabilities() []string {
	adapterRegistryMu.RLock()
	defer adapterRegistryMu.RUnlock()
	names := make([]string, 0, len(capabilityMap))
	for name := range capabilityMap {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func RegisteredAdapterCount() int {
	adapterRegistryMu.RLock()
	defer adapterRegistryMu.RUnlock()
	return len(nameMap)
}

// AdapterInfo is a JSON-serialisable summary of a registered adapter for admin display.
type AdapterInfo struct {
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	Provider     string   `json:"provider,omitempty"`
}

// ListRegisteredAdapters returns a summary of all registered adapters with their capabilities.
func ListRegisteredAdapters() []AdapterInfo {
	adapterRegistryMu.RLock()
	defer adapterRegistryMu.RUnlock()

	// Build reverse map: adapter name → capabilities
	adapterCaps := make(map[string][]string)
	adapterProvider := make(map[string]string)
	for capName, was := range capabilityMap {
		for _, wa := range was {
			name := wa.Adapter.Name()
			adapterCaps[name] = append(adapterCaps[name], capName)
		}
	}
	// Get provider from first capability binding
	for _, adapter := range nameMap {
		caps := adapter.DescribeCapabilities()
		if len(caps) > 0 {
			adapterProvider[adapter.Name()] = caps[0].Provider
		}
	}

	result := make([]AdapterInfo, 0, len(nameMap))
	for name := range nameMap {
		caps := adapterCaps[name]
		sort.Strings(caps)
		result = append(result, AdapterInfo{
			Name:         name,
			Capabilities: caps,
			Provider:     adapterProvider[name],
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func ClearRegistry() {
	adapterRegistryMu.Lock()
	defer adapterRegistryMu.Unlock()
	capabilityMap = make(map[string][]weightedAdapter)
	nameMap = make(map[string]ProviderAdapter)
}
