package remilia

import (
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// TestEnsureMatcherChainWithState_RaceCondition tests for race conditions in ensureMatcherChainWithState.
// It simulates multiple goroutines triggering chain rebuilding concurrently.
func TestEnsureMatcherChainWithState_RaceCondition(t *testing.T) {
	engine := NewEngine()

	// Add global middleware
	engine.Use(func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			return next(ctx)
		}
	})

	matcher := engine.On(dto.C2CMessageCreate).Handle(func(ctx *Context) {})

	// We will simulate concurrent event processing which triggers ensureMatcherChainWithState
	concurrency := 50
	iterations := 100
	var wg sync.WaitGroup

	start := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < iterations; j++ {
				// Trigger event processing
				// ProcessEvent calls executeMatcher -> ensureMatcherChainWithState
				payload := &dto.Payload{
					ID:   "test-event",
					Type: dto.C2CMessageCreate,
				}
				ctx := NewContext(payload, nil)
				engine.ProcessEvent(ctx)
			}
		}()
	}

	close(start)

	// Also concurrently update middleware to invalidate cache
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for j := 0; j < iterations/10; j++ {
			time.Sleep(time.Millisecond)
			matcher.Use(func(next HandlerE) HandlerE {
				return func(ctx *Context) error {
					return next(ctx)
				}
			})
		}
	}()

	wg.Wait()
}
