package gemini

import (
    "net/http"
    "time"
)

type Option func(*options)

type options struct {
    apiKey     string
    model      string
    baseURL    string
    httpClient *http.Client
    timeout    time.Duration
}

func defaultOptions() options {
    return options{
        baseURL: "https://generativelanguage.googleapis.com/v1beta",
        timeout: 60 * time.Second,
    }
}

func WithAPIKey(key string) Option {
    return func(o *options) { o.apiKey = key }
}

func WithModel(model string) Option {
    return func(o *options) { o.model = model }
}

func WithBaseURL(url string) Option {
    return func(o *options) { o.baseURL = url }
}

func WithHTTPClient(client *http.Client) Option {
    return func(o *options) { o.httpClient = client }
}

func WithTimeout(d time.Duration) Option {
    return func(o *options) { o.timeout = d }
}
