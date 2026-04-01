// Package storage provides a namespaced, type-safe data persistence layer
// for Remilia plugins.
//
// # Architecture
//
// The package is organized in three layers:
//
//  1. [Backend] — raw bytes + context. What storage providers implement.
//     Built-in backends: [MemoryStorage], [SQLiteStorage], [RedisStorage].
//
//  2. [Store] — namespaced, typed view. What plugin consumers use.
//     Obtained via [Plugin.NS]; handles key prefixing and serialization
//     automatically. Use the generic functions [Get], [Set], [GetOrDefault]
//     for type-safe access without manual JSON.
//
//  3. [Plugin] — the plugin API, registered as "storage" in the plugin manager.
//
// # Quick start
//
//	// Register with a SQLite backend
//	db, _ := storage.NewSQLiteStorage("bot.db")
//	pm.Register(storage.NewWithBackend(db))
//
//	// In another plugin's Setup — get a namespaced store
//	s := plugin.Must[storage.Plugin](ctx, "storage")
//	store := s.NS("my-plugin")           // all keys prefixed with "my-plugin:"
//
//	// Type-safe read/write, no manual JSON
//	storage.Set(ctx, store, "config", myConfig, 0)
//	cfg, err := storage.Get[MyConfig](ctx, store, "config")
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/plugin"
)

// ─── Layer 1: Backend ──────────────────────────────────────────────────────

// Backend is the minimal interface that storage providers must implement.
// All operations accept a [context.Context] for timeout and cancellation control.
//
// Plugin consumers should NOT use Backend directly; use [Store] (via [Plugin.NS]) instead.
type Backend interface {
	// Get retrieves the raw bytes stored under key.
	// Returns [ErrNotFound] if the key does not exist, [ErrExpired] if it has expired.
	Get(ctx context.Context, key string) ([]byte, error)

	// Set stores value under key with an optional TTL.
	// ttl=0 means the key never expires.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete removes key. No-op if the key does not exist.
	Delete(ctx context.Context, key string) error

	// Exists reports whether key exists and has not expired.
	Exists(ctx context.Context, key string) bool

	// Keys returns all non-expired keys matching the glob pattern.
	Keys(ctx context.Context, pattern string) ([]string, error)

	// Close releases any resources held by the backend (called during plugin Teardown).
	Close() error
}

// CleanableBackend is an optional extension for backends that support active
// expired-key cleanup. Implement this to enable the background cleanup goroutine.
type CleanableBackend interface {
	Backend
	CleanExpired(ctx context.Context) (int, error)
}

// ─── Codec ─────────────────────────────────────────────────────────────────

// Codec handles serialization and deserialization of stored values.
type Codec interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte, v any) error
}

// JSONCodec is the default [Codec] using [encoding/json].
type JSONCodec struct{}

func (JSONCodec) Marshal(v any) ([]byte, error)      { return json.Marshal(v) }
func (JSONCodec) Unmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

// ─── Layer 2: Store ────────────────────────────────────────────────────────

// Store is a namespaced, codec-aware view over a [Backend].
//
// Obtain a Store via [Plugin.NS]. All keys are automatically prefixed with
// "<namespace>:", preventing collisions between plugins sharing the same backend.
//
//	store := storagePlugin.NS("acl")
//	storage.Set(ctx, store, "entries", data, 0) // backend key: "acl:entries"
type Store struct {
	backend Backend
	prefix  string
	codec   Codec
}

func (s *Store) fullKey(k string) string { return s.prefix + k }

// Delete removes key from this namespace.
func (s *Store) Delete(ctx context.Context, key string) error {
	return s.backend.Delete(ctx, s.fullKey(key))
}

// Exists reports whether key exists and has not expired.
func (s *Store) Exists(ctx context.Context, key string) bool {
	return s.backend.Exists(ctx, s.fullKey(key))
}

// Keys returns all non-expired keys matching the glob pattern within this namespace.
// Returned keys have the namespace prefix stripped.
func (s *Store) Keys(ctx context.Context, pattern string) ([]string, error) {
	rawKeys, err := s.backend.Keys(ctx, s.fullKey(pattern))
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(rawKeys))
	for i, k := range rawKeys {
		keys[i] = strings.TrimPrefix(k, s.prefix)
	}
	return keys, nil
}

// Clear removes all keys within this namespace only.
// Keys in other namespaces are NOT affected.
func (s *Store) Clear(ctx context.Context) error {
	rawKeys, err := s.backend.Keys(ctx, s.fullKey("*"))
	if err != nil {
		return err
	}
	for _, fullKey := range rawKeys {
		if err := s.backend.Delete(ctx, fullKey); err != nil {
			return err
		}
	}
	return nil
}

// GetRaw retrieves raw bytes. Prefer the generic [Get] for typed access.
func (s *Store) GetRaw(ctx context.Context, key string) ([]byte, error) {
	return s.backend.Get(ctx, s.fullKey(key))
}

// SetRaw stores raw bytes. Prefer the generic [Set] for typed access.
// ttl=0 means no expiry.
func (s *Store) SetRaw(ctx context.Context, key string, data []byte, ttl time.Duration) error {
	return s.backend.Set(ctx, s.fullKey(key), data, ttl)
}

// ─── Generic helpers ───────────────────────────────────────────────────────

// Get retrieves and unmarshals a value of type T from the store.
//
//	type BanList map[string]time.Time
//	bans, err := storage.Get[BanList](ctx, store, "bans")
func Get[T any](ctx context.Context, s *Store, key string) (T, error) {
	var zero T
	data, err := s.backend.Get(ctx, s.fullKey(key))
	if err != nil {
		return zero, err
	}
	if err := s.codec.Unmarshal(data, &zero); err != nil {
		return zero, err
	}
	return zero, nil
}

// Set marshals val and stores it under key with optional TTL.
// ttl=0 means no expiry.
//
//	err := storage.Set(ctx, store, "bans", banList, 0)
func Set[T any](ctx context.Context, s *Store, key string, val T, ttl time.Duration) error {
	data, err := s.codec.Marshal(val)
	if err != nil {
		return err
	}
	return s.backend.Set(ctx, s.fullKey(key), data, ttl)
}

// GetOrDefault returns the stored value, or def if the key is missing or any error occurs.
//
//	count := storage.GetOrDefault(ctx, store, "counter", 0)
func GetOrDefault[T any](ctx context.Context, s *Store, key string, def T) T {
	val, err := Get[T](ctx, s, key)
	if err != nil {
		return def
	}
	return val
}

// ─── Layer 3: Plugin ───────────────────────────────────────────────────────

// Plugin is the storage plugin API exposed to other plugins.
//
// Use [Plugin.NS] to get a namespaced [Store]; never access the [Backend] directly.
type Plugin struct {
	mu      sync.RWMutex
	backend Backend
	codec   Codec
}

// NewPlugin creates a standalone Plugin for the given backend.
// Use this for unit tests or scenarios without the plugin system lifecycle.
// For production, use [New] or [NewWithBackend].
func NewPlugin(b Backend) *Plugin {
	return &Plugin{backend: b, codec: JSONCodec{}}
}

// NS returns a namespaced [Store] for the given namespace.
// All keys written/read through the returned Store are automatically prefixed
// with "<namespace>:", preventing collisions with other plugins.
func (p *Plugin) NS(namespace string) *Store {
	p.mu.RLock()
	b, c := p.backend, p.codec
	p.mu.RUnlock()
	return &Store{backend: b, prefix: namespace + ":", codec: c}
}

// Backend returns the underlying [Backend] for advanced use (e.g. migrations).
func (p *Plugin) Backend() Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.backend
}

// SetBackend hot-swaps the storage backend (thread-safe).
// Existing [Store] instances from previous [Plugin.NS] calls keep the old backend;
// call NS again to get a Store backed by the new backend.
func (p *Plugin) SetBackend(b Backend) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.backend = b
}

// ─── Descriptor constructors ───────────────────────────────────────────────

// New creates a storage plugin descriptor using the default in-memory backend.
func New() *plugin.Descriptor { return NewWithBackend(NewMemoryStorage()) }

// NewWithBackend creates a storage plugin descriptor using the given backend.
func NewWithBackend(b Backend) *plugin.Descriptor {
	pluginAPI := NewPlugin(b)

	return &plugin.Descriptor{
		Name:    "storage",
		Version: "3.0.0",
		Deps:    []string{},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "统一的数据存储抽象层，支持多种后端（内存 / SQLite / Redis）",
			Category:    "核心",
			Tags:        []string{"存储", "数据", "核心"},
			HelpText: `存储插件使用说明：
  store := storagePlugin.NS("my-plugin")           // 获取命名空间 Store
  storage.Set(ctx, store, "key", value, 0)          // 写入（自动序列化）
  v, err := storage.Get[MyType](ctx, store, "key")  // 读取（自动反序列化）`,
		},

		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Infof("Loading storage plugin (backend=%T)", b)

			if cleanable, ok := b.(CleanableBackend); ok {
				ctx.Go(func(runCtx context.Context) {
					ticker := time.NewTicker(time.Minute)
					defer ticker.Stop()
					for {
						select {
						case <-ticker.C:
							n, err := cleanable.CleanExpired(runCtx)
							if err != nil {
								ctx.Log.Error("Background clean failed", err)
							} else if n > 0 {
								ctx.Log.Infof("Cleaned %d expired keys", n)
							}
						case <-runCtx.Done():
							return
						}
					}
				})
			}
			return pluginAPI, nil
		},

		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.Log.Info("Unloading storage plugin")
			if err := b.Close(); err != nil {
				ctx.Log.Errorf("Failed to close storage backend: %v", err)
			}
			return nil
		},
	}
}

// NewV2WithBackend is a deprecated alias for [NewWithBackend].
//
// Deprecated: Use [NewWithBackend] instead.
func NewV2WithBackend(b Backend) *plugin.Descriptor { return NewWithBackend(b) }

// ─── Sentinel errors ───────────────────────────────────────────────────────

var (
	// ErrNotFound is returned when the requested key does not exist.
	ErrNotFound = errors.New("key not found")
	// ErrExpired is returned when the requested key has passed its TTL.
	ErrExpired = errors.New("key expired")
)
