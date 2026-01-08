package remilia

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// HTTPServer encapsulates the http.Server and its lifecycle management.
type HTTPServer struct {
	srv *http.Server
	wg  sync.WaitGroup
}

// NewHTTPServer creates a new HTTPServer.
func NewHTTPServer(addr string, handler http.Handler) *HTTPServer {
	return &HTTPServer{
		srv: &http.Server{
			Addr:    addr,
			Handler: handler,
		},
	}
}

// Start runs the HTTP server in a background goroutine.
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
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	shutdownCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		shutdownCtx, cancel = context.WithTimeout(ctx, 5*time.Second)
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
