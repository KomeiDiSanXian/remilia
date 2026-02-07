package storage

import (
	"path/filepath"
	"sync"
	"time"
)

// MemoryStorage 内存存储实现
type MemoryStorage struct {
	data map[string]*memoryItem
	mu   sync.RWMutex
}

type memoryItem struct {
	value     []byte
	expiresAt time.Time
}

// NewMemoryStorage 创建内存存储
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		data: make(map[string]*memoryItem),
	}
}

// Get 获取值
func (m *MemoryStorage) Get(key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	item, exists := m.data[key]
	if !exists {
		return nil, ErrNotFound
	}

	// 检查是否过期
	if !item.expiresAt.IsZero() && time.Now().After(item.expiresAt) {
		return nil, ErrExpired
	}

	// 返回副本，避免外部修改
	result := make([]byte, len(item.value))
	copy(result, item.value)
	return result, nil
}

// Set 设置值
func (m *MemoryStorage) Set(key string, value []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 复制值，避免外部修改
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	item := &memoryItem{
		value: valueCopy,
	}

	if ttl > 0 {
		item.expiresAt = time.Now().Add(ttl)
	}

	m.data[key] = item
	return nil
}

// Delete 删除值
func (m *MemoryStorage) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.data, key)
	return nil
}

// Exists 检查键是否存在
func (m *MemoryStorage) Exists(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	item, exists := m.data[key]
	if !exists {
		return false
	}

	// 检查是否过期
	if !item.expiresAt.IsZero() && time.Now().After(item.expiresAt) {
		return false
	}

	return true
}

// Keys 列出匹配的键
func (m *MemoryStorage) Keys(pattern string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var keys []string
	for key := range m.data {
		// 简单的通配符匹配
		matched, err := filepath.Match(pattern, key)
		if err != nil {
			return nil, err
		}
		if matched {
			// 检查是否过期
			item := m.data[key]
			if item.expiresAt.IsZero() || time.Now().Before(item.expiresAt) {
				keys = append(keys, key)
			}
		}
	}

	return keys, nil
}

// Clear 清空所有数据
func (m *MemoryStorage) Clear() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data = make(map[string]*memoryItem)
	return nil
}

// CleanExpired 清理过期数据
func (m *MemoryStorage) CleanExpired() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	now := time.Now()

	for key, item := range m.data {
		if !item.expiresAt.IsZero() && now.After(item.expiresAt) {
			delete(m.data, key)
			count++
		}
	}

	return count
}

// Size 返回存储的键数量
func (m *MemoryStorage) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data)
}
