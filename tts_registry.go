package gai

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/shillcollin/gai/tts"
)

// TTSProviderConfig holds configuration for initializing a TTS provider.
type TTSProviderConfig struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	Custom     map[string]any
}

// TTSProviderFactory creates TTS provider instances from configuration.
type TTSProviderFactory interface {
	// New creates a new TTS provider instance with the given configuration.
	New(config TTSProviderConfig) (tts.Provider, error)

	// DefaultConfig returns default configuration, typically from environment variables.
	DefaultConfig() TTSProviderConfig
}

var (
	ttsRegistryMu sync.RWMutex
	ttsRegistry   = make(map[string]TTSProviderFactory)
)

// RegisterTTSProvider registers a TTS provider factory. This is typically called
// from a provider's init() function to enable self-registration on import.
//
// Example:
//
//	func init() {
//	    gai.RegisterTTSProvider("elevenlabs", &Factory{})
//	}
//
// Panics if a provider with the same name is already registered.
func RegisterTTSProvider(name string, factory TTSProviderFactory) {
	ttsRegistryMu.Lock()
	defer ttsRegistryMu.Unlock()

	if _, exists := ttsRegistry[name]; exists {
		panic(fmt.Sprintf("gai: TTS provider %q already registered", name))
	}
	ttsRegistry[name] = factory
}

// GetTTSProviderFactory returns the factory for a registered TTS provider.
func GetTTSProviderFactory(name string) (TTSProviderFactory, bool) {
	ttsRegistryMu.RLock()
	defer ttsRegistryMu.RUnlock()
	factory, ok := ttsRegistry[name]
	return factory, ok
}

// RegisteredTTSProviders returns the names of all registered TTS providers.
func RegisteredTTSProviders() []string {
	ttsRegistryMu.RLock()
	defer ttsRegistryMu.RUnlock()

	names := make([]string, 0, len(ttsRegistry))
	for name := range ttsRegistry {
		names = append(names, name)
	}
	return names
}

// IsTTSProviderRegistered checks if a TTS provider is registered.
func IsTTSProviderRegistered(name string) bool {
	ttsRegistryMu.RLock()
	defer ttsRegistryMu.RUnlock()
	_, ok := ttsRegistry[name]
	return ok
}

// clearTTSRegistry removes all registered TTS providers. For testing only.
func clearTTSRegistry() {
	ttsRegistryMu.Lock()
	defer ttsRegistryMu.Unlock()
	ttsRegistry = make(map[string]TTSProviderFactory)
}
