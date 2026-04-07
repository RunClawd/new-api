package basegate

import (
	"sync"
)

var (
	adapterRegistryMu sync.RWMutex
	adapterRegistry   = make(map[string]ProviderAdapter)
)

func RegisterAdapter(adapter ProviderAdapter) {
	adapterRegistryMu.Lock()
	defer adapterRegistryMu.Unlock()
	for _, b := range adapter.DescribeCapabilities() {
		adapterRegistry[b.CapabilityPattern] = adapter
	}
}

func LookupAdapter(modelName string) ProviderAdapter {
	adapterRegistryMu.RLock()
	defer adapterRegistryMu.RUnlock()
	return adapterRegistry[modelName]
}

func ListRegisteredCapabilities() []string {
	adapterRegistryMu.RLock()
	defer adapterRegistryMu.RUnlock()
	names := make([]string, 0, len(adapterRegistry))
	for name := range adapterRegistry {
		names = append(names, name)
	}
	return names
}

func RegisteredAdapterCount() int {
	adapterRegistryMu.RLock()
	defer adapterRegistryMu.RUnlock()
	return len(adapterRegistry)
}

func ClearRegistry() {
	adapterRegistryMu.Lock()
	defer adapterRegistryMu.Unlock()
	adapterRegistry = make(map[string]ProviderAdapter)
}
