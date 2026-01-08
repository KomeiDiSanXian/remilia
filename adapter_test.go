package remilia

import (
	"context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockAdapter struct {
	mock.Mock
	handler func(*dto.Payload)
}

func (m *MockAdapter) Start(ctx context.Context, handler func(*dto.Payload)) error {
	args := m.Called(ctx, handler)
	m.handler = handler
	return args.Error(0)
}

func (m *MockAdapter) Shutdown(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func TestBot_WithAdapter(t *testing.T) {
	mockAdapter := new(MockAdapter)
	mockAdapter.On("Start", mock.Anything, mock.Anything).Return(nil)
	mockAdapter.On("Shutdown", mock.Anything).Return(nil)

	info := &dto.BotInfo{
		AppID:     12345,
		Token:     "test-token",
		AppSecret: "test-secret",
	}
	bot := New(info, WithAdapter(mockAdapter))

	// Test Start
	bot.Start()
	// Allow goroutine to start
	time.Sleep(10 * time.Millisecond)

	mockAdapter.AssertCalled(t, "Start", mock.Anything, mock.Anything)

	// Test Event Flow
	executed := false
	bot.OnAny(func(ctx *Context) bool {
		executed = true
		return true
	})

	// Simulate event from adapter
	if mockAdapter.handler != nil {
		mockAdapter.handler(&dto.Payload{})
	} else {
		t.Fatal("handler was not set on mockAdapter")
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)
	assert.True(t, executed)

	// Test Shutdown
	bot.Shutdown(context.Background())
	mockAdapter.AssertCalled(t, "Shutdown", mock.Anything)
}
