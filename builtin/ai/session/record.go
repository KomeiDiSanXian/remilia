// record.go — 会话的持久化记录（GORM schema）与双向转换。
package session

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// --- GORM 持久化记录 ---

// Record 对应数据库表，用于 GORM 持久化。
type Record struct {
	ID        string `gorm:"primaryKey"`
	UserID    string `gorm:"index"`
	ChatID    string `gorm:"index"`
	Messages  string `gorm:"type:text"`
	CallCount int    `gorm:"default:0"`
	ToolCount int    `gorm:"default:0"`
	// Plan 进行中的任务计划（JSON；跨重启继续执行）。
	Plan string `gorm:"type:text"`
	// PendingImages 未消费图片引用（JSON；跨重启窗口内仍可合并）。
	PendingImages string `gorm:"type:text"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ToRecord 将 Session 转换为数据库记录。
func (s *Session) ToRecord() *Record {
	data, err := json.Marshal(MessagesForPersistence(s.Messages))
	if err != nil {
		logger.Errorf("[AI] Failed to marshal session messages for %s: %v", s.ID, err)
		data = []byte("[]")
	}
	var planJSON string
	if s.plan != nil {
		if pb, perr := json.Marshal(s.plan); perr == nil {
			planJSON = string(pb)
		} else {
			logger.Errorf("[AI] Failed to marshal session plan for %s: %v", s.ID, perr)
		}
	}
	var pendingJSON string
	if s.pendingImage != nil {
		if pb, perr := json.Marshal(s.pendingImage); perr == nil {
			pendingJSON = string(pb)
		} else {
			logger.Errorf("[AI] Failed to marshal pending images for %s: %v", s.ID, perr)
		}
	}
	return &Record{
		ID:            s.ID,
		UserID:        s.UserID,
		ChatID:        s.ChatID,
		Messages:      string(data),
		CallCount:     s.CallCount,
		ToolCount:     s.ToolCount,
		Plan:          planJSON,
		PendingImages: pendingJSON,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
}

// MessagesForPersistence 不保存附件二进制数据。重启后以文本占位保留上下文，
// 避免向 Provider 发送无法复原的空多模态内容。
func MessagesForPersistence(messages []protocol.Message) []protocol.Message {
	result := make([]protocol.Message, len(messages))
	for i, message := range messages {
		result[i] = message
		if len(message.ContentParts) == 0 {
			continue
		}

		parts := make([]string, 0, len(message.ContentParts))
		for _, part := range message.ContentParts {
			switch part.Type {
			case protocol.ContentPartText:
				if part.Text != "" {
					parts = append(parts, part.Text)
				}
			case protocol.ContentPartImage:
				parts = append(parts, "[用户发送了图片，附件内容在重启后不可用]")
			case protocol.ContentPartAudio:
				parts = append(parts, "[用户发送了音频，附件内容在重启后不可用]")
			}
		}
		result[i].Content = strings.Join(parts, "\n")
		result[i].ContentParts = nil
	}
	return result
}

// ToSession 将数据库记录还原为 Session。
func (r *Record) ToSession() *Session {
	var msgs []protocol.Message
	if err := json.Unmarshal([]byte(r.Messages), &msgs); err != nil {
		logger.Warnf("[AI] Failed to unmarshal session messages (corrupted?): %v", err)
	}
	s := &Session{
		ID:        r.ID,
		UserID:    r.UserID,
		ChatID:    r.ChatID,
		Messages:  msgs,
		CallCount: r.CallCount,
		ToolCount: r.ToolCount,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
	if r.Plan != "" {
		var plan Plan
		if err := json.Unmarshal([]byte(r.Plan), &plan); err == nil {
			s.plan = &plan
		} else {
			logger.Warnf("[AI] Failed to unmarshal session plan (corrupted?): %v", err)
		}
	}
	if r.PendingImages != "" {
		var pi PendingImageState
		if err := json.Unmarshal([]byte(r.PendingImages), &pi); err == nil {
			s.pendingImage = &pi
		} else {
			logger.Warnf("[AI] Failed to unmarshal pending images (corrupted?): %v", err)
		}
	}
	return s
}
