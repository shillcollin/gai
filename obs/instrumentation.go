package obs

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// RequestRecorder encapsulates per-request tracing/metrics bookkeeping.
type RequestRecorder struct {
	start time.Time
	span  trace.Span
	attrs []attribute.KeyValue
}

// StartRequest starts a span and prepares metric aggregation.
func StartRequest(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, *RequestRecorder) {
	tracer := Tracer()
	ctx, span := tracer.Start(ctx, name, trace.WithAttributes(attrs...))
	recordRequest(attrs...)
	return ctx, &RequestRecorder{start: time.Now(), span: span, attrs: attrs}
}

// End finalizes span/metrics for the request.
func (r *RequestRecorder) End(err error, usage UsageTokens) {
	if r == nil {
		return
	}
	if err != nil {
		r.span.RecordError(err)
		r.span.SetStatus(codes.Error, err.Error())
	}
	latency := time.Since(r.start).Seconds() * 1000
	recordLatency(latency, r.attrs...)
	if usage.TotalTokens > 0 || usage.InputTokens > 0 || usage.OutputTokens > 0 {
		RecordUsage(usage, r.attrs...)
	}
	r.span.End()
}

// AddAttributes appends attributes to both span and subsequent metrics.
func (r *RequestRecorder) AddAttributes(attrs ...attribute.KeyValue) {
	if r == nil {
		return
	}
	r.attrs = append(r.attrs, attrs...)
	r.span.SetAttributes(attrs...)
}
