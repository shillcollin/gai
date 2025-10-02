package xai

import (
	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/internal/httpclient"
	"github.com/shillcollin/gai/providers/compat"
)

// Client for the XAI API.
type Client struct {
	core.Provider
}

// New constructs a new XAI client.
func New(opts ...Option) *Client {
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}

	if o.httpClient == nil {
		o.httpClient = httpclient.New(httpclient.WithTimeout(o.timeout))
	}

	compatOpts := compat.CompatOpts{
		BaseURL:    o.baseURL,
		APIKey:     o.apiKey,
		Model:      o.model,
		HTTPClient: o.httpClient,
		Headers:    o.headers,
	}

	return &Client{
		Provider: compat.New(compatOpts),
	}
}