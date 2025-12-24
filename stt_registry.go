package gai

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/shillcollin/gai/stt"
)

// STTProviderConfig holds configuration for initializing an STT provider.
type STTProviderConfig struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	Custom     map[string]any
}

// STTProviderFactory creates STT provider instances from configuration.
type STTProviderFactory interface {
	// New creates a new STT provider instance with the given configuration.
	New(config STTProviderConfig) (stt.Provider, error)

	// DefaultConfig returns default configuration, typically from environment variables.
	DefaultConfig() STTProviderConfig
}

var (
	sttRegistryMu sync.RWMutex
	sttRegistry   = make(map[string]STTProviderFactory)
)

// RegisterSTTProvider registers an STT provider factory. This is typically called
// from a provider's init() function to enable self-registration on import.
//
// Example:
//
//	func init() {
//	    gai.RegisterSTTProvider("deepgram", &Factory{})
//	}
//
// Panics if a provider with the same name is already registered.
func RegisterSTTProvider(name string, factory STTProviderFactory) {
	sttRegistryMu.Lock()
	defer sttRegistryMu.Unlock()

	if _, exists := sttRegistry[name]; exists {
		panic(fmt.Sprintf("gai: STT provider %q already registered", name))
	}
	sttRegistry[name] = factory
}

// GetSTTProviderFactory returns the factory for a registered STT provider.
func GetSTTProviderFactory(name string) (STTProviderFactory, bool) {
	sttRegistryMu.RLock()
	defer sttRegistryMu.RUnlock()
	factory, ok := sttRegistry[name]
	return factory, ok
}

// RegisteredSTTProviders returns the names of all registered STT providers.
func RegisteredSTTProviders() []string {
	sttRegistryMu.RLock()
	defer sttRegistryMu.RUnlock()

	names := make([]string, 0, len(sttRegistry))
	for name := range sttRegistry {
		names = append(names, name)
	}
	return names
}

// IsSTTProviderRegistered checks if an STT provider is registered.
func IsSTTProviderRegistered(name string) bool {
	sttRegistryMu.RLock()
	defer sttRegistryMu.RUnlock()
	_, ok := sttRegistry[name]
	return ok
}

// clearSTTRegistry removes all registered STT providers. For testing only.
func clearSTTRegistry() {
	sttRegistryMu.Lock()
	defer sttRegistryMu.Unlock()
	sttRegistry = make(map[string]STTProviderFactory)
}
