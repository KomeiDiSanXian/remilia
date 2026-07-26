package server

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
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
			// 必须设置读取超时：零值表示"永不超时"，攻击者只需缓慢地
			// 逐字节发送请求头即可长期占住连接和 goroutine（Slowloris）。
			// 本类型同时承载管理 API、health、metrics 等对外端点。
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			IdleTimeout:       120 * time.Second,
			MaxHeaderBytes:    1 << 20, // 1MB
			// 刻意不设 WriteTimeout：SSE 日志流等长连接需要持续写出。
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
	s.wg.Go(func() {
		logger.Infof("[Server] Listening on %s", s.srv.Addr)
		if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.WithError(err).Error("[Server] Failed to start HTTP server")
		}
	})
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

	logger.Debug("[Server] Shutting down HTTP server...")
	if err := s.srv.Shutdown(shutdownCtx); err != nil {
		logger.WithError(err).Warn("[Server] HTTP server shutdown error")
		return err
	}
	logger.Debug("[Server] HTTP server closed")

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
