// record.go — 会话的持久化记录（GORM schema）与双向转换。

package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// --- GORM 持久化记录 ---

// SessionRecordTable 会话持久化表名。
//
// 必须显式声明：GORM 对名为 Record 的结构体默认映射到 records 表，而
// records 在共享库中过于通用（同库其他插件的 Record 结构体会撞名）。
// 历史版本确实落在 records 表，升级时由 [NewGormSessionStore] 自动改名。
const SessionRecordTable = "ai_session_records"

// Record 对应数据库表，用于 GORM 持久化。
type Record struct {
	// ID 会话标识 "{platform}:{chatID}:{userID}"，长度远小于 191；
	// 声明 size 是为了给非 SQLite 驱动一个确定的定长索引列
	// （MySQL/PG 对无长度字符串主键的处理各不相同）。
	ID string `gorm:"primaryKey;size:191"`
	// UserID / ChatID 不建索引：没有任何查询按它们过滤（会话一律按 ID 取），
	// 每次整记录 upsert 却要多维护两棵 B-tree。历史库里的旧索引由
	// NewGormSessionStore 负责删除。
	UserID string
	ChatID string
	// Messages 用 mediumtext：MySQL TEXT 上限 64KB，中文 UTF-8 下一条工具
	// 结果即可达 ~24KB（MaxToolResultLen 按字符截断），20 条历史轻松超限，
	// 超限后该会话每次落库都会失败并回滚到旧快照。mediumtext 16MB 兜底。
	//
	// SQLite 只按类型名做 affinity，mediumtext 落到 TEXT（无上限）；
	// 换成 PostgreSQL 时该类型名不合法，需同步改为 text（PG 的 text 无实际
	// 长度上限），这是换驱动时唯一需要人工调整的列类型。
	Messages  string `gorm:"type:mediumtext"`
	CallCount int    `gorm:"default:0"`
	ToolCount int    `gorm:"default:0"`
	// Plan 当前进行中的任务计划（JSON 序列化；会话间互相执行）。
	Plan string `gorm:"type:text"`
	// PendingImages 未消费图片引用（JSON 序列化；跨重启窗口内仍可合并）
	PendingImages string `gorm:"type:text"`
	CreatedAt     time.Time
	// UpdatedAt 会话最后活跃时间，同时是库侧过期清理（PurgeExpired）的过滤列，
	// 必须建索引：没有它每次清理都是全表扫。
	UpdatedAt time.Time `gorm:"index"`
}

// TableName 固定表名，避免 GORM 复数化成通用的 records。
func (Record) TableName() string { return SessionRecordTable }

// ToRecord 将 Session 转换为数据库记录。
//
// 与早期版本不同，序列化失败不再退化成空历史：调用方（落库路径）宁可
// 跳过本次写库，也不能用 "[]" 覆盖掉库里已有的对话——历史不可重建，
// 一次失败的 marshal 不该变成一次静默的清空。
func (s *Session) ToRecord() (*Record, error) {
	data, err := json.Marshal(MessagesForPersistence(s.Messages))
	if err != nil {
		return nil, fmt.Errorf("session: marshal messages for %s: %w", s.ID, err)
	}
	var planJSON string
	if s.plan != nil {
		pb, perr := json.Marshal(s.plan)
		if perr != nil {
			return nil, fmt.Errorf("session: marshal plan for %s: %w", s.ID, perr)
		}
		planJSON = string(pb)
	}
	var pendingJSON string
	if s.pendingImage != nil {
		pb, perr := json.Marshal(s.pendingImage)
		if perr != nil {
			return nil, fmt.Errorf("session: marshal pending images for %s: %w", s.ID, perr)
		}
		pendingJSON = string(pb)
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
	}, nil
}

// 多模态附件的文本占位。两条路径语义不同，文案因此不同：
//   - 落库：附件在重启后无法复原；
//   - 请求/内存降级：附件只是本次不再随请求发送（字节仍可重新下载）。
const (
	persistImagePlaceholder = "[用户发送了图片，附件内容在重启后不可用]"
	persistAudioPlaceholder = "[用户发送了音频，附件内容在重启后不可用]"

	stripImagePlaceholder = "[历史图片未随本次请求发送]"
	stripAudioPlaceholder = "[历史音频未随本次请求发送]"
)

// stripBinaryParts 把多模态片段里的二进制替换为文本占位，返回新消息（不修改入参）。
func stripBinaryParts(message protocol.Message, imagePlaceholder, audioPlaceholder string) protocol.Message {
	if len(message.ContentParts) == 0 {
		return message
	}
	parts := make([]string, 0, len(message.ContentParts))
	for _, part := range message.ContentParts {
		switch part.Type {
		case protocol.ContentPartText:
			if part.Text != "" {
				parts = append(parts, part.Text)
			}
		case protocol.ContentPartImage:
			parts = append(parts, imagePlaceholder)
		case protocol.ContentPartAudio:
			parts = append(parts, audioPlaceholder)
		}
	}
	message.Content = strings.Join(parts, "\n")
	message.ContentParts = nil
	return message
}

// StripBinaryParts 把消息的多模态二进制替换为"本次请求不再发送"的文本占位。
//
// 请求构建与内存回收共用这一实现，保证两条路径产出的消息逐字节一致：
// 若内存里已经降级的消息与请求时临时降级的结果不同，前缀缓存会无谓失效。
func StripBinaryParts(message protocol.Message) protocol.Message {
	return stripBinaryParts(message, stripImagePlaceholder, stripAudioPlaceholder)
}

// StripBinaryPartsOlderThan 把 Timestamp 早于 cutoff 的消息的附件二进制就地
// 替换为文本占位，返回释放的附件片段数。
//
// 回合结束后调用：图片只在 image_context_window 内还会随请求发送（判定见
// runtime.PrepareRequestMessages），更早的图片留在会话里只是白占内存。
// 时间戳为零（未知）的消息保持原样——宁可不释放，也不误伤当前轮的图片。
func (s *Session) StripBinaryPartsOlderThan(cutoff time.Time) int {
	s.Lock()
	defer s.Unlock()
	released := 0
	for i := range s.Messages {
		m := s.Messages[i]
		if len(m.ContentParts) == 0 || m.Timestamp.IsZero() || !m.Timestamp.Before(cutoff) {
			continue
		}
		released += len(m.ContentParts)
		s.Messages[i] = StripBinaryParts(m)
	}
	return released
}

// MessagesForPersistence 生成落库用的消息副本：**不保存附件二进制，但保留引用**。
//
// 附件片段只保留类型与引用（AttachmentID / SourceURL），重启后由请求构建阶段
// 按引用重新取回（见 hydrateAttachmentParts）；完全没有引用的片段退化为文本
// 占位。`Content` 同时写入扁平化文本，作为"引用也取不回"或旧版本读回的降级内容。
func MessagesForPersistence(messages []protocol.Message) []protocol.Message {
	result := make([]protocol.Message, len(messages))
	for i, message := range messages {
		if len(message.ContentParts) == 0 {
			result[i] = message
			continue
		}

		parts := make([]protocol.ContentPart, 0, len(message.ContentParts))
		texts := make([]string, 0, len(message.ContentParts))
		for _, part := range message.ContentParts {
			switch part.Type {
			case protocol.ContentPartText:
				parts = append(parts, part)
				if part.Text != "" {
					texts = append(texts, part.Text)
				}
			case protocol.ContentPartImage, protocol.ContentPartAudio:
				if part.SourceURL == "" && part.AttachmentID == 0 {
					// 无任何引用：无法恢复，只能保留占位文本。
					texts = append(texts, persistPlaceholder(part.Type))
					continue
				}
				ref := part
				ref.Data = nil
				parts = append(parts, ref)
				texts = append(texts, persistPlaceholder(part.Type))
			}
		}

		message.ContentParts = parts
		message.Content = strings.Join(texts, "\n")
		result[i] = message
	}
	return result
}

// persistPlaceholder 返回落库降级文本（引用取不回时的可见内容）。
func persistPlaceholder(kind protocol.ContentPartType) string {
	if kind == protocol.ContentPartAudio {
		return persistAudioPlaceholder
	}
	return persistImagePlaceholder
}

// ToSession 将数据库记录还原为 Session。
//
// 消息历史损坏时返回错误而不是"空历史"：空历史会在下一次落库时把库里
// 那行覆盖成真正的空，等于把一次可修复的读失败变成永久丢数据。
// 调用方（Load）把错误上抛，会话进入"加载失败"降级态（见 Session.loadFailed）。
// 计划/待合并图片损坏只影响附加状态，记日志后忽略即可。
func (r *Record) ToSession() (*Session, error) {
	var msgs []protocol.Message
	// 空串等价于"没有消息"：历史版本或外部工具写入的裸行可能是空字符串，
	// 不该被当成损坏。
	if trimmed := strings.TrimSpace(r.Messages); trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &msgs); err != nil {
			return nil, fmt.Errorf("session: unmarshal messages for %s: %w", r.ID, err)
		}
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
	return s, nil
}
