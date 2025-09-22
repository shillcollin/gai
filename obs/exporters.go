package obs

import (
	"context"
	"crypto/tls"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func newOTLPExporter(ctx context.Context, opts Options) (sdktrace.SpanExporter, error) {
	if opts.Endpoint == "" {
		opts.Endpoint = "localhost:4317"
	}

	dialOpts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(opts.Endpoint)}
	if opts.Insecure {
		dialOpts = append(dialOpts, otlptracegrpc.WithInsecure())
	} else {
		dialOpts = append(dialOpts, otlptracegrpc.WithTLSCredentials(credentials.NewTLS(&tls.Config{})))
	}
	if len(opts.Headers) > 0 {
		dialOpts = append(dialOpts, otlptracegrpc.WithHeaders(opts.Headers))
	}
	dialOpts = append(dialOpts, otlptracegrpc.WithDialOption(grpc.WithBlock()))

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	exporter, err := otlptracegrpc.New(ctx, dialOpts...)
	if err == nil {
		return exporter, nil
	}

	httpOpts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(opts.Endpoint)}
	if opts.Insecure {
		httpOpts = append(httpOpts, otlptracehttp.WithInsecure())
	}
	if len(opts.Headers) > 0 {
		httpOpts = append(httpOpts, otlptracehttp.WithHeaders(opts.Headers))
	}

	httpExporter, httpErr := otlptracehttp.New(ctx, httpOpts...)
	if httpErr != nil {
		return nil, err
	}
	return httpExporter, nil
}
