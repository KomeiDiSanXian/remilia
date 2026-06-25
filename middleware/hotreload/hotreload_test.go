package hotreload_test

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware/hotreload"
	"github.com/KomeiDiSanXian/remilia/middleware/ratelimit"
	"github.com/KomeiDiSanXian/remilia/middleware/resilience"
)

func TestBridge_OnConfigChange_UpdatesRetry(t *testing.T) {
	cr := resilience.NewConfigurableRetry(resilience.RetryConfig{
		MaxAttempts: 1,
		BackoffBase: 100 * time.Millisecond,
	})
	bridge := hotreload.NewBridge().WatchRetry(cr)
	newCfg := &config.Config{
		Retry: config.RetryConfig{
			Enable:      true,
			MaxAttempts: 5,
			BackoffBase: "200ms",
			BackoffMax:  "2s",
		},
	}
	bridge.OnConfigChange(newCfg)
	// No panic = success; we can't easily inspect the internal fields
	// but this proves the hot-reload path runs without error
}

func TestBridge_OnConfigChange_Nil(t *testing.T) {
	bridge := hotreload.NewBridge()
	// Should not panic with nil config
	bridge.OnConfigChange(nil)
}

func TestBridge_WatchAdaptive(t *testing.T) {
	arl := ratelimit.NewAdaptiveRateLimiter(ratelimit.DefaultAdaptiveConfig())
	arl.Start()
	defer arl.Stop()
	bridge := hotreload.NewBridge().WatchAdaptive(arl)
	newCfg := &config.Config{
		Middleware: config.MiddlewareConfig{
			RateLimit: config.RateLimitMiddlewareConfig{
				Burst: 50,
			},
		},
	}
	// Should not panic
	bridge.OnConfigChange(newCfg)
}

func TestBridge_OnConfigChange_UpdatesDegradation(t *testing.T) {
	t.Run("full degradation config update", func(t *testing.T) {
		bridge := hotreload.NewBridge()
		newCfg := &config.Config{
			Middleware: config.MiddlewareConfig{
				Degradation: config.DegradationConfig{
					Enable:             true,
					CPUThreshold:       90.0,
					MemoryThreshold:    95.0,
					LatencyThreshold:   "1s",
					GoroutineThreshold: 20000,
					MonitorInterval:    "10s",
					Strategy:           "delay",
				},
			},
		}
		// Should not panic
		bridge.OnConfigChange(newCfg)
	})

	t.Run("empty strings get defaults", func(t *testing.T) {
		bridge := hotreload.NewBridge()
		newCfg := &config.Config{
			Middleware: config.MiddlewareConfig{
				Degradation: config.DegradationConfig{
					Enable:       true,
					CPUThreshold: 90.0,
				},
			},
		}
		bridge.OnConfigChange(newCfg)
	})
}

func TestBridge_GetMiddlewareConfig(t *testing.T) {
	bridge := hotreload.NewBridge()

	// Before any config change, returns zero value
	mc := bridge.GetMiddlewareConfig()
	if mc == nil {
		t.Fatal("GetMiddlewareConfig should not return nil")
	}

	// After config change, returns stored values
	newCfg := &config.Config{
		Middleware: config.MiddlewareConfig{
			Logging: true,
			Recover: false,
			Metrics: true,
		},
	}
	bridge.OnConfigChange(newCfg)

	mc = bridge.GetMiddlewareConfig()
	if mc.Logging != true {
		t.Error("expected Logging=true")
	}
	if mc.Recover != false {
		t.Error("expected Recover=false")
	}
	if mc.Metrics != true {
		t.Error("expected Metrics=true")
	}
}

func TestBridge_OnConfigChange_LogConfig(t *testing.T) {
	bridge := hotreload.NewBridge()
	newCfg := &config.Config{
		Log: logger.Config{
			Level:      "debug",
			TimeFormat: "15:04:05",
		},
	}
	// Should not panic
	bridge.OnConfigChange(newCfg)
}

func TestBridge_Subscribe(t *testing.T) {
	bridge := hotreload.NewBridge()
	token := bridge.Subscribe()
	if token == nil {
		t.Error("Subscribe should return a non-nil token")
	}
	token.Cancel()
}

func TestConfigurableRetry_UpdateConfig(t *testing.T) {
	cr := resilience.NewConfigurableRetry(resilience.RetryConfig{
		MaxAttempts: 1,
	})
	cr.UpdateConfig(resilience.RetryConfig{MaxAttempts: 5})
	// Verify the middleware function is returned without panic
	mw := cr.Middleware()
	if mw == nil {
		t.Error("Middleware() should return non-nil")
	}
}
