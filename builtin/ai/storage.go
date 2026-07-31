// Package ai storage.go — 基于 GORM 的会话持久化实现。
//
// 本文件提供两种 SessionStore 实现：
//   - gormSessionStore: 基于 storage 插件的 GORM 持久化存储，
//     自动建表，支持 CRUD 操作
//   - noopSessionStore: 空实现（什么也不做），当 storage 插件不可用时作为默认值
package ai

import (
	"errors"
	"fmt"

	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
	"gorm.io/gorm/clause"
)

// gormSessionStore 基于 GORM 的会话持久化存储。
// 使用 storage 插件提供的 Client 接口进行数据库操作。
type gormSessionStore struct {
	client *infrastorage.Plugin
}

// NewGormSessionStore 创建一个基于 GORM 的会话存储。
// 初始化时会自动创建 session_records 表。
func NewGormSessionStore(client *infrastorage.Plugin) (SessionStore, error) {
	if err := client.AutoMigrate(&sessionRecord{}); err != nil {
		return nil, fmt.Errorf("ai: migrate session table: %w", err)
	}
	return &gormSessionStore{client: client}, nil
}

// Load 从数据库加载会话。
// 会话不存在时返回 (nil, nil) 而非错误。
func (s *gormSessionStore) Load(sessionID string) (*Session, error) {
	var rec sessionRecord
	if err := s.client.Where("id = ?", sessionID).First(&rec); err != nil {
		if errors.Is(err, infrastorage.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return rec.toSession(), nil
}

// Save 保存会话到数据库（单语句 upsert，避免 SELECT+INSERT/UPDATE 两次往返）。
// 主键存在时更新全部字段，不存在时插入。
func (s *gormSessionStore) Save(session *Session) error {
	rec := session.toRecord()
	return s.client.DB().
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			UpdateAll: true,
		}).
		Create(rec).Error
}

// Delete 从数据库删除会话。
func (s *gormSessionStore) Delete(sessionID string) error {
	return s.client.Where("id = ?", sessionID).Delete(&sessionRecord{})
}

// noopSessionStore 空实现，不进行任何持久化。
// 当 storage 插件不可用时使用。
type noopSessionStore struct{}

func (n *noopSessionStore) Load(_ string) (*Session, error) { return nil, nil }
func (n *noopSessionStore) Save(_ *Session) error           { return nil }
func (n *noopSessionStore) Delete(_ string) error           { return nil }
