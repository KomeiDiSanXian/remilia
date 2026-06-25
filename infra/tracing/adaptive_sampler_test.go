package tracing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAdaptiveSampler_SetBaseSamplingRate(t *testing.T) {
	t.Run("updates base rate and current rate", func(t *testing.T) {
		as := NewAdaptiveSampler(DefaultAdaptiveSamplerConfig())
		assert.Equal(t, 0.1, as.GetCurrentSamplingRate())

		as.SetBaseSamplingRate(0.5)
		assert.Equal(t, 0.5, as.baseSamplingRate)
		assert.Equal(t, 0.5, as.GetCurrentSamplingRate())
	})

	t.Run("clamps to MinSamplingRate", func(t *testing.T) {
		cfg := DefaultAdaptiveSamplerConfig()
		cfg.MinSamplingRate = 0.05
		as := NewAdaptiveSampler(cfg)

		as.SetBaseSamplingRate(0.01)
		assert.InDelta(t, 0.05, as.GetCurrentSamplingRate(), 0.001)
	})

	t.Run("clamps to MaxSamplingRate", func(t *testing.T) {
		cfg := DefaultAdaptiveSamplerConfig()
		cfg.MaxSamplingRate = 0.8
		as := NewAdaptiveSampler(cfg)

		as.SetBaseSamplingRate(1.0)
		assert.InDelta(t, 0.8, as.GetCurrentSamplingRate(), 0.001)
	})

	t.Run("zero rate defaults to 0.01", func(t *testing.T) {
		as := NewAdaptiveSampler(DefaultAdaptiveSamplerConfig())
		as.SetBaseSamplingRate(0)
		assert.InDelta(t, 0.01, as.GetCurrentSamplingRate(), 0.001)
	})

	t.Run("rebuilds dynamic sampler", func(t *testing.T) {
		as := NewAdaptiveSampler(DefaultAdaptiveSamplerConfig())
		old := as.dynamicSampler.Load()
		as.SetBaseSamplingRate(0.75)
		new := as.dynamicSampler.Load()
		assert.NotEqual(t, old, new)
	})

	t.Run("stats reflect updated rates", func(t *testing.T) {
		as := NewAdaptiveSampler(DefaultAdaptiveSamplerConfig())
		as.SetBaseSamplingRate(0.3)
		stats := as.GetStats()
		assert.InDelta(t, 0.3, stats.BaseSamplingRate, 0.001)
		assert.InDelta(t, 0.3, stats.CurrentSamplingRate, 0.001)
	})
}

func TestAdaptiveSampler_AdjustSamplingRate(t *testing.T) {
	t.Run("below error threshold uses base rate", func(t *testing.T) {
		cfg := DefaultAdaptiveSamplerConfig()
		cfg.BaseSamplingRate = 0.2
		cfg.ErrorThreshold = 0.1
		as := NewAdaptiveSampler(cfg)

		as.totalSpans.Store(100)
		as.errorSpans.Store(5) // 5% < 10%
		as.AdjustSamplingRate()
		assert.InDelta(t, 0.2, as.GetCurrentSamplingRate(), 0.001)
	})

	t.Run("above error threshold uses high error rate", func(t *testing.T) {
		cfg := DefaultAdaptiveSamplerConfig()
		cfg.BaseSamplingRate = 0.2
		cfg.ErrorThreshold = 0.1
		cfg.HighErrorSamplingRate = 0.8
		as := NewAdaptiveSampler(cfg)

		as.totalSpans.Store(100)
		as.errorSpans.Store(30) // 30% > 10%
		as.AdjustSamplingRate()
		assert.InDelta(t, 0.8, as.GetCurrentSamplingRate(), 0.001)
	})

	t.Run("no spans does nothing", func(t *testing.T) {
		cfg := DefaultAdaptiveSamplerConfig()
		cfg.BaseSamplingRate = 0.2
		as := NewAdaptiveSampler(cfg)
		as.SetBaseSamplingRate(0.5)

		as.totalSpans.Store(0)
		as.AdjustSamplingRate()
		assert.InDelta(t, 0.5, as.GetCurrentSamplingRate(), 0.001)
	})
}

func TestAdaptiveSampler_NewWithDefaults(t *testing.T) {
	as := NewAdaptiveSampler(AdaptiveSamplerConfig{})
	assert.NotNil(t, as)
	assert.InDelta(t, 0.1, as.baseSamplingRate, 0.001)
	assert.InDelta(t, 0.01, as.config.MinSamplingRate, 0.001)
	assert.InDelta(t, 1.0, as.config.MaxSamplingRate, 0.001)
	assert.InDelta(t, 0.05, as.config.ErrorThreshold, 0.001)
	assert.InDelta(t, 0.5, as.config.HighErrorSamplingRate, 0.001)
	assert.Equal(t, 1*time.Minute, as.config.AdjustInterval)
	// AlwaysSampleErrors is not set during validation; zero value struct keeps false
	assert.False(t, as.config.AlwaysSampleErrors)
}
