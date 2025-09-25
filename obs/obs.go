package obs

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	manager     *Manager
	managerOnce sync.Once
)

// Manager coordinates OTEL setup and downstream sinks.
type Manager struct {
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	tracer         trace.Tracer
	meter          metric.Meter

	sinks []Sink
}

// Sink consumes structured observability events (e.g. Braintrust, Arize).
type Sink interface {
	LogCompletion(context.Context, Completion) error
	Shutdown(context.Context) error
}

type noopSpanExporter struct{}

func (noopSpanExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error { return nil }

func (noopSpanExporter) Shutdown(context.Context) error { return nil }

// Init configures global tracing/metrics and configured sinks. Safe to call once.
func Init(ctx context.Context, opts Options) (func(context.Context) error, error) {
	var initErr error
	managerOnce.Do(func() {
		if opts.ServiceName == "" {
			opts.ServiceName = "gai"
		}
		if opts.SampleRatio <= 0 || opts.SampleRatio > 1 {
			opts.SampleRatio = 1
		}

		res, err := buildResource(opts)
		if err != nil {
			initErr = err
			return
		}

		tracerProvider, err := buildTracerProvider(ctx, opts, res)
		if err != nil {
			initErr = err
			return
		}

		var meterProvider *sdkmetric.MeterProvider
		if !opts.DisableMetrics {
			meterProvider = sdkmetric.NewMeterProvider(sdkmetric.WithResource(res))
			otel.SetMeterProvider(meterProvider)
		}

		tracer := tracerProvider.Tracer("github.com/shillcollin/gai/obs")
		var meter metric.Meter
		if meterProvider != nil {
			meter = meterProvider.Meter("github.com/shillcollin/gai/obs")
		} else {
			meter = otel.Meter("github.com/shillcollin/gai/obs")
		}

		sinks, err := buildSinks(ctx, opts)
		if err != nil {
			_ = tracerProvider.Shutdown(ctx)
			if meterProvider != nil {
				_ = meterProvider.Shutdown(ctx)
			}
			initErr = err
			return
		}

		manager = &Manager{
			tracerProvider: tracerProvider,
			meterProvider:  meterProvider,
			tracer:         tracer,
			meter:          meter,
			sinks:          sinks,
		}

		otel.SetTracerProvider(tracerProvider)
		installMetrics(meter)
	})

	if initErr != nil {
		return nil, initErr
	}
	if manager == nil {
		return nil, errors.New("observability already initialized")
	}

	shutdown := func(ctx context.Context) error {
		var firstErr error
		for _, sink := range manager.sinks {
			if err := sink.Shutdown(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if manager.meterProvider != nil {
			if err := manager.meterProvider.Shutdown(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if err := manager.tracerProvider.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
		return firstErr
	}

	return shutdown, nil
}

func buildResource(opts Options) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(opts.ServiceName),
	}
	if opts.Environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironment(opts.Environment))
	}
	if opts.Version != "" {
		attrs = append(attrs, semconv.ServiceVersion(opts.Version))
	}
	return resource.Merge(resource.Default(), resource.NewSchemaless(attrs...))
}

func buildTracerProvider(ctx context.Context, opts Options, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	var spanExporter sdktrace.SpanExporter
	var err error
	switch opts.Exporter {
	case ExporterStdout:
		spanExporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
	case ExporterNone:
		spanExporter = noopSpanExporter{}
	default:
		spanExporter, err = newOTLPExporter(ctx, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("build exporter: %w", err)
	}

	sampler := sdktrace.TraceIDRatioBased(opts.SampleRatio)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sampler),
		sdktrace.WithBatcher(spanExporter),
		sdktrace.WithResource(res),
	)
	return tp, nil
}

func buildSinks(ctx context.Context, opts Options) ([]Sink, error) {
	sinks := []Sink{}
	if opts.Braintrust.Enabled {
		sink, err := newBraintrustSink(ctx, opts.Braintrust)
		if err != nil {
			return nil, err
		}
		sinks = append(sinks, sink)
	}
	if opts.Arize.Enabled {
		sink := newArizeSinkStub()
		sinks = append(sinks, sink)
	}
	return sinks, nil
}

// Tracer exposes the configured tracer.
func Tracer() trace.Tracer {
	if manager == nil {
		return otel.Tracer("github.com/shillcollin/gai/obs")
	}
	return manager.tracer
}

// Meter exposes the configured meter for custom instrumentation.
func Meter() metric.Meter {
	if manager == nil {
		return otel.Meter("github.com/shillcollin/gai/obs")
	}
	return manager.meter
}

// RecordRequest records request-level metrics.
func RecordRequest(attrs ...attribute.KeyValue) {
	if manager == nil {
		return
	}
	recordRequest(attrs...)
}

// RecordUsage emits token/cost metrics.
func RecordUsage(usageTokens UsageTokens, attrs ...attribute.KeyValue) {
	if manager == nil {
		return
	}
	recordUsage(usageTokens, attrs...)
}

// LogCompletion fan-outs completion metadata to configured sinks.
func LogCompletion(ctx context.Context, completion Completion) {
	if manager == nil {
		return
	}
	for _, sink := range manager.sinks {
		if err := sink.LogCompletion(ctx, completion); err != nil {
			// We deliberately swallow sink errors to avoid impacting hot paths.
		}
	}
}

// Completion captures the result of a model invocation for downstream logging.
type Completion struct {
	Provider     string
	Model        string
	RequestID    string
	Input        []Message
	Output       Message
	Usage        UsageTokens
	LatencyMS    int64
	Metadata     map[string]any
	Error        string
	CreatedAtUTC int64
	ToolCalls    []ToolCallRecord
}

// ToolCallRecord captures a summarized tool invocation for observability sinks.
type ToolCallRecord struct {
	Step       int            `json:"step"`
	ID         string         `json:"id,omitempty"`
	Name       string         `json:"name,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
	Result     any            `json:"result,omitempty"`
	Error      string         `json:"error,omitempty"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Retries    int            `json:"retries,omitempty"`
}

// Message keeps observability copies of chat messages without leaking internal structs.
type Message struct {
	Role string         `json:"role"`
	Text string         `json:"text,omitempty"`
	Data map[string]any `json:"data,omitempty"`
}

// UsageTokens standardizes token/cost metrics across sinks.
type UsageTokens struct {
	InputTokens     int
	OutputTokens    int
	TotalTokens     int
	ReasoningTokens int
	CachedTokens    int
	AudioTokens     int
	CostUSD         float64
}

// ConvertMessages builds lightweight message representations from core messages.
func ConvertMessages(messages []MessageConvertible) []Message {
	out := make([]Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		out = append(out, msg.ToObsMessage())
	}
	return out
}

// MessageConvertible allows types to provide observability-safe projections.
type MessageConvertible interface {
	ToObsMessage() Message
}
