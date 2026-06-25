// Package tracing 提供基于 OpenTelemetry 的分布式追踪支持。
//
// 主要功能：
//   - 初始化 OTLP 追踪导出器
//   - 自适应采样器（根据错误率动态调整采样率）
//   - 与 Bot 生命周期绑定的 TracerProvider 管理
//
// 典型用法：
//
//	tp, err := tracing.NewTracerProvider(ctx, tracing.Config{
//	    Exporter: "otlp",
//	    Endpoint: "localhost:4317",
//	    ServiceName: "my-bot",
//	})
//	if err != nil { log.Fatal(err) }
//	defer tp.Shutdown(ctx)
package tracing

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// Config 追踪配置。
//
// 此类型同时作为 config.Config.Tracing 的配置结构体（config 包通过类型别名引用），
// 因此所有字段均带有 yaml/mapstructure tag，可直接被 YAML 反序列化。
type Config struct {
	// Enable 是否启用追踪（原 Enabled）。
	// yaml tag 同时支持 "enable" 和 "enabled" 向后兼容。
	Enable bool `yaml:"enable" mapstructure:"enable"`

	// ServiceName 服务名称
	ServiceName string `yaml:"service_name" mapstructure:"service_name"`

	// ServiceVersion 服务版本
	ServiceVersion string `yaml:"service_version" mapstructure:"service_version"`

	// Environment 环境（dev, staging, prod）
	Environment string `yaml:"environment" mapstructure:"environment"`

	// Exporter 导出器类型（otlp, stdout）
	Exporter string `yaml:"exporter" mapstructure:"exporter"`

	// Endpoint 追踪后端地址
	// OTLP: http://localhost:4318
	Endpoint string `yaml:"endpoint" mapstructure:"endpoint"`

	// SamplingRate 采样率 (0.0 - 1.0)
	// 1.0 = 100% 采样，0.1 = 10% 采样
	SamplingRate float64 `yaml:"sampling_rate" mapstructure:"sampling_rate"`

	// UseAdaptiveSampling 是否使用自适应采样
	// 启用后，SamplingRate 将作为基础采样率
	UseAdaptiveSampling bool `yaml:"use_adaptive_sampling" mapstructure:"use_adaptive_sampling"`

	// AdaptiveSamplerConfig 自适应采样器配置
	// 仅在 UseAdaptiveSampling = true 时有效
	AdaptiveSamplerConfig *AdaptiveSamplerConfig `yaml:"adaptive_sampler" mapstructure:"adaptive_sampler"`

	// IncludeEventDetail 是否在 Span 中包含事件详情（内容、作者等）
	IncludeEventDetail bool `yaml:"include_event_detail" mapstructure:"include_event_detail"`

	// Headers 额外的 HTTP 头（用于 OTLP 认证）
	Headers map[string]string `yaml:"headers" mapstructure:"headers"`
}

// Validate 验证追踪配置有效性
func (c *Config) Validate() error {
	if !c.Enable {
		return nil
	}
	if c.ServiceName == "" {
		return fmt.Errorf("tracing.service_name is required when tracing is enabled")
	}
	validExporters := map[string]bool{
		"otlp": true, "tempo": true, "grafana": true,
		"zipkin": true, "stdout": true, "console": true,
	}
	if !validExporters[c.Exporter] {
		return fmt.Errorf("tracing.exporter must be one of [otlp, tempo, grafana, zipkin, stdout, console], got '%s'", c.Exporter)
	}
	if c.Exporter != "stdout" && c.Exporter != "console" && c.Endpoint == "" {
		return fmt.Errorf("tracing.endpoint is required when exporter is '%s'", c.Exporter)
	}
	if c.SamplingRate < 0 || c.SamplingRate > 1 {
		return fmt.Errorf("tracing.sampling_rate must be between 0 and 1, got %f", c.SamplingRate)
	}
	return nil
}

// DefaultConfig 返回默认配置
func DefaultConfig() Config {
	return Config{
		Enable:         false,
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
	tp              *sdktrace.TracerProvider
	config          Config
	adaptiveSampler *AdaptiveSampler
}

// NewProvider 创建追踪提供者
func NewProvider(config Config) (*Provider, error) {
	if !config.Enable {
		logger.Info("[Tracing] Tracing is disabled, using no-op provider")
		// 创建 no-op provider 并设置为全局，保证 otel.Tracer() 行为一致
		tp := sdktrace.NewTracerProvider()
		otel.SetTracerProvider(tp)
		// 同样设置传播器，避免跨进程 trace 头被忽略
		otel.SetTextMapPropagator(
			propagation.NewCompositeTextMapPropagator(
				propagation.TraceContext{},
				propagation.Baggage{},
			),
		)
		return &Provider{
			tp:     tp,
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
	case "stdout", "console":
		exporter, err = createStdoutExporter()
	default:
		return nil, fmt.Errorf("unsupported exporter: %s", config.Exporter)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create exporter: %w", err)
	}

	// 创建采样器
	var sampler sdktrace.Sampler
	var adaptiveSampler *AdaptiveSampler

	if config.UseAdaptiveSampling {
		// 使用自适应采样器
		samplerConfig := DefaultAdaptiveSamplerConfig()
		if config.AdaptiveSamplerConfig != nil {
			samplerConfig = *config.AdaptiveSamplerConfig
		}
		// 使用配置的采样率作为基础采样率
		samplerConfig.BaseSamplingRate = config.SamplingRate

		adaptiveSampler = NewAdaptiveSampler(samplerConfig)
		sampler = adaptiveSampler

		logger.Info("[Tracing] Using adaptive sampling strategy")
	} else {
		// 使用固定采样率
		sampler = sdktrace.ParentBased(
			sdktrace.TraceIDRatioBased(config.SamplingRate),
		)
		logger.WithField("rate", config.SamplingRate).Info("[Tracing] Using fixed sampling rate")
	}

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
		"adaptive": config.UseAdaptiveSampling,
	}).Info("[Tracing] Tracing initialized")

	return &Provider{
		tp:              tp,
		config:          config,
		adaptiveSampler: adaptiveSampler,
	}, nil
}

// createOTLPExporter 创建 OTLP 导出器（支持 Tempo、Grafana Cloud）
func createOTLPExporter(config Config) (sdktrace.SpanExporter, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(config.Endpoint),
		otlptracehttp.WithTimeout(10 * time.Second),
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

// createStdoutExporter 创建控制台输出导出器（用于调试）
func createStdoutExporter() (sdktrace.SpanExporter, error) {
	// 使用简单的日志输出
	return &stdoutExporter{}, nil
}

// stdoutExporter 简单的控制台导出器
type stdoutExporter struct{}

func (e *stdoutExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
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

func (e *stdoutExporter) Shutdown(context.Context) error {
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
	return p.config.Enable
}

// SetSamplingRate 运行时更新采样率。
// 仅当启用了自适应采样时生效；固定采样率模式不支持运行时更新。
func (p *Provider) SetSamplingRate(rate float64) {
	if p.adaptiveSampler == nil {
		logger.WithField("rate", rate).Warn("[Tracing] Fixed sampling rate does not support runtime update, enable adaptive_sampling=true to change at runtime")
		return
	}
	p.adaptiveSampler.SetBaseSamplingRate(rate)
	p.config.SamplingRate = rate
	logger.WithField("rate", rate).Info("[Tracing] Sampling rate updated")
}

// GetAdaptiveSampler 获取自适应采样器
// 如果未启用自适应采样，返回 nil
func (p *Provider) GetAdaptiveSampler() *AdaptiveSampler {
	return p.adaptiveSampler
}

// StartAdaptiveMonitor 启动自适应采样监控
// 仅在启用自适应采样时有效
func (p *Provider) StartAdaptiveMonitor(ctx context.Context) {
	if p.adaptiveSampler == nil {
		logger.Warn("[Tracing] Adaptive sampling not enabled, monitor not started")
		return
	}

	logger.Info("[Tracing] Starting adaptive sampling monitor")
	p.adaptiveSampler.StartMonitor(ctx)
}

// GetSamplingStats 获取采样统计信息
func (p *Provider) GetSamplingStats() *AdaptiveSamplerStats {
	if p.adaptiveSampler == nil {
		return nil
	}

	return new(p.adaptiveSampler.GetStats())
}
