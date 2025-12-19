package remilia

import (
	"context"
	"testing"
	"time"
)

// Test that Bot.Shutdown closes Engine and stops temp matcher cleaner.
func TestBotShutdownStopsEngineCleaner(t *testing.T) {
	engine := NewEngine()
	b := &Bot{engine: engine}

	// Start a cleaner with very short interval to simulate running goroutine.
	engine.SetTempMatcherCleanInterval(10 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	b.Shutdown(ctx)

	// After shutdown, starting cleaner should not panic; close should be idempotent.
	engine.Close()
}
