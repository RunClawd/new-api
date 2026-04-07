package basegate

import (
	"sync"
)

var (
	adapterRegistryMu sync.RWMutex
	capabilityMap     = make(map[string][]ProviderAdapter)
	nameMap           = make(map[string]ProviderAdapter)
)

func RegisterAdapter(adapter ProviderAdapter) {
	adapterRegistryMu.Lock()
	defer adapterRegistryMu.Unlock()
	nameMap[adapter.Name()] = adapter
	for _, b := range adapter.DescribeCapabilities() {
		capabilityMap[b.CapabilityPattern] = append(capabilityMap[b.CapabilityPattern], adapter)
	}
}

func LookupAdapters(modelName string) []ProviderAdapter {
	adapterRegistryMu.RLock()
	defer adapterRegistryMu.RUnlock()
	return capabilityMap[modelName]
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
	return names
}

func RegisteredAdapterCount() int {
	adapterRegistryMu.RLock()
	defer adapterRegistryMu.RUnlock()
	return len(nameMap)
}

func ClearRegistry() {
	adapterRegistryMu.Lock()
	defer adapterRegistryMu.Unlock()
	capabilityMap = make(map[string][]ProviderAdapter)
	nameMap = make(map[string]ProviderAdapter)
}
