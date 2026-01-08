package remilia

import (
	"context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"
)

func TestEngine_Shutdown_WaitsForInFlightEvents(t *testing.T) {
	e := NewEngine()

	release := make(chan struct{})
	started := make(chan struct{})

	e.OnAny().Handle(func(ctx *Context) {
		close(started)
		<-release
	})

	content := "hello"
	detail, _ := sjson.SetBytes([]byte("{}"), "content", content)
	ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate, Detail: detail}, nil)

	done := make(chan struct{})
	go func() {
		e.ProcessEvent(ctx)
		close(done)
	}()

	<-started

	shutdownDone := make(chan struct{})
	go func() {
		err := e.Shutdown(context.Background())
		require.NoError(t, err)
		close(shutdownDone)
	}()

	// 确保在 handler 未释放前 Shutdown 不会提前返回
	select {
	case <-shutdownDone:
		t.Fatalf("Shutdown returned before in-flight event finished")
	case <-time.After(50 * time.Millisecond):
		// ok
	}

	close(release)

	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 2*time.Second, 10*time.Millisecond)

	require.Eventually(t, func() bool {
		select {
		case <-shutdownDone:
			return true
		default:
			return false
		}
	}, 2*time.Second, 10*time.Millisecond)
}

func TestEngine_Shutdown_ContextTimeout(t *testing.T) {
	e := NewEngine()

	release := make(chan struct{})
	started := make(chan struct{})

	e.OnAny().Handle(func(ctx *Context) {
		close(started)
		<-release
	})

	detail, _ := sjson.SetBytes([]byte("{}"), "content", "hello")
	ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate, Detail: detail}, nil)

	go e.ProcessEvent(ctx)
	<-started

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := e.Shutdown(shutdownCtx)
	require.Error(t, err)

	close(release)
}

func TestBot_Shutdown_UsesEngineShutdown(t *testing.T) {
	e := NewEngine()
	b := &Bot{engine: e}

	release := make(chan struct{})
	started := make(chan struct{})
	e.OnAny().Handle(func(ctx *Context) {
		close(started)
		<-release
	})

	detail, _ := sjson.SetBytes([]byte("{}"), "content", "hello")
	ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate, Detail: detail}, nil)
	go e.ProcessEvent(ctx)
	<-started

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		b.Shutdown(shutdownCtx)
		close(done)
	}()

	// 确保在 handler 未释放前 Shutdown 不会提前返回
	select {
	case <-done:
		t.Fatalf("Bot.Shutdown returned before engine finished")
	case <-time.After(50 * time.Millisecond):
		// ok
	}

	close(release)

	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 2*time.Second, 10*time.Millisecond)
}
