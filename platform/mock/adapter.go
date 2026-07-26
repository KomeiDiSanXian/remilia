// Package mock provides shared mock implementations of platform interfaces for testing.
//
// Usage:
//
//	import "github.com/KomeiDiSanXian/remilia/platform/mock"
//
//	// Create adapter with default values
//	a := mock.NewAdapter()
//
//	// Custom configuration
//	a := mock.NewAdapter(
//	    mock.WithPlatform("discord"),
//	    mock.WithBotID("bot123"),
//	)
//
//	// Inject event into handler
//	a.InjectEvent(ctx, myEvent)
package mock

import (
	stdctx "context"
	"slices"
	"sync"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// AdapterCall records a single adapter method call for test assertions.
type AdapterCall struct {
	Method string
	Error  error
}

// AdapterOption configures a Adapter.
type AdapterOption func(*Adapter)

// WithPlatform sets the platform name returned by Platform().
func WithPlatform(name string) AdapterOption {
	return func(a *Adapter) { a.platform = name }
}

// WithBotID sets the bot ID returned by BotID().
func WithBotID(id string) AdapterOption {
	return func(a *Adapter) { a.botID = id }
}

// WithBotName sets the bot name returned by BotName().
func WithBotName(name string) AdapterOption {
	return func(a *Adapter) { a.botName = name }
}

// WithSender sets the Sender returned by Sender().
func WithSender(s platform.Sender) AdapterOption {
	return func(a *Adapter) { a.sender = s }
}

// WithCapabilities sets the capabilities returned by Capabilities().
func WithCapabilities(c platform.Capabilities) AdapterOption {
	return func(a *Adapter) { a.caps = c }
}

// WithHealthDetail sets the health detail returned by HealthDetail().
func WithHealthDetail(detail map[string]any) AdapterOption {
	return func(a *Adapter) { a.healthDetail = detail }
}

// WithStartError makes Start() return the given error.
func WithStartError(err error) AdapterOption {
	return func(a *Adapter) { a.startErr = err }
}

// WithStopError makes Stop() return the given error.
func WithStopError(err error) AdapterOption {
	return func(a *Adapter) { a.stopErr = err }
}

// WithDisconnectCallback pre-registers a disconnect callback.
func WithDisconnectCallback(fn func(error)) AdapterOption {
	return func(a *Adapter) { a.OnDisconnect(fn) }
}

// Adapter is a full mock implementation of platform.Adapter
// plus optional interfaces RecoverableAdapter, BotIdentity, and HealthDetailer.
//
// All calls are recorded in Calls for test assertions.
// Use InjectEvent to simulate platform events.
type Adapter struct {
	platform     string
	botID        string
	botName      string
	sender       platform.Sender
	caps         platform.Capabilities
	healthDetail map[string]any
	startErr     error
	stopErr      error

	mu           sync.Mutex
	Calls        []AdapterCall
	running      bool
	eventHandler func(platform.Event)

	platform.DisconnectNotifier
}

// NewAdapter creates a Adapter with sensible defaults.
func NewAdapter(opts ...AdapterOption) *Adapter {
	a := &Adapter{
		platform: "mock",
		sender:   &platform.NoopSender{},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Platform returns the mock platform name.
func (a *Adapter) Platform() string {
	return a.platform
}

// Start records the call and runs the event handler loop until ctx is done.
// If WithStartError was set, returns that error immediately.
func (a *Adapter) Start(ctx stdctx.Context, handler func(platform.Event)) error {
	a.mu.Lock()
	a.Calls = append(a.Calls, AdapterCall{Method: "Start"})
	if a.startErr != nil {
		err := a.startErr
		a.mu.Unlock()
		return err
	}
	a.eventHandler = handler
	a.running = true
	a.mu.Unlock()

	<-ctx.Done()

	a.mu.Lock()
	a.running = false
	a.mu.Unlock()
	return ctx.Err()
}

// Stop records the call and stops the adapter.
func (a *Adapter) Stop(ctx stdctx.Context) error {
	a.mu.Lock()
	a.Calls = append(a.Calls, AdapterCall{Method: "Stop"})
	a.running = false
	err := a.stopErr
	a.mu.Unlock()
	return err
}

// Sender returns the mock sender.
func (a *Adapter) Sender() platform.Sender {
	return a.sender
}

// Capabilities returns the mock capabilities.
func (a *Adapter) Capabilities() platform.Capabilities {
	return a.caps
}

// IsRunning returns the current running state.
func (a *Adapter) IsRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running
}

// BotID returns the mock bot ID (implements BotIdentity).
func (a *Adapter) BotID() string {
	return a.botID
}

// BotName returns the mock bot name (implements BotIdentity).
func (a *Adapter) BotName() string {
	return a.botName
}

// HealthDetail returns the mock health detail (implements HealthDetailer).
func (a *Adapter) HealthDetail() map[string]any {
	return a.healthDetail
}

// InjectEvent delivers an event to the registered handler.
// Returns false if the adapter hasn't been started yet.
func (a *Adapter) InjectEvent(event platform.Event) bool {
	a.mu.Lock()
	h := a.eventHandler
	a.mu.Unlock()
	if h == nil {
		return false
	}
	h(event)
	return true
}

// CalledTimes returns the number of times a method was called.
func (a *Adapter) CalledTimes(method string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, c := range a.Calls {
		if c.Method == method {
			n++
		}
	}
	return n
}

// ResetCalls clears the call history.
func (a *Adapter) ResetCalls() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Calls = nil
}

// Snapshot 返回已记录调用的副本，可安全地并发读取。
//
// 断言调用内容时请使用本方法，不要直接读取导出字段 Calls：
// 所有写入都在 a.mu 保护下进行，而直接读取 Calls 不持锁，
// 与内部的 append 构成数据竞争（-race 必报）。
func (a *Adapter) Snapshot() []AdapterCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.Calls)
}
