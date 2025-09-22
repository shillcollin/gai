package obs

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	metricsOnce      sync.Once
	requestCounter   metric.Int64Counter
	latencyHistogram metric.Float64Histogram
	inputTokensHist  metric.Int64Histogram
	outputTokensHist metric.Int64Histogram
	totalTokensHist  metric.Int64Histogram

	bgOnce sync.Once
	bgCtx  context.Context
)

func installMetrics(m meter) {
	metricsOnce.Do(func() {
		if m == nil {
			return
		}
		requestCounter, _ = m.Int64Counter("gai.requests", metric.WithDescription("Total LLM requests"))
		latencyHistogram, _ = m.Float64Histogram("gai.request.latency_ms", metric.WithDescription("LLM latency (ms)"))
		inputTokensHist, _ = m.Int64Histogram("gai.tokens.input", metric.WithDescription("Input tokens"))
		outputTokensHist, _ = m.Int64Histogram("gai.tokens.output", metric.WithDescription("Output tokens"))
		totalTokensHist, _ = m.Int64Histogram("gai.tokens.total", metric.WithDescription("Total tokens"))
	})
}

type meter interface {
	Int64Counter(string, ...metric.Int64CounterOption) (metric.Int64Counter, error)
	Float64Histogram(string, ...metric.Float64HistogramOption) (metric.Float64Histogram, error)
	Int64Histogram(string, ...metric.Int64HistogramOption) (metric.Int64Histogram, error)
}

func recordRequest(attrs ...attribute.KeyValue) {
	if requestCounter != nil {
		if len(attrs) > 0 {
			requestCounter.Add(backgroundContext(), 1, metric.WithAttributes(attrs...))
		} else {
			requestCounter.Add(backgroundContext(), 1)
		}
	}
}

func recordLatency(ms float64, attrs ...attribute.KeyValue) {
	if latencyHistogram != nil {
		if len(attrs) > 0 {
			latencyHistogram.Record(backgroundContext(), ms, metric.WithAttributes(attrs...))
		} else {
			latencyHistogram.Record(backgroundContext(), ms)
		}
	}
}

func recordUsage(usage UsageTokens, attrs ...attribute.KeyValue) {
	ctx := backgroundContext()
	if inputTokensHist != nil {
		if len(attrs) > 0 {
			inputTokensHist.Record(ctx, int64(usage.InputTokens), metric.WithAttributes(attrs...))
		} else {
			inputTokensHist.Record(ctx, int64(usage.InputTokens))
		}
	}
	if outputTokensHist != nil {
		if len(attrs) > 0 {
			outputTokensHist.Record(ctx, int64(usage.OutputTokens), metric.WithAttributes(attrs...))
		} else {
			outputTokensHist.Record(ctx, int64(usage.OutputTokens))
		}
	}
	if totalTokensHist != nil {
		if len(attrs) > 0 {
			totalTokensHist.Record(ctx, int64(usage.TotalTokens), metric.WithAttributes(attrs...))
		} else {
			totalTokensHist.Record(ctx, int64(usage.TotalTokens))
		}
	}
}

func backgroundContext() context.Context {
	bgOnce.Do(func() {
		bgCtx = context.Background()
	})
	return bgCtx
}
