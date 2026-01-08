package remilia

import (
	"fmt"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestEngine_DeleteMatchers(t *testing.T) {
	e := NewEngine()
	defer e.Close()

	// Create 10 matchers
	var matchers []*Matcher
	for i := 0; i < 10; i++ {
		m := e.On(dto.C2CMessageCreate, func(ctx *Context) bool { return true })
		matchers = append(matchers, m)
	}

	assert.Equal(t, 10, e.GetMatcherCount())

	// Delete 5 matchers
	toDelete := matchers[:5]
	e.DeleteMatchers(toDelete)

	assert.Equal(t, 5, e.GetMatcherCount())

	// Verify the remaining matchers are the correct ones
	state := e.state.Load().(*engineState)
	for _, m := range state.matchers {
		found := false
		for _, deleted := range toDelete {
			if m == deleted {
				found = true
				break
			}
		}
		assert.False(t, found, "Deleted matcher should not be in state")
	}
}

func TestEngine_AsyncDelete(t *testing.T) {
	e := NewEngine()
	defer e.Close()

	// Create a temporary matcher
	done := make(chan bool, 1)
	e.On(dto.C2CMessageCreate, func(ctx *Context) bool { return true }).
		SetTemp(true).
		Handle(func(ctx *Context) {
			done <- true
		})

	assert.Equal(t, 1, e.GetTempMatcherCount())

	// Trigger the matcher
	ctx := &Context{
		event: &dto.Payload{
			Type: dto.C2CMessageCreate,
		},
	}
	fmt.Println("Processing event...")
	e.ProcessEvent(ctx)
	fmt.Println("Event processed")

	select {
	case <-done:
		fmt.Println("Handler executed")
	case <-time.After(1 * time.Second):
		t.Fatal("Handler not executed")
	}

	// Wait for async deletion (ticker is 100ms)
	assert.Eventually(t, func() bool {
		return e.GetTempMatcherCount() == 0
	}, 1*time.Second, 50*time.Millisecond, "Matcher should be deleted asynchronously")
}

func TestEngine_CleanExpiredMatchers_Batch(t *testing.T) {
	e := NewEngine()
	defer e.Close()
	e.SetTempMatcherCleanInterval(0) // Disable auto cleaner to control manually

	// Create 10 expired matchers
	for i := 0; i < 10; i++ {
		m := e.On(dto.C2CMessageCreate, func(ctx *Context) bool { return true })
		// Manually set expiration to past
		m.rt.mu.Lock()
		m.rt.expiresAt = time.Now().Add(-1 * time.Hour)
		m.rt.mu.Unlock()

		m.SetTemp(true)
	}

	// Create 5 valid matchers
	for i := 0; i < 5; i++ {
		e.On(dto.C2CMessageCreate, func(ctx *Context) bool { return true }).
			SetTempWithTimeout(1 * time.Hour)
	}

	assert.Equal(t, 15, e.GetTempMatcherCount())

	// Run cleaner
	e.cleanExpiredMatchers()

	assert.Equal(t, 5, e.GetTempMatcherCount(), "Expired matchers should be removed")
}
