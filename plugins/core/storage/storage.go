package storage

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
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

// Plugin 存储插件 API
type Plugin struct {
	storage Storage
	mu      sync.RWMutex
}

// New 创建存储插件（v2 API）
// 默认使用内存存储作为后端
func New() *plugin.PluginDescriptor {
	return NewV2WithBackend(NewMemoryStorage())
}

// NewV2WithBackend 使用指定后端创建存储插件（v2 API）
func NewV2WithBackend(storage Storage) *plugin.PluginDescriptor {
	// 创建 Plugin 包装器
	pluginAPI := &Plugin{
		storage: storage,
	}

	return &plugin.PluginDescriptor{
		Name:        "storage",
		Version:     "2.0.0",
		Author:      "Remilia Team",
		Description: "统一的数据存储抽象层，支持多种后端",
		Category:    "核心",
		Tags:        []string{"存储", "数据", "核心"},
		Deps:        []string{},
		HelpText: `存储插件使用说明：
提供统一的 KV 存储接口，支持多种后端：
- Memory - 内存存储（默认）
- Redis - Redis 集群（待实现）
- SQLite - 本地数据库（待实现）

API 使用 (v2):
  storagePlugin := ctx.MustGet("storage").(*storage.Plugin)
  storagePlugin.Get(key) - 获取值
  storagePlugin.Set(key, value, ttl) - 设置值
  storagePlugin.Delete(key) - 删除值
  storagePlugin.Exists(key) - 检查存在
  storagePlugin.Keys(pattern) - 列出键`,

		Setup: func(ctx *plugin.SetupContext) error {
			logger.Info("[StoragePlugin] Loading storage plugin (v2)...")
			logger.Infof("[StoragePlugin] Backend: %T", storage)

			// 注册 API 包装器到容器
			ctx.Manager.GetContainer().Register("storage_api", pluginAPI)

			logger.Info("[StoragePlugin] Storage plugin loaded successfully")
			return nil
		},

		Teardown: func() error {
			logger.Info("[StoragePlugin] Unloading storage plugin...")
			if err := storage.Clear(); err != nil {
				logger.WithError(err).Warn("[StoragePlugin] Failed to clear storage")
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
