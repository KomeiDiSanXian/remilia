package tracing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTracingConfig_DefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.False(t, cfg.Enable)
	assert.Equal(t, "remilia-bot", cfg.ServiceName)
	assert.Equal(t, "1.0.0", cfg.ServiceVersion)
	assert.Equal(t, "development", cfg.Environment)
	assert.Equal(t, "otlp", cfg.Exporter)
	assert.Equal(t, "http://localhost:4318", cfg.Endpoint)
	assert.Equal(t, 1.0, cfg.SamplingRate)
	assert.NotNil(t, cfg.Headers)
	assert.Empty(t, cfg.Headers)
}

func TestTracingConfig_Validate_Disabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enable = false
	assert.NoError(t, cfg.Validate())
}

func TestTracingConfig_Validate_Enabled_MissingServiceName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enable = true
	cfg.ServiceName = ""
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "service_name")
}

func TestTracingConfig_Validate_Enabled_InvalidExporter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enable = true
	cfg.Exporter = "invalid"
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exporter")
}

func TestTracingConfig_Validate_Enabled_InvalidSamplingRate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enable = true
	cfg.SamplingRate = 1.5
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sampling_rate")
}

func TestTracingConfig_Validate_Enabled_Valid(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enable = true
	assert.NoError(t, cfg.Validate())
}

func TestTracingConfig_NewProvider_Disabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enable = false
	provider, err := NewProvider(cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.False(t, provider.IsEnabled())
	assert.NotNil(t, provider.Tracer("test"))
	assert.NoError(t, provider.Shutdown(context.Background()))
}

func TestTracingConfig_NewProvider_StdoutExporter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enable = true
	cfg.Exporter = "stdout"
	provider, err := NewProvider(cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.True(t, provider.IsEnabled())
	assert.NotNil(t, provider.Tracer("test"))
	assert.NoError(t, provider.Shutdown(context.Background()))
}

func TestTracingConfig_NewProvider_ConsoleExporter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enable = true
	cfg.Exporter = "console"
	provider, err := NewProvider(cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.True(t, provider.IsEnabled())
	assert.NoError(t, provider.Shutdown(context.Background()))
}

func TestTracingConfig_DefaultAdaptiveSamplerConfig(t *testing.T) {
	cfg := DefaultAdaptiveSamplerConfig()
	assert.Equal(t, 0.1, cfg.BaseSamplingRate)
	assert.Equal(t, 0.01, cfg.MinSamplingRate)
	assert.Equal(t, 1.0, cfg.MaxSamplingRate)
	assert.Equal(t, 0.05, cfg.ErrorThreshold)
	assert.Equal(t, 0.5, cfg.HighErrorSamplingRate)
	assert.True(t, cfg.AlwaysSampleErrors)
}
