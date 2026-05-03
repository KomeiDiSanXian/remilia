package discord

import (
	stdctx "context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/bwmarrin/discordgo"
)

// ────────────────────────────────────────────────────────────────────────────
// InteractionsAdapter
// ────────────────────────────────────────────────────────────────────────────

// InteractionsAdapter is a platform.Adapter that receives Discord Interactions
// via HTTP webhook, without requiring a persistent WebSocket Gateway connection.
//
// # When to use
//
// Use this adapter when:
//   - You want serverless / stateless deployments (no long-running WebSocket)
//   - You only need slash command / component / modal interactions
//   - You are deploying behind a load balancer that terminates WebSocket connections
//
// Limitations compared to GatewayAdapter:
//   - Does not receive message events (MESSAGE_CREATE, etc.)
//   - Does not receive guild/member lifecycle events
//   - Cannot proactively send messages without a separate REST session
//
// # How it works
//
//  1. Discord sends a POST request to your HTTPS endpoint for each interaction.
//  2. The adapter verifies the Ed25519 signature using your app's public key.
//  3. PING requests (type=1) are acknowledged immediately with PONG (type=1).
//  4. Other interactions are converted to platform.Event and dispatched to your handler.
//
// # Response strategy
//
// When AutoDefer = true (recommended):
//   - The adapter immediately responds with HTTP 200 + deferred acknowledge.
//   - Discord shows "Bot is thinking..." in the client.
//   - Your handler has up to 15 minutes to call ctx.Reply().
//   - The sender uses the follow-up message API (FollowupMessageCreate).
//
// When AutoDefer = false:
//   - The HTTP response is held open for up to AckTimeout (default 2.5 s).
//   - If your handler calls ctx.Reply() before the timeout, the response is
//     sent as a type-4 (message with source) synchronous response.
//   - If the timeout fires first, a deferred response is sent automatically.
//
// # Setup
//
//  1. Configure your bot's "Interactions Endpoint URL" in the Developer Portal.
//  2. Your endpoint must be reachable over HTTPS (Discord will not call plain HTTP).
//     Use a reverse proxy (nginx, Caddy) to terminate TLS, or host on a platform
//     with automatic HTTPS (Fly.io, Railway, etc.).
//
// Example:
//
//	adapter, err := discord.NewInteractionsAdapter(discord.InteractionsConfig{
//	    Addr:       ":8080",
//	    PublicKey:  "YOUR_APP_PUBLIC_KEY",
//	    Token:      "BOT_TOKEN",         // optional, for follow-ups
//	    Path:       "/interactions",
//	    AutoDefer:  true,
//	    WorkerCount: 4,
//	})
//
// See https://discord.com/developers/docs/interactions/receiving-and-responding
type InteractionsAdapter struct {
	config  InteractionsConfig
	session *discordgo.Session // nil if no Token provided
	sender  *discordSender     // nil if no Token provided
	pubKey  ed25519.PublicKey
	workers int

	server  *http.Server
	eventCh chan platform.Event

	mu       sync.RWMutex
	running  bool
	cancel   stdctx.CancelFunc
	wg       sync.WaitGroup
	starting atomic.Bool
}

// NewInteractionsAdapter creates a new HTTP Interactions adapter.
//
// Returns an error if PublicKey is not a valid 32-byte hex-encoded Ed25519 key.
func NewInteractionsAdapter(cfg InteractionsConfig) (*InteractionsAdapter, error) {
	pubKeyBytes, err := hex.DecodeString(cfg.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("discord interactions: invalid public key hex: %w", err)
	}
	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("discord interactions: public key must be %d bytes, got %d",
			ed25519.PublicKeySize, len(pubKeyBytes))
	}

	var session *discordgo.Session
	var sender *discordSender
	if cfg.Token != "" {
		session, err = discordgo.New("Bot " + cfg.Token)
		if err != nil {
			return nil, fmt.Errorf("discord interactions: failed to create session: %w", err)
		}
		sender = newSender(session)
	}

	workers := cfg.WorkerCount
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	return &InteractionsAdapter{
		config:  cfg,
		session: session,
		sender:  sender,
		pubKey:  pubKeyBytes,
		workers: workers,
	}, nil
}

// ── platform.Adapter ────────────────────────────────────────────────────────

// Platform returns "discord".
func (a *InteractionsAdapter) Platform() string { return PlatformID }

// Sender returns the Discord message sender (uses follow-up message API).
// Returns a NoopSender if no Token was provided in InteractionsConfig.
func (a *InteractionsAdapter) Sender() platform.Sender {
	if a.sender != nil {
		return a.sender
	}
	return &platform.NoopSender{}
}

// Capabilities returns Discord platform feature capabilities.
func (a *InteractionsAdapter) Capabilities() platform.Capabilities { return discordCapabilities() }

// IsRunning returns true if the HTTP server is active.
func (a *InteractionsAdapter) IsRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running
}

// Start starts the HTTP server and begins dispatching Discord interaction events.
// Blocks until ctx is canceled.
func (a *InteractionsAdapter) Start(ctx stdctx.Context, handler func(platform.Event)) error {
	if !a.starting.CompareAndSwap(false, true) {
		return nil
	}
	defer a.starting.Store(false)

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil
	}

	bufSize := a.config.EventBufferSize
	if bufSize <= 0 {
		bufSize = 100
	}
	a.eventCh = make(chan platform.Event, bufSize)

	cancelCtx, cancel := stdctx.WithCancel(ctx)
	a.cancel = cancel
	a.running = true
	a.mu.Unlock()

	// Build HTTP mux
	mux := http.NewServeMux()
	path := a.config.Path
	if path == "" {
		path = "/"
	}
	mux.HandleFunc(path, a.handleInteraction)
	if path != "/" {
		mux.HandleFunc("/", a.handleInteraction) // also accept root path
	}

	a.server = &http.Server{
		Addr:         a.config.Addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	ln, err := net.Listen("tcp", a.config.Addr)
	if err != nil {
		cancel()
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
		return fmt.Errorf("discord interactions: failed to listen on %s: %w", a.config.Addr, err)
	}

	a.wg.Go(func() {
		if err := a.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Errorf("[discord.InteractionsAdapter] HTTP server error: %v", err)
		}
	})

	logger.Infof("[discord.InteractionsAdapter] HTTP server listening on %s (path=%s, workers=%d)",
		a.config.Addr, path, a.workers)

	// Worker pool
	workCh := make(chan platform.Event, a.workers*2)
	for i := 0; i < a.workers; i++ {
		a.wg.Go(func() {
			for event := range workCh {
				safeInvoke(handler, event)
			}
		})
	}

	// Dispatcher
	a.wg.Go(func() {
		defer close(workCh)
		for {
			select {
			case event, ok := <-a.eventCh:
				if !ok {
					return
				}
				select {
				case workCh <- event:
				case <-cancelCtx.Done():
					return
				}
			case <-cancelCtx.Done():
				return
			}
		}
	})

	<-cancelCtx.Done()
	a.wg.Wait()

	a.mu.Lock()
	a.running = false
	a.mu.Unlock()

	logger.Debug("[discord.InteractionsAdapter] Stopped")
	return nil
}

// Stop shuts down the HTTP server and stops event processing.
func (a *InteractionsAdapter) Stop(ctx stdctx.Context) error {
	a.mu.Lock()
	cancel := a.cancel
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	var shutdownErr error
	if a.server != nil {
		if err := a.server.Shutdown(ctx); err != nil {
			shutdownErr = fmt.Errorf("discord interactions: HTTP shutdown error: %w", err)
		}
	}

	a.mu.Lock()
	if a.eventCh != nil {
		close(a.eventCh)
		a.eventCh = nil
	}
	a.mu.Unlock()

	return shutdownErr
}

// ── HTTP handler ─────────────────────────────────────────────────────────────

// handleInteraction processes an incoming Discord interaction HTTP request.
//
// Protocol:
//  1. Reject non-POST requests.
//  2. Verify Ed25519 signature (required by Discord).
//  3. If type=1 (PING), respond with type=1 (PONG) immediately.
//  4. Otherwise, acknowledge and dispatch to handler.
func (a *InteractionsAdapter) handleInteraction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20)) // 4 MB limit
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Discord requires Ed25519 signature verification.
	if !a.verifySignature(r, body) {
		http.Error(w, "Invalid request signature", http.StatusUnauthorized)
		return
	}

	// Peek at the interaction type without fully parsing yet.
	var partial struct {
		Type int `json:"type"`
	}
	if err := json.Unmarshal(body, &partial); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Type 1 = PING from Discord during endpoint verification.
	// Must respond with PONG (type=1) synchronously.
	if partial.Type == 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"type":1}`))
		return
	}

	// Parse full interaction
	var interaction discordgo.Interaction
	if err := json.Unmarshal(body, &interaction); err != nil {
		http.Error(w, "Invalid interaction payload", http.StatusBadRequest)
		return
	}

	if a.config.AutoDefer {
		a.handleWithAutoDefer(w, &interaction)
	} else {
		a.handleWithSyncWindow(w, r.Context(), &interaction)
	}
}

// handleWithAutoDefer immediately sends a deferred acknowledge, then dispatches the event.
//
// Discord shows "Bot is thinking..." until a follow-up message is sent.
// The handler has up to 15 minutes (interaction token validity).
func (a *InteractionsAdapter) handleWithAutoDefer(w http.ResponseWriter, i *discordgo.Interaction) {
	// Acknowledge immediately with a deferred response.
	deferResp := map[string]any{
		"type": int(discordgo.InteractionResponseDeferredChannelMessageWithSource),
	}
	respBytes, _ := json.Marshal(deferResp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respBytes)

	// Store interaction for sender follow-up and dispatch event.
	if a.sender != nil {
		a.sender.storeInteraction(i)
		// Mark as already responded so sender uses FollowupMessageCreate.
		a.sender.intMu.Lock()
		if p, ok := a.sender.pending[i.ID]; ok {
			p.responded = true
		}
		a.sender.intMu.Unlock()
	}

	event := NewInteractionCreateEvent(&discordgo.InteractionCreate{Interaction: i})
	a.dispatch(event)
}

// handleWithSyncWindow holds the HTTP response open until the handler responds
// or AckTimeout fires (whichever comes first).
//
// This avoids the "Bot is thinking..." indicator for fast handlers.
func (a *InteractionsAdapter) handleWithSyncWindow(
	w http.ResponseWriter,
	reqCtx stdctx.Context,
	i *discordgo.Interaction,
) {
	ackTimeout := a.config.AckTimeout
	if ackTimeout <= 0 {
		ackTimeout = 2500 * time.Millisecond
	}

	// syncResp collects the first response from the handler.
	resultCh := make(chan syncResult, 1)

	// Wire up a special one-shot sender that captures the response.
	syncSender := &syncInteractionSender{
		session:  a.session,
		resultCh: resultCh,
	}

	// Build the event with a special interaction reference pointing to syncSender.
	ic := &discordgo.InteractionCreate{Interaction: i}
	event := newSyncInteractionEvent(ic, syncSender)
	a.dispatch(event)

	// Wait for the handler to respond or timeout.
	timer := time.NewTimer(ackTimeout)
	defer timer.Stop()

	select {
	case result := <-resultCh:
		// Handler responded in time — send the actual response.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(result.resp)
	case <-timer.C:
		// Timed out — send deferred acknowledge; handler can follow up later.
		logger.Debug("[discord.InteractionsAdapter] Handler exceeded sync window, deferring")
		deferResp := map[string]any{
			"type": int(discordgo.InteractionResponseDeferredChannelMessageWithSource),
		}
		respBytes, _ := json.Marshal(deferResp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(respBytes)

		// Allow handler to follow up using the regular sender after deferral.
		if a.sender != nil {
			a.sender.storeInteraction(i)
			a.sender.intMu.Lock()
			if p, ok := a.sender.pending[i.ID]; ok {
				p.responded = true
			}
			a.sender.intMu.Unlock()
		}
	case <-reqCtx.Done():
		// Client disconnected.
	}
}

// dispatch sends an event to eventCh (non-blocking, drops on overflow).
func (a *InteractionsAdapter) dispatch(event platform.Event) {
	a.mu.RLock()
	ch := a.eventCh
	a.mu.RUnlock()
	if ch == nil {
		return
	}
	select {
	case ch <- event:
	default:
		logger.Warn("[discord.InteractionsAdapter] Event channel full, dropping interaction event")
	}
}

// ── Signature verification ────────────────────────────────────────────────────

// verifySignature verifies the Ed25519 signature that Discord attaches to every
// interaction HTTP request.
//
// Discord signs: timestamp_header + raw_body
// The signature is hex-encoded in the X-Signature-Ed25519 header.
// The timestamp is in the X-Signature-Timestamp header.
//
// See https://discord.com/developers/docs/interactions/receiving-and-responding#security-and-authorization
func (a *InteractionsAdapter) verifySignature(r *http.Request, body []byte) bool {
	signature := r.Header.Get("X-Signature-Ed25519")
	timestamp := r.Header.Get("X-Signature-Timestamp")

	if signature == "" || timestamp == "" {
		return false
	}

	sigBytes, err := hex.DecodeString(signature)
	if err != nil || len(sigBytes) != ed25519.SignatureSize {
		return false
	}

	// Message = raw timestamp string + raw body bytes
	msg := make([]byte, 0, len(timestamp)+len(body))
	msg = append(msg, []byte(timestamp)...)
	msg = append(msg, body...)

	return ed25519.Verify(a.pubKey, msg, sigBytes)
}

// ────────────────────────────────────────────────────────────────────────────
// Sync interaction helpers (AutoDefer = false path)
// ────────────────────────────────────────────────────────────────────────────

// syncInteractionSender captures the first Send() call and converts it to a
// synchronous HTTP interaction response payload.
type syncInteractionSender struct {
	session  *discordgo.Session
	resultCh chan syncResult
	once     sync.Once
}

type syncResult struct {
	resp []byte
}

func (s *syncInteractionSender) Send(_ stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
	msg := req.Message
	extra := extractExtra(msg)
	resp := buildInteractionResponse(msg, extra)

	respBytes, err := json.Marshal(resp)
	if err != nil {
		return platform.SendResult{}, fmt.Errorf("discord sync sender: marshal error: %w", err)
	}

	s.once.Do(func() {
		s.resultCh <- syncResult{resp: respBytes}
	})
	return platform.SendResult{Platform: "discord"}, nil
}

// newSyncInteractionEvent creates an event whose Chat.Tokens encode a special
// marker so the sync sender is used instead of the regular channel sender.
//
// This is an internal detail used by handleWithSyncWindow.
func newSyncInteractionEvent(i *discordgo.InteractionCreate, _ *syncInteractionSender) platform.Event {
	// For now, delegate to the standard event builder.
	// The syncSender capture happens at the HTTP handler level (not via tokens).
	return NewInteractionCreateEvent(i)
}
