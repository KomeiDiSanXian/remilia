package telemetry

import (
	"errors"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	testutil "github.com/KomeiDiSanXian/remilia/middleware/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultTracingConfig(t *testing.T) {
	cfg := DefaultTracingConfig()

	assert.Equal(t, "remilia", cfg.TracerName)
	assert.False(t, cfg.IncludeEventDetail)
	assert.Equal(t, 200, cfg.MaxContentLength)
	assert.NotNil(t, cfg.SpanNameFunc)

	// SpanNameFunc returns event type from the context
	ctx := testutil.CreateTestContext()
	name := cfg.SpanNameFunc(ctx)
	assert.Equal(t, "event.PRIVATE_MESSAGE", name)

	// SpanNameFunc falls back to "event.process" when no event
	emptyCtx := eventctx.NewContextFromEvent(nil, nil)
	name = cfg.SpanNameFunc(emptyCtx)
	assert.Equal(t, "event.process", name)
}

func TestTracing(t *testing.T) {
	t.Run("nil config does not panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			mw := Tracing(TracingConfig{})
			handler := mw(testutil.MockHandler(nil, 0))
			ctx := testutil.CreateTestContext()
			_ = handler(ctx)
		})
	})

	t.Run("passes through on success", func(t *testing.T) {
		cfg := DefaultTracingConfig()
		mw := Tracing(cfg)
		handler := mw(testutil.MockHandler(nil, 0))

		ctx := testutil.CreateTestContext()
		err := handler(ctx)

		assert.NoError(t, err)
	})

	t.Run("passes through on error", func(t *testing.T) {
		expectedErr := errors.New("handler error")
		cfg := DefaultTracingConfig()
		mw := Tracing(cfg)
		handler := mw(testutil.MockHandler(expectedErr, 0))

		ctx := testutil.CreateTestContext()
		err := handler(ctx)

		assert.ErrorIs(t, err, expectedErr)
	})

	t.Run("sets trace and span IDs on context", func(t *testing.T) {
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
		prev := otel.GetTracerProvider()
		otel.SetTracerProvider(tp)
		t.Cleanup(func() { otel.SetTracerProvider(prev) })

		cfg := DefaultTracingConfig()
		mw := Tracing(cfg)
		handler := mw(testutil.MockHandler(nil, 0))

		ctx := testutil.CreateTestContext()
		err := handler(ctx)
		require.NoError(t, err)

		// Verify spans were created
		spans := exporter.GetSpans()
		require.Len(t, spans, 1)
		assert.Equal(t, "event.PRIVATE_MESSAGE", spans[0].Name)

		// Verify context has trace/span IDs
		traceID := GetTraceID(ctx)
		spanID := GetSpanID(ctx)
		assert.NotEmpty(t, traceID)
		assert.NotEmpty(t, spanID)
		assert.Equal(t, spans[0].SpanContext.TraceID().String(), traceID)
		assert.Equal(t, spans[0].SpanContext.SpanID().String(), spanID)
	})
}

func TestTracingNamed(t *testing.T) {
	t.Run("wraps middleware with named span", func(t *testing.T) {
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
		prev := otel.GetTracerProvider()
		otel.SetTracerProvider(tp)
		t.Cleanup(func() { otel.SetTracerProvider(prev) })

		mw := TracingNamed("my-middleware", mockMiddleware)
		handler := mw(testutil.MockHandler(nil, 0))

		ctx := testutil.CreateTestContext()
		err := handler(ctx)
		require.NoError(t, err)

		spans := exporter.GetSpans()
		require.Len(t, spans, 1)
		assert.Equal(t, "middleware.my-middleware", spans[0].Name)

		// Verify middleware name attribute
		found := false
		for _, attr := range spans[0].Attributes {
			if string(attr.Key) == "remilia.middleware.name" {
				assert.Equal(t, "my-middleware", attr.Value.AsString())
				found = true
				break
			}
		}
		assert.True(t, found, "middleware name attribute not found")
	})

	t.Run("passes through handler error", func(t *testing.T) {
		expectedErr := errors.New("handler error")
		mw := TracingNamed("my-middleware", mockMiddleware)
		handler := mw(testutil.MockHandler(expectedErr, 0))

		ctx := testutil.CreateTestContext()
		err := handler(ctx)
		assert.ErrorIs(t, err, expectedErr)
	})
}

func TestTracingHandler(t *testing.T) {
	t.Run("wraps handler with tracing span", func(t *testing.T) {
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
		prev := otel.GetTracerProvider()
		otel.SetTracerProvider(tp)
		t.Cleanup(func() { otel.SetTracerProvider(prev) })

		var called bool
		handler := TracingHandler("ping", func(ctx *eventctx.Context) error {
			called = true
			return nil
		})

		ctx := testutil.CreateTestContext()
		err := handler(ctx)
		require.NoError(t, err)
		assert.True(t, called)

		spans := exporter.GetSpans()
		require.Len(t, spans, 1)
		assert.Equal(t, "handler.ping", spans[0].Name)

		// Verify handler name attribute
		found := false
		for _, attr := range spans[0].Attributes {
			if string(attr.Key) == "remilia.handler.name" {
				assert.Equal(t, "ping", attr.Value.AsString())
				found = true
				break
			}
		}
		assert.True(t, found, "handler name attribute not found")
	})

	t.Run("passes through handler error", func(t *testing.T) {
		expectedErr := errors.New("handler error")
		handler := TracingHandler("ping", func(ctx *eventctx.Context) error {
			return expectedErr
		})

		ctx := testutil.CreateTestContext()
		err := handler(ctx)
		assert.ErrorIs(t, err, expectedErr)
	})

	t.Run("returns a handler with same signature", func(t *testing.T) {
		handler := TracingHandler("test", testutil.MockHandler(nil, 0))
		ctx := testutil.CreateTestContext()
		err := handler(ctx)
		assert.NoError(t, err)
	})
}

func TestGetTraceID(t *testing.T) {
	t.Run("returns empty for nil context", func(t *testing.T) {
		assert.Empty(t, GetTraceID(nil))
	})

	t.Run("returns empty when no tracing context", func(t *testing.T) {
		ctx := testutil.CreateTestContext()
		assert.Empty(t, GetTraceID(ctx))
	})

	t.Run("returns trace ID when tracing is active", func(t *testing.T) {
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
		prev := otel.GetTracerProvider()
		otel.SetTracerProvider(tp)
		t.Cleanup(func() { otel.SetTracerProvider(prev) })

		cfg := DefaultTracingConfig()
		handler := Tracing(cfg)(testutil.MockHandler(nil, 0))

		ctx := testutil.CreateTestContext()
		_ = handler(ctx)

		traceID := GetTraceID(ctx)
		assert.NotEmpty(t, traceID)
	})
}

func TestGetSpanID(t *testing.T) {
	t.Run("returns empty for nil context", func(t *testing.T) {
		assert.Empty(t, GetSpanID(nil))
	})

	t.Run("returns empty when no tracing context", func(t *testing.T) {
		ctx := testutil.CreateTestContext()
		assert.Empty(t, GetSpanID(ctx))
	})

	t.Run("returns span ID when tracing is active", func(t *testing.T) {
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
		prev := otel.GetTracerProvider()
		otel.SetTracerProvider(tp)
		t.Cleanup(func() { otel.SetTracerProvider(prev) })

		cfg := DefaultTracingConfig()
		handler := Tracing(cfg)(testutil.MockHandler(nil, 0))

		ctx := testutil.CreateTestContext()
		_ = handler(ctx)

		spanID := GetSpanID(ctx)
		assert.NotEmpty(t, spanID)
	})
}

// mockMiddleware is a simple middleware that does nothing.
func mockMiddleware(next eventctx.Handler) eventctx.Handler {
	return func(ctx *eventctx.Context) error {
		return next(ctx)
	}
}
