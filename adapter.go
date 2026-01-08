package remilia

import (
	"context"
	"net/http"
	"sync"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/openapi/protocol/webhook"
	"github.com/sirupsen/logrus"
)

// Adapter connects an event source to the Bot
type Adapter interface {
	// Start starts the adapter and pushes events to the handleFunc.
	// It should run asynchronously.
	Start(ctx context.Context, handleFunc func(*dto.Payload)) error

	// Shutdown stops the adapter gracefully.
	Shutdown(ctx context.Context) error
}

// WebhookAdapter implements Adapter for Webhook
type WebhookAdapter struct {
	wh     webhook.WebHook
	server *HTTPServer
	wg     sync.WaitGroup

	// startCtx controls the lifetime of the event loop started in Start.
	// We cancel it in Shutdown to guarantee the loop exits.
	//
	// Contract/Behavior:
	//   - Shutdown(ctx) guarantees that the goroutine started by Start() (the event loop)
	//     has exited before returning (or returns ctx.Err() if the shutdown context is done).
	//   - Shutdown(ctx) does NOT close the channel returned by wh.EventStream(); the
	//     channel's lifecycle is owned by the webhook implementation.
	//
	// Rationale:
	//   - WebHook is a pure event source interface; it may reuse its EventStream across
	//     multiple consumers or keep it open for the process lifetime.
	//   - Forcing the channel to close would require a breaking change to the WebHook API
	//     (e.g. adding Close/Shutdown).
	startCtx    context.Context
	startCancel context.CancelFunc

	startOnce    sync.Once
	shutdownOnce sync.Once
}

// NewWebhookAdapter creates a new WebhookAdapter
func NewWebhookAdapter(wh webhook.WebHook) *WebhookAdapter {
	return &WebhookAdapter{
		wh: wh,
	}
}

// Start starts the HTTP server and event loop
func (a *WebhookAdapter) Start(ctx context.Context, handleFunc func(*dto.Payload)) error {
	if a.wh == nil {
		return nil
	}

	// Create an internal context for the event loop so Shutdown can always stop it.
	// If caller already passed a cancellable ctx, we still wrap it to keep a local cancel func.
	a.startOnce.Do(func() {
		a.startCtx, a.startCancel = context.WithCancel(ctx)
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/", a.wh.Handle)
	a.server = NewHTTPServer(a.wh.Addr(), mux)

	// Start the HTTP server
	a.server.Start()

	// Start a goroutine to listen for events
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ch := a.wh.EventStream()

		for {
			select {
			case event, ok := <-ch:
				if !ok {
					logrus.Info("[WebhookAdapter] Event stream closed")
					return
				}
				logrus.WithFields(logrus.Fields{
					"type": event.Type,
					"id":   event.ID,
				}).Debug("[WebhookAdapter] Received event")

				handleFunc(event)
			case <-a.startCtx.Done():
				logrus.Info("[WebhookAdapter] Stopping event loop")
				return
			}
		}
	}()

	return nil
}

// Shutdown stops the HTTP server and waits for the event loop to exit.
func (a *WebhookAdapter) Shutdown(ctx context.Context) error {
	var shutdownErr error

	a.shutdownOnce.Do(func() {
		// 1) Stop the event loop first to avoid calling handleFunc while shutting down.
		if a.startCancel != nil {
			a.startCancel()
		}

		// 2) Shutdown the HTTP server.
		if a.server != nil {
			if err := a.server.Shutdown(ctx); err != nil {
				shutdownErr = err
				return
			}
		}

		// 3) Wait for Start() loop goroutine to exit, bounded by ctx.
		done := make(chan struct{})
		go func() {
			a.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			return
		case <-ctx.Done():
			shutdownErr = ctx.Err()
			return
		}
	})

	return shutdownErr
}
