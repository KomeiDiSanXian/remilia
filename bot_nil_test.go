package remilia

import (
	"context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
)

// TestNewBot_NilAdapter tests that NewBot does NOT panic when adapter is nil.
// Nil adapter is valid for registry-only (multi-platform) usage.
func TestNewBot_NilAdapter(t *testing.T) {
	eng := engine.NewEngine()

	assert.NotPanics(t, func() {
		bot := NewBot(nil, eng)
		assert.NotNil(t, bot)
	}, "NewBot should not panic when adapter is nil (registry-only mode)")
}

// TestNewBot_NilEngine tests that NewBot panics when engine is nil
func TestNewBot_NilEngine(t *testing.T) {
	// Create a minimal test adapter
	adapter := &testAdapter{}

	assert.Panics(t, func() {
		NewBot(adapter, nil)
	}, "NewBot should panic when engine is nil")
}

// TestNewBot_BothNil tests that NewBot panics when both are nil
func TestNewBot_BothNil(t *testing.T) {
	assert.Panics(t, func() {
		NewBot(nil, nil)
	}, "NewBot should panic when both adapter and engine are nil")
}

// testAdapter is a minimal Adapter implementation for testing
type testAdapter struct{}

func (a *testAdapter) Platform() string                                      { return "test" }
func (a *testAdapter) Start(_ context.Context, _ func(platform.Event)) error { return nil }
func (a *testAdapter) Stop(_ context.Context) error                          { return nil }
func (a *testAdapter) Sender() platform.Sender                               { return &platform.NoopSender{} }
func (a *testAdapter) Capabilities() platform.Capabilities {
	return platform.Capabilities{}
}
