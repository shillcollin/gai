package gai

import (
	"errors"
	"fmt"
)

// Sentinel errors for common failure cases.
var (
	// ErrNoProvider is returned when a provider is not registered.
	ErrNoProvider = errors.New("provider not registered")

	// ErrInvalidModel is returned for malformed model strings.
	ErrInvalidModel = errors.New("invalid model format (expected provider/model)")

	// ErrNoModel is returned when no model is specified and no default is set.
	ErrNoModel = errors.New("no model specified")

	// ErrNoAPIKey is returned when a required API key is not configured.
	ErrNoAPIKey = errors.New("API key not configured")

	// ErrNoText is returned when a text response was expected but not present.
	ErrNoText = errors.New("no text in response")
)

// ModelError provides detailed information about model resolution failures.
type ModelError struct {
	Model     string   // The model string that failed to resolve
	Err       error    // The underlying error
	Available []string // Available providers (if applicable)
}

func (e *ModelError) Error() string {
	if len(e.Available) > 0 {
		return fmt.Sprintf("model %q: %v (available providers: %v)", e.Model, e.Err, e.Available)
	}
	return fmt.Sprintf("model %q: %v", e.Model, e.Err)
}

func (e *ModelError) Unwrap() error {
	return e.Err
}

// ProviderError wraps errors from provider operations.
type ProviderError struct {
	Provider string
	Op       string
	Err      error
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("%s: %s: %v", e.Provider, e.Op, e.Err)
}

func (e *ProviderError) Unwrap() error {
	return e.Err
}
