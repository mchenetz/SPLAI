package observability

import (
	"context"
	"crypto/tls"
	"os"
	"strconv"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/credentials"
)

var (
	tracerOnce sync.Once
	shutdownFn func(context.Context) error
)

func InitTracingFromEnv(service string) (func(context.Context) error, error) {
	var initErr error
	tracerOnce.Do(func() {
		exporterName := strings.ToLower(strings.TrimSpace(os.Getenv("SPLAI_OTEL_EXPORTER")))
		if exporterName == "" || exporterName == "none" {
			otel.SetTracerProvider(trace.NewNoopTracerProvider())
			shutdownFn = func(context.Context) error { return nil }
			return
		}

		exp, err := buildExporter(context.Background(), exporterName)
		if err != nil {
			initErr = err
			return
		}
		res, err := resource.New(context.Background(),
			resource.WithAttributes(
				semconv.ServiceNameKey.String(service),
				attribute.String("splai.environment", strings.TrimSpace(os.Getenv("SPLAI_ENVIRONMENT"))),
			),
		)
		if err != nil {
			initErr = err
			return
		}

		tp := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exp),
			sdktrace.WithSampler(buildSamplerFromEnv()),
			sdktrace.WithResource(res),
		)
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
		shutdownFn = tp.Shutdown
	})
	if shutdownFn == nil {
		shutdownFn = func(context.Context) error { return nil }
	}
	return shutdownFn, initErr
}

func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	t := otel.Tracer("splai")
	return t.Start(ctx, name, trace.WithAttributes(attrs...))
}

func buildExporter(ctx context.Context, exporterName string) (sdktrace.SpanExporter, error) {
	headers := parseHeaders(strings.TrimSpace(os.Getenv("SPLAI_OTEL_HEADERS")))
	insecure := getenvBool("SPLAI_OTEL_INSECURE", true)
	switch exporterName {
	case "stdout":
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	case "otlp", "otlpgrpc", "grpc":
		endpoint := strings.TrimSpace(os.Getenv("SPLAI_OTEL_ENDPOINT"))
		if endpoint == "" {
			endpoint = "localhost:4317"
		}
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(endpoint),
		}
		if len(headers) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(headers))
		}
		if insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		} else {
			opts = append(opts, otlptracegrpc.WithTLSCredentials(credentials.NewTLS(&tls.Config{})))
		}
		return otlptracegrpc.New(ctx, opts...)
	case "otlphttp", "http":
		endpoint := strings.TrimSpace(os.Getenv("SPLAI_OTEL_ENDPOINT"))
		if endpoint == "" {
			endpoint = "http://localhost:4318"
		}
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpointURL(endpoint),
		}
		if len(headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(headers))
		}
		if insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		return otlptracehttp.New(ctx, opts...)
	default:
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	}
}

func parseHeaders(raw string) map[string]string {
	out := map[string]string{}
	if raw == "" {
		return out
	}
	pairs := strings.Split(raw, ",")
	for _, p := range pairs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		if k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}

func buildSamplerFromEnv() sdktrace.Sampler {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("SPLAI_OTEL_SAMPLER")))
	switch mode {
	case "always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample())
	case "traceidratio", "ratio":
		ratio := getenvFloat("SPLAI_OTEL_SAMPLER_RATIO", 1.0)
		if ratio < 0 {
			ratio = 0
		}
		if ratio > 1 {
			ratio = 1
		}
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))
	default:
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	}
}

func getenvBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes":
		return true
	case "0", "false", "no":
		return false
	default:
		return fallback
	}
}

func getenvFloat(key string, fallback float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}
