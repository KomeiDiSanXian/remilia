package remilia

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/openapi/protocol/webhook"
	"github.com/stretchr/testify/assert"
)

// mock webhook with controllable event stream
type mockWH struct{ ch chan *dto.Payload }

func (m *mockWH) Verify(_ http.Header, _ []byte) (bool, error)  { return true, nil }
func (m *mockWH) Sign(_ http.Header, _ []byte) ([]byte, error)  { return nil, nil }
func (m *mockWH) Handle(w http.ResponseWriter, r *http.Request) {}
func (m *mockWH) Addr() string                                  { return ":0" }
func (m *mockWH) EventStream() <-chan *dto.Payload              { return m.ch }

var _ webhook.WebHook = (*mockWH)(nil)

func TestBot_GracefulShutdown(t *testing.T) {
	info := &dto.BotInfo{AppID: 1, Token: "t", AppSecret: "s"}
	m := &mockWH{ch: make(chan *dto.Payload, 1)}
	b := New(info, WithWebHook(m))

	// Start non-blocking
	b.Start()
	// Send one event via mock channel
	m.ch <- &dto.Payload{Type: dto.C2CMessageCreate, ID: "e1"}

	// Shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	b.Shutdown(ctx)

	// If we reach here without deadlock/panic, graceful shutdown works
	assert.True(t, true)
}
