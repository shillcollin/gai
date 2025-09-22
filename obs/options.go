package obs

import "time"

// ExporterType enumerates supported tracing exporter backends.
type ExporterType string

const (
	ExporterOTLP   ExporterType = "otlp"
	ExporterStdout ExporterType = "stdout"
	ExporterNone   ExporterType = "none"
)

// Options control observability initialization.
type Options struct {
	ServiceName string
	Environment string
	Version     string

	Exporter    ExporterType
	Endpoint    string
	Insecure    bool
	Headers     map[string]string
	SampleRatio float64

	Braintrust BraintrustOptions
	Arize      ArizeOptions

	DisableMetrics bool
}

// BraintrustOptions configure Braintrust logging.
type BraintrustOptions struct {
	Enabled   bool
	APIKey    string
	Project   string
	ProjectID string
	Dataset   string
	BaseURL   string

	BatchSize     int
	FlushInterval time.Duration
	HTTPTimeout   time.Duration
}

// ArizeOptions reserved for future support. Included to signal roadmap priorities.
type ArizeOptions struct {
	Enabled bool
}

// DefaultOptions returns sane defaults when env configuration is partial.
func DefaultOptions() Options {
	return Options{
		Exporter:    ExporterOTLP,
		SampleRatio: 1.0,
		Braintrust: BraintrustOptions{
			BatchSize:     32,
			FlushInterval: 3 * time.Second,
			HTTPTimeout:   10 * time.Second,
		},
	}
}
