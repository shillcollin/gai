package openai

import (
	"net/http"
	"time"
)

type Option func(*options)

type options struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
	headers    map[string]string
	timeout    time.Duration
}

func defaultOptions() options {
	return options{
		baseURL: "https://api.openai.com/v1",
		timeout: 60 * time.Second,
		headers: map[string]string{},
	}
}

// WithAPIKey configures the API key.
func WithAPIKey(key string) Option {
	return func(o *options) { o.apiKey = key }
}

// WithModel sets the default model.
func WithModel(model string) Option {
	return func(o *options) { o.model = model }
}

// WithBaseURL overrides the API base URL.
func WithBaseURL(url string) Option {
	return func(o *options) { o.baseURL = url }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(o *options) { o.httpClient = client }
}

// WithHeader adds a static request header.
func WithHeader(key, value string) Option {
	return func(o *options) {
		if o.headers == nil {
			o.headers = map[string]string{}
		}
		o.headers[key] = value
	}
}

// WithTimeout customizes the client timeout.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}
