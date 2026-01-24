package server

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// HTTPServer encapsulates the http.Server and its lifecycle management.
//
// It provides convenient Start() and Shutdown() methods for managing
// the HTTP server lifecycle in a concurrent-safe manner.
type HTTPServer struct {
	srv             *http.Server
	wg              sync.WaitGroup
	shutdownTimeout time.Duration // 关闭超时时间
}

// NewHTTPServer creates a new HTTPServer.
//
// Parameters:
//   - addr: the address to listen on (e.g., "localhost:8080")
//   - handler: the HTTP handler to use (can be nil for default ServeMux)
//
// Example:
//
//	server := server.NewHTTPServer("localhost:8080", myHandler)
//	server.Start()
//	defer server.Stop(context.Background())
func NewHTTPServer(addr string, handler http.Handler) *HTTPServer {
	return &HTTPServer{
		srv: &http.Server{
			Addr:    addr,
			Handler: handler,
		},
		shutdownTimeout: 5 * time.Second, // 默认 5 秒
	}
}

// WithShutdownTimeout 设置关闭超时时间
func (s *HTTPServer) WithShutdownTimeout(timeout time.Duration) *HTTPServer {
	s.shutdownTimeout = timeout
	return s
}

// Start runs the HTTP server in a background goroutine.
//
// This method is non-blocking and returns immediately.
// The server will start accepting connections in the background.
func (s *HTTPServer) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		logrus.Infof("[Server] Listening on %s", s.srv.Addr)
		if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logrus.WithError(err).Error("[Server] Failed to start HTTP server")
		}
	}()
}

// Shutdown gracefully shuts down the server.
//
// This method blocks until the server has finished shutting down or
// the context is canceled.
//
// If the provided context has no deadline, a default timeout configured
// in the server (default: 5 seconds) is used.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//	if err := server.Stop(ctx); err != nil {
//	    log.Printf("Server shutdown error: %v", err)
//	}
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	shutdownCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		shutdownCtx, cancel = context.WithTimeout(ctx, s.shutdownTimeout)
		defer cancel()
	}

	logrus.Debug("[Server] Shutting down HTTP server...")
	if err := s.srv.Shutdown(shutdownCtx); err != nil {
		logrus.WithError(err).Warn("[Server] HTTP server shutdown error")
		return err
	}
	logrus.Debug("[Server] HTTP server closed")

	// Wait for the server goroutine to exit
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-shutdownCtx.Done():
		return shutdownCtx.Err()
	}
}
