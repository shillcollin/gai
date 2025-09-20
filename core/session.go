package core

import "time"

// Session references cached conversation state maintained by providers.
type Session struct {
	Provider  string         `json:"provider"`
	ID        string         `json:"id"`
	CreatedAt time.Time      `json:"created_at,omitempty"`
	ExpiresAt time.Time      `json:"expires_at,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}
