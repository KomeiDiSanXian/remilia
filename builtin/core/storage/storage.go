package storage

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Storage 统一存储接口
type Storage interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte, ttl time.Duration) error
	Delete(key string) error
	Exists(key string) bool
	Keys(pattern string) ([]string, error)
	Clear() error
}

// Client 插件可选持久化最小接口
//
// 各需要可选持久化的插件（acl、antispam、auditlog 等）应将内部的
// storageBackend 字段类型改为此接口，统一使用同一约束而非各自重复定义。
//
// *Plugin 实现了此接口，可直接赋值：
//
//	var c storage.Client = storagePluginInstance
type Client interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte, ttl time.Duration) error
	Delete(key string) error
}

// CleanableStorage 支持主动清理过期键的存储接口（可选实现）
type CleanableStorage interface {
	Storage
	// CleanExpired 清理所有已过期的键，返回清理的键数量和错误
	CleanExpired() (int, error)
}

// Plugin 存储插件 API
type Plugin struct {
	storage Storage
	mu      sync.RWMutex
}

// New 创建存储插件（v2 API）
// 默认使用内存存储作为后端
func New() *plugin.Descriptor {
	return NewV2WithBackend(NewMemoryStorage())
}

// NewWithBackend 使用指定后端创建存储插件（NewV2WithBackend 的别名）
func NewWithBackend(storage Storage) *plugin.Descriptor {
	return NewV2WithBackend(storage)
}

// NewPlugin 创建独立的 Plugin 实例，直接包装存储后端。
//
// 适用于不需要插件系统生命周期管理的场景（如独立 demo、单元测试）。
// 若需要生命周期管理，请使用 [NewWithBackend]，它返回 *plugin.Descriptor。
func NewPlugin(s Storage) *Plugin {
	return &Plugin{storage: s}
}

// NewV2WithBackend 使用指定后端创建存储插件（v2 API）
func NewV2WithBackend(storage Storage) *plugin.Descriptor {
	// 创建 Plugin 包装器
	pluginAPI := &Plugin{
		storage: storage,
	}

	return &plugin.Descriptor{
		Name:    "storage",
		Version: "2.0.0",
		Deps:    []string{},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "统一的数据存储抽象层，支持多种后端",
			Category:    "核心",
			Tags:        []string{"存储", "数据", "核心"},
			HelpText: `存储插件使用说明：
  storagePlugin := plugin.Require[storage.Plugin](ctx, "storage")
  storagePlugin.Get(key)
  storagePlugin.Set(key, value, ttl)
  storagePlugin.Delete(key)`,
		},

		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Infof("Loading storage plugin (backend=%T)", storage)

			// 后台清理：由框架管理生命周期（ctx.Go 会在 Teardown 前自动 cancel 并等待退出）
			if cleanable, ok := storage.(CleanableStorage); ok {
				ctx.Go(func(runCtx context.Context) {
					ticker := time.NewTicker(time.Minute)
					defer ticker.Stop()
					for {
						select {
						case <-ticker.C:
							n, err := cleanable.CleanExpired()
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

			// 以接口类型额外导出，供依赖接口而非具体类型的消费者使用（面向依赖倒置原则）：
			//   plugin.MustAs[storage.Client](ctx, "storage.Client")
			//   plugin.MustAs[storage.Storage](ctx, "storage.Storage")
			plugin.ExportInterface[Client](ctx, "storage.Client", pluginAPI)
			plugin.ExportInterface[Storage](ctx, "storage.Storage", pluginAPI)

			// 主 key "storage" 导出 *Plugin 具体类型（return 自动注册）
			return pluginAPI, nil
		},

		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.Log.Info("Unloading storage plugin")
			// 无需手动停止 goroutine，框架已在此之前 cancel 并等待
			if err := storage.Clear(); err != nil {
				ctx.Log.Error("Failed to clear storage", err)
			}
			return nil
		},
	}
}

// GetStorage 获取存储实例
func (p *Plugin) GetStorage() Storage {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.storage
}

// SetStorage 设置存储后端
func (p *Plugin) SetStorage(storage Storage) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.storage = storage
}

// Get 获取值
func (p *Plugin) Get(key string) ([]byte, error) {
	return p.storage.Get(key)
}

// GetJSON 获取 JSON 值
func (p *Plugin) GetJSON(key string, v any) error {
	data, err := p.storage.Get(key)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// Set 设置值
func (p *Plugin) Set(key string, value []byte, ttl time.Duration) error {
	return p.storage.Set(key, value, ttl)
}

// SetJSON 设置 JSON 值
func (p *Plugin) SetJSON(key string, v any, ttl time.Duration) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return p.storage.Set(key, data, ttl)
}

// Delete 删除值
func (p *Plugin) Delete(key string) error {
	return p.storage.Delete(key)
}

// Exists 检查键是否存在
func (p *Plugin) Exists(key string) bool {
	return p.storage.Exists(key)
}

// Keys 列出匹配的键
func (p *Plugin) Keys(pattern string) ([]string, error) {
	return p.storage.Keys(pattern)
}

// Clear 清空所有数据
func (p *Plugin) Clear() error {
	return p.storage.Clear()
}

var (
	// ErrNotFound 键不存在
	ErrNotFound = errors.New("key not found")
	// ErrExpired 键已过期
	ErrExpired = errors.New("key expired")
)
