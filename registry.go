package gai

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/shillcollin/gai/core"
)

// ProviderConfig holds configuration for initializing a provider.
type ProviderConfig struct {
	APIKey       string
	BaseURL      string
	DefaultModel string
	Headers      map[string]string
	HTTPClient   *http.Client
}

// ProviderFactory creates provider instances from configuration.
type ProviderFactory interface {
	// New creates a new provider instance with the given configuration.
	New(config ProviderConfig) (core.Provider, error)

	// DefaultConfig returns default configuration, typically from environment variables.
	DefaultConfig() ProviderConfig
}

var (
	registryMu sync.RWMutex
	registry   = make(map[string]ProviderFactory)
)

// RegisterProvider registers a provider factory. This is typically called from
// a provider's init() function to enable self-registration on import.
//
// Example:
//
//	func init() {
//	    gai.RegisterProvider("openai", &Factory{})
//	}
//
// Panics if a provider with the same name is already registered.
func RegisterProvider(name string, factory ProviderFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("gai: provider %q already registered", name))
	}
	registry[name] = factory
}

// GetProviderFactory returns the factory for a registered provider.
func GetProviderFactory(name string) (ProviderFactory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	factory, ok := registry[name]
	return factory, ok
}

// RegisteredProviders returns the names of all registered providers.
func RegisteredProviders() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}

// IsProviderRegistered checks if a provider is registered.
func IsProviderRegistered(name string) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	_, ok := registry[name]
	return ok
}

// clearRegistry removes all registered providers. For testing only.
func clearRegistry() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = make(map[string]ProviderFactory)
}
