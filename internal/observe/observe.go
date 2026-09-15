package observe

import (
	"context"
	"net/http"
	"os"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("settleagent/gateway")

// Init configures the global tracer provider. Trace and span IDs are always
// generated (local SDK); spans export OTLP/HTTP only when
// OTEL_EXPORTER_OTLP_ENDPOINT is set.
func Init(ctx context.Context) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName("settleagent-api")),
	)
	if err != nil {
		return nil, err
	}
	var opts []trace.TracerProviderOption
	opts = append(opts, trace.WithResource(res))
	if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); endpoint != "" {
		exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
		if err != nil {
			return nil, err
		}
		opts = append(opts, trace.WithBatcher(exp))
	}
	tp := trace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

type ctxKey struct{}

func TraceID(ctx context.Context) string {
	if sc := oteltrace.SpanContextFromContext(ctx); sc.IsValid() {
		return sc.TraceID().String()
	}
	return ""
}

func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(ctxKey{}).(string); ok {
		return id
	}
	return ""
}

// Middleware assigns a request ID, opens a server span, and propagates both.
// It wraps outside auth so rejected attempts are traced too.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = uuid.NewString()
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, reqID)
		ctx, span := tracer.Start(ctx, r.Method+" "+r.URL.Path,
			oteltrace.WithAttributes(
				semconv.HTTPRequestMethodKey.String(r.Method),
				semconv.URLPathKey.String(r.URL.Path),
			),
		)
		defer span.End()
		w.Header().Set("X-Request-ID", reqID)
		if tid := TraceID(ctx); tid != "" {
			w.Header().Set("X-Trace-ID", tid)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Start opens a child span; the gateway service uses it around
// policy evaluation, approval waits, tool execution, and receipts.
func Start(ctx context.Context, name string, attrs map[string]string) (context.Context, oteltrace.Span) {
	var kv []attribute.KeyValue
	for k, v := range attrs {
		kv = append(kv, attribute.String(k, v))
	}
	return tracer.Start(ctx, name, oteltrace.WithAttributes(kv...))
}
