package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findAvailableAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func TestNewHTTPServer(t *testing.T) {
	t.Run("with valid addr and nil handler", func(t *testing.T) {
		s := NewHTTPServer(":8080", nil)
		require.NotNil(t, s)
		assert.NotNil(t, s.srv)
		assert.Equal(t, ":8080", s.srv.Addr)
		assert.Nil(t, s.srv.Handler)
	})

	t.Run("with valid addr and simple handler", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		s := NewHTTPServer("127.0.0.1:9000", handler)
		require.NotNil(t, s)
		assert.Equal(t, "127.0.0.1:9000", s.srv.Addr)
		assert.NotNil(t, s.srv.Handler)
	})
}

func TestWithShutdownTimeout(t *testing.T) {
	t.Run("returns same instance", func(t *testing.T) {
		s := NewHTTPServer(":0", nil)
		result := s.WithShutdownTimeout(10 * time.Second)
		assert.Same(t, s, result)
	})

	t.Run("default timeout is 5 seconds", func(t *testing.T) {
		s := NewHTTPServer(":0", nil)
		assert.Equal(t, 5*time.Second, s.shutdownTimeout)
	})

	t.Run("sets timeout correctly", func(t *testing.T) {
		s := NewHTTPServer(":0", nil)
		s.WithShutdownTimeout(100 * time.Millisecond)
		assert.Equal(t, 100*time.Millisecond, s.shutdownTimeout)
	})
}

func TestStartShutdown(t *testing.T) {
	addr := findAvailableAddr(t)
	s := NewHTTPServer(addr, nil)
	s.Start()

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := s.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestShutdownWithoutStart(t *testing.T) {
	s := NewHTTPServer(":0", nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := s.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestServerHandlesRequests(t *testing.T) {
	addr := findAvailableAddr(t)

	var mu sync.Mutex
	requestReceived := false

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		requestReceived = true
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	s := NewHTTPServer(addr, handler)
	s.Start()

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://" + addr)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ok", string(body))

	mu.Lock()
	assert.True(t, requestReceived)
	mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = s.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestShutdownPreCancelledContext(t *testing.T) {
	addr := findAvailableAddr(t)
	s := NewHTTPServer(addr, nil)
	s.Start()
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.Shutdown(ctx)
	assert.Error(t, err)

	cleanCtx, cleanCancel := context.WithTimeout(context.Background(), time.Second)
	defer cleanCancel()
	_ = s.Shutdown(cleanCtx)
}

func TestMultipleShutdownCalls(t *testing.T) {
	addr := findAvailableAddr(t)
	s := NewHTTPServer(addr, nil)
	s.Start()
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err1 := s.Shutdown(ctx)
	assert.NoError(t, err1)

	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	err2 := s.Shutdown(ctx2)
	assert.NoError(t, err2)
}
