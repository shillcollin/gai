package anthropic

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
    headers    map[string]string
    timeout    time.Duration
}

func defaultOptions() options {
    return options{
        baseURL: "https://api.anthropic.com/v1",
        timeout: 60 * time.Second,
        headers: map[string]string{},
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

func WithHeader(key, value string) Option {
    return func(o *options) {
        if o.headers == nil {
            o.headers = map[string]string{}
        }
        o.headers[key] = value
    }
}

func WithTimeout(d time.Duration) Option {
    return func(o *options) { o.timeout = d }
}
