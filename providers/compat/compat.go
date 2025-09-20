package compat

import (
	"context"
	"net/http"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/providers/openai"
)

// CompatOpts configures the OpenAI-compatible client.
type CompatOpts struct {
	BaseURL    string
	APIKey     string
	Model      string
	Headers    map[string]string
	HTTPClient *http.Client
}

type Client struct {
	inner core.Provider
}

// New constructs a provider targeting an OpenAI-compatible API surface.
func New(opts CompatOpts) *Client {
	options := []openai.Option{
		openai.WithBaseURL(opts.BaseURL),
		openai.WithAPIKey(opts.APIKey),
		openai.WithModel(opts.Model),
	}
	if opts.HTTPClient != nil {
		options = append(options, openai.WithHTTPClient(opts.HTTPClient))
	}
	for k, v := range opts.Headers {
		options = append(options, openai.WithHeader(k, v))
	}
	return &Client{inner: openai.New(options...)}
}

func (c *Client) GenerateText(ctx context.Context, req core.Request) (*core.TextResult, error) {
	return c.inner.GenerateText(ctx, req)
}

func (c *Client) StreamText(ctx context.Context, req core.Request) (*core.Stream, error) {
	return c.inner.StreamText(ctx, req)
}

func (c *Client) GenerateObject(ctx context.Context, req core.Request) (*core.ObjectResultRaw, error) {
	return c.inner.GenerateObject(ctx, req)
}

func (c *Client) StreamObject(ctx context.Context, req core.Request) (*core.ObjectStreamRaw, error) {
	return c.inner.StreamObject(ctx, req)
}

func (c *Client) Capabilities() core.Capabilities {
	return c.inner.Capabilities()
}
