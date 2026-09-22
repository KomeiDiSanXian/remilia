// storage.go — 会话持久化的两个实现：GORM 存储与空实现。
package session

import (
	"errors"
	"fmt"

	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
	"gorm.io/gorm/clause"
)

// GormStore 基于 GORM 的会话持久化存储。
// 使用 storage 插件提供的 Client 接口进行数据库操作。
type GormStore struct {
	client *infrastorage.Plugin
}

// NewGormSessionStore 创建一个基于 GORM 的会话存储。
// 初始化时会自动创建 session_records 表。
func NewGormSessionStore(client *infrastorage.Plugin) (SessionStore, error) {
	if err := client.AutoMigrate(&Record{}); err != nil {
		return nil, fmt.Errorf("ai: migrate session table: %w", err)
	}
	return &GormStore{client: client}, nil
}

// Load 从数据库加载会话。
// 会话不存在时返回 (nil, nil) 而非错误。
func (s *GormStore) Load(sessionID string) (*Session, error) {
	var rec Record
	if err := s.client.Where("id = ?", sessionID).First(&rec); err != nil {
		if errors.Is(err, infrastorage.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return rec.ToSession(), nil
}

// Save 保存会话到数据库（单语句 upsert，避免 SELECT+INSERT/UPDATE 两次往返）。
// 主键存在时更新全部字段，不存在时插入。
func (s *GormStore) Save(session *Session) error {
	rec := session.ToRecord()
	return s.client.DB().
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			UpdateAll: true,
		}).
		Create(rec).Error
}

// Delete 从数据库删除会话。
func (s *GormStore) Delete(sessionID string) error {
	return s.client.Where("id = ?", sessionID).Delete(&Record{})
}

// NoopStore 空实现，不进行任何持久化。
// 当 storage 插件不可用时使用。
type NoopStore struct{}

func (n *NoopStore) Load(_ string) (*Session, error) { return nil, nil }
func (n *NoopStore) Save(_ *Session) error           { return nil }
func (n *NoopStore) Delete(_ string) error           { return nil }
