package tracing

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// Config 追踪配置
type Config struct {
	// Enabled 是否启用追踪
	Enabled bool

	// ServiceName 服务名称
	ServiceName string

	// ServiceVersion 服务版本
	ServiceVersion string

	// Environment 环境（dev, staging, prod）
	Environment string

	// Exporter 导出器类型（otlp, zipkin, stdout）
	Exporter string

	// Endpoint 追踪后端地址
	// OTLP: http://localhost:4318
	// Zipkin: http://localhost:9411/api/v2/spans
	Endpoint string

	// SamplingRate 采样率 (0.0 - 1.0)
	// 1.0 = 100% 采样，0.1 = 10% 采样
	SamplingRate float64

	// Headers 额外的 HTTP 头（用于 OTLP）
	Headers map[string]string
}

// DefaultConfig 返回默认配置
func DefaultConfig() Config {
	return Config{
		Enabled:        false,
		ServiceName:    "remilia-bot",
		ServiceVersion: "1.0.0",
		Environment:    "development",
		Exporter:       "otlp",
		Endpoint:       "http://localhost:4318",
		SamplingRate:   1.0,
		Headers:        make(map[string]string),
	}
}

// Provider 追踪提供者
type Provider struct {
	tp     *sdktrace.TracerProvider
	config Config
}

// NewProvider 创建追踪提供者
func NewProvider(config Config) (*Provider, error) {
	if !config.Enabled {
		logger.Info("[Tracing] Tracing is disabled")
		return &Provider{
			tp:     sdktrace.NewTracerProvider(),
			config: config,
		}, nil
	}

	// 创建资源
	res, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(config.ServiceName),
			semconv.ServiceVersion(config.ServiceVersion),
			attribute.String("environment", config.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// 创建导出器
	var exporter sdktrace.SpanExporter
	switch config.Exporter {
	case "otlp", "tempo", "grafana":
		exporter, err = createOTLPExporter(config)
	case "zipkin":
		exporter, err = createZipkinExporter(config)
	case "stdout", "console":
		exporter, err = createStdoutExporter()
	default:
		return nil, fmt.Errorf("unsupported exporter: %s", config.Exporter)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create exporter: %w", err)
	}

	// 创建采样器
	sampler := sdktrace.ParentBased(
		sdktrace.TraceIDRatioBased(config.SamplingRate),
	)

	// 创建 TracerProvider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// 设置全局 TracerProvider
	otel.SetTracerProvider(tp)

	// 设置全局传播器（支持 W3C Trace Context）
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	logger.WithFields(logger.Fields{
		"service":  config.ServiceName,
		"exporter": config.Exporter,
		"endpoint": config.Endpoint,
		"sampling": config.SamplingRate,
	}).Info("[Tracing] Tracing initialized")

	return &Provider{
		tp:     tp,
		config: config,
	}, nil
}

// createOTLPExporter 创建 OTLP 导出器（支持 Tempo、Grafana Cloud）
func createOTLPExporter(config Config) (sdktrace.SpanExporter, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(config.Endpoint),
	}

	// 如果没有显式指定 https，默认使用 http（开发环境）
	if config.Endpoint != "" && len(config.Endpoint) > 8 && config.Endpoint[:5] != "https" {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	// 添加自定义头（用于认证等）
	if len(config.Headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(config.Headers))
	}

	return otlptracehttp.New(context.Background(), opts...)
}

// createZipkinExporter 创建 Zipkin 导出器
func createZipkinExporter(config Config) (sdktrace.SpanExporter, error) {
	return zipkin.New(config.Endpoint)
}

// createStdoutExporter 创建控制台输出导出器（用于调试）
func createStdoutExporter() (sdktrace.SpanExporter, error) {
	// 使用简单的日志输出
	return &stdoutExporter{}, nil
}

// stdoutExporter 简单的控制台导出器
type stdoutExporter struct{}

func (e *stdoutExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	for _, span := range spans {
		logger.WithFields(logger.Fields{
			"trace_id": span.SpanContext().TraceID().String(),
			"span_id":  span.SpanContext().SpanID().String(),
			"name":     span.Name(),
			"duration": span.EndTime().Sub(span.StartTime()),
		}).Debug("[Tracing] Span exported")
	}
	return nil
}

func (e *stdoutExporter) Shutdown(ctx context.Context) error {
	return nil
}

// Shutdown 关闭追踪提供者
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.tp == nil {
		return nil
	}

	// 设置超时
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := p.tp.Shutdown(shutdownCtx); err != nil {
		logger.WithError(err).Warn("[Tracing] Failed to shutdown tracer provider")
		return err
	}

	logger.Info("[Tracing] Tracer provider shut down successfully")
	return nil
}

// Tracer 获取 Tracer
func (p *Provider) Tracer(name string) trace.Tracer {
	if p.tp == nil {
		return otel.Tracer(name)
	}
	return p.tp.Tracer(name)
}

// IsEnabled 检查追踪是否启用
func (p *Provider) IsEnabled() bool {
	return p.config.Enabled
}
