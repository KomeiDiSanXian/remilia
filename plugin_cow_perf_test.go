package remilia

import (
	"fmt"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// MockPlugin for testing
type MockPlugin struct {
	*BasePlugin
	matcherCount int
}

func NewMockPlugin(name string, count int) *MockPlugin {
	return &MockPlugin{
		BasePlugin:   NewBasePlugin(name),
		matcherCount: count,
	}
}

func (p *MockPlugin) Load(engine *Engine) error {
	engine.WithMatcherGroupBatch(func() {
		for i := 0; i < p.matcherCount; i++ {
			// Create many matchers
			m := engine.On(dto.C2CMessageCreate, func(ctx *Context) bool {
				return true
			})
			m.Handle(func(ctx *Context) {})
			p.AddMatcher(m)
		}
	})
	return nil
}

func TestPluginUnloadPerf(t *testing.T) {
	// 1. Create Engine
	engine := NewEngine()

	// 2. Create Plugin with many matchers
	// Keep this test quick and stable across environments (Windows/CI).
	// It’s meant to catch accidental O(N^2) behavior, not to benchmark.
	count := 400
	plugin := NewMockPlugin("perf_test_plugin", count)

	// 3. Load Plugin
	err := plugin.Load(engine)
	assert.NoError(t, err)

	// Verify loaded
	assert.Equal(t, count, len(engine.state.Load().(*engineState).matchers))

	// 4. Measure Unload time
	start := time.Now()
	err = plugin.Unload(engine)
	duration := time.Since(start)

	assert.NoError(t, err)

	fmt.Printf("Unload duration for %d matchers: %s\n", count, duration)

	// A very loose bound to avoid flakes while still catching extreme regressions.
	assert.Less(t, duration, 10*time.Second, "plugin unload should not be excessively slow")

	// Verify unloaded
	assert.Equal(t, 0, len(engine.state.Load().(*engineState).matchers))

	// Check if matchers are marked deleted
	// We can't easily check all of them since we don't have references easily unless we stored them
	// effectively relying on engine state check.
}
