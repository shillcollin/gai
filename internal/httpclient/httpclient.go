package httpclient

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// Options controls the HTTP client construction used by providers.
type Options struct {
	Timeout               time.Duration
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	MaxConnsPerHost       int
	IdleConnTimeout       time.Duration
	TLSHandshakeTimeout   time.Duration
	ExpectContinueTimeout time.Duration
	DisableCompression    bool
	Transport             http.RoundTripper
}

// Option mutates Options.
type Option func(*Options)

// WithTimeout sets the request timeout.
func WithTimeout(d time.Duration) Option {
	return func(o *Options) { o.Timeout = d }
}

// WithTransport provides a custom transport overriding defaults.
func WithTransport(rt http.RoundTripper) Option {
	return func(o *Options) { o.Transport = rt }
}

// DefaultOptions returns sensible defaults for provider usage.
func DefaultOptions() Options {
	return Options{
		Timeout:               60 * time.Second,
		MaxIdleConns:          128,
		MaxIdleConnsPerHost:   32,
		MaxConnsPerHost:       64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// New constructs an *http.Client configured for high throughput API calls.
func New(opts ...Option) *http.Client {
	options := DefaultOptions()
	for _, opt := range opts {
		opt(&options)
	}

	transport := options.Transport
	if transport == nil {
		transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   15 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          options.MaxIdleConns,
			MaxIdleConnsPerHost:   options.MaxIdleConnsPerHost,
			MaxConnsPerHost:       options.MaxConnsPerHost,
			IdleConnTimeout:       options.IdleConnTimeout,
			TLSHandshakeTimeout:   options.TLSHandshakeTimeout,
			ExpectContinueTimeout: options.ExpectContinueTimeout,
			DisableCompression:    options.DisableCompression,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		}
	}

	return &http.Client{
		Timeout:   options.Timeout,
		Transport: transport,
	}
}
