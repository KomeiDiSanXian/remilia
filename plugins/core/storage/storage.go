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
	storage   Storage
	mu        sync.RWMutex
	stopClean chan struct{} // 停止后台清理协程的信号
}

// startCleanRoutine 启动后台定期清理协程（仅当 storage 实现了 CleanableStorage 时）
func (p *Plugin) startCleanRoutine() chan struct{} {
	cleanable, ok := p.storage.(CleanableStorage)
	if !ok {
		return nil
	}
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				n, err := cleanable.CleanExpired()
				if err != nil {
					logger.WithError(err).Warn("[StoragePlugin] Background clean failed")
				} else if n > 0 {
					logger.Infof("[StoragePlugin] Cleaned %d expired keys", n)
				}
			case <-stop:
				return
			}
		}
	}()
	return stop
}

// New 创建存储插件（v2 API）
// 默认使用内存存储作为后端
func New() *plugin.PluginDescriptor {
	return NewV2WithBackend(NewMemoryStorage())
}

// NewWithBackend 使用指定后端创建存储插件（NewV2WithBackend 的别名）
func NewWithBackend(storage Storage) *plugin.PluginDescriptor {
	return NewV2WithBackend(storage)
}

// NewV2WithBackend 使用指定后端创建存储插件（v2 API）
func NewV2WithBackend(storage Storage) *plugin.PluginDescriptor {
	// 创建 Plugin 包装器
	pluginAPI := &Plugin{
		storage: storage,
	}

	return &plugin.PluginDescriptor{
		Name:    "storage",
		Version: "2.0.0",
		Deps:    []string{},
		Meta: &plugin.PluginMeta{
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
			pluginAPI.stopClean = pluginAPI.startCleanRoutine()
			return pluginAPI, nil
		},

		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.Log.Info("Unloading storage plugin")
			if pluginAPI.stopClean != nil {
				close(pluginAPI.stopClean)
				pluginAPI.stopClean = nil
			}
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
