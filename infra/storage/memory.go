package storage

import (
	"errors"
	"fmt"
	"sync"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// NewMemory 创建一个基于 SQLite 内存数据库的存储客户端。
//
// 适用于单元测试或不需要持久化的轻量场景。
// 每次调用返回独立的内存数据库实例，互不干扰。
//
// 注意：内存数据库在进程退出后数据丢失。
// 多个连接共享同一内存数据库时，请使用带命名的 DSN:
//
//	storage.NewMemory() // ":memory:" 每次新建独立实例
//
// 示例（在测试中）：
//
//	client := storage.NewMemory()
//	client.AutoMigrate(&MyModel{})
//	client.Create(&MyModel{...})
func NewMemory() (*Plugin, error) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: open in-memory db failed: %w", err)
	}
	return newPlugin(db), nil
}

// MemoryClient 纯 Go 内存存储，不依赖 CGO/SQLite，专用于轻量测试。
//
// 仅实现 Client 接口中最常用的方法，不支持复杂查询（如 WHERE 条件）。
// 若需要完整的 SQL 支持，请使用 [NewMemory]（基于 SQLite）。
type MemoryClient struct {
	mu     sync.RWMutex
	tables map[string][]any // tableName -> []row（row 为 interface{} 的占位符）
}

// NewMemoryClient 创建一个纯内存 Client（不依赖 CGO，仅供简单测试使用）。
func NewMemoryClient() *MemoryClient {
	return &MemoryClient{tables: make(map[string][]any)}
}

// AutoMigrate 对于 MemoryClient 是空操作（无需迁移）
func (m *MemoryClient) AutoMigrate(_ ...any) error { return nil }

// Create 追加一条记录到内存表（key 以类型名为表名）
func (m *MemoryClient) Create(value any) error {
	if value == nil {
		return errors.New("memory: cannot create nil value")
	}
	key := fmt.Sprintf("%T", value)
	m.mu.Lock()
	m.tables[key] = append(m.tables[key], value)
	m.mu.Unlock()
	return nil
}

// Save 等同于 Create（内存模式不区分 INSERT/UPDATE）
func (m *MemoryClient) Save(value any) error { return m.Create(value) }

// First 不支持，始终返回 ErrNotFound
func (m *MemoryClient) First(_ any, _ ...any) error { return ErrNotFound }

// Find 不支持条件过滤，始终返回 nil（不填充 dest）
func (m *MemoryClient) Find(_ any, _ ...any) error { return nil }

// Delete 不支持，始终返回 nil
func (m *MemoryClient) Delete(_ any, _ ...any) error { return nil }

// Updates 不支持，始终返回 nil
func (m *MemoryClient) Updates(_ any) error { return nil }

// Where 返回自身（MemoryClient 不支持条件过滤）
func (m *MemoryClient) Where(_ any, _ ...any) Client { return m }

// Order 返回自身（MemoryClient 不支持排序）
func (m *MemoryClient) Order(_ any) Client { return m }

// Limit 返回自身（MemoryClient 不支持 LIMIT）
func (m *MemoryClient) Limit(_ int) Client { return m }

// Offset 返回自身（MemoryClient 不支持 OFFSET）
func (m *MemoryClient) Offset(_ int) Client { return m }

// DB 始终返回 nil（MemoryClient 没有 gorm.DB）
func (m *MemoryClient) DB() *gorm.DB { return nil }
