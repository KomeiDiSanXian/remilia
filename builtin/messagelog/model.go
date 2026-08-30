package messagelog

import (
	"context"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// DefaultCapacity 每个群/用户的内存环形缓冲区默认大小。
const DefaultCapacity = 1000

// SendStatus 出站消息发送状态。
type SendStatus string

const (
	// SendStatusPending 已入队，允许恢复的中间状态（非永久状态）。
	SendStatusPending SendStatus = "pending"
	// SendStatusSent 平台确认发送成功。
	SendStatusSent SendStatus = "sent"
	// SendStatusFailed 平台返回失败。
	SendStatusFailed SendStatus = "failed"
	// SendStatusUnknown 本地无法确定平台最终结果（如发送超时，平台可能已收到）。
	SendStatusUnknown SendStatus = "unknown"
)

// RecordEntry 一条消息的完整记录（内存缓存 + DB 查询的统一结构）。
//
// 入站/出站共用同一套字段：差异通过 IsOutbound + SendStatus 表达，
// 出站不再有「降级子集」。
type RecordEntry struct {
	Timestamp  time.Time // 事件发生时间
	CreatedAt  time.Time // 记录入库时间
	EditedAt   time.Time
	RecalledAt time.Time
	RequestID  string // RequestID 中间件分配的追踪 ID
	Platform   string // 平台标识符（"qq", "discord", "telegram"）
	Kind       string // 事件类别（"GROUP_MESSAGE", "PRIVATE_MESSAGE", "OUTBOUND"）
	// EventID 消息逻辑身份（messagelog 生成的 UUIDv7，幂等去重）。
	EventID string
	// PlatformMessageID 平台消息 ID（回复/撤回/编辑/去重锚点）。
	PlatformMessageID string
	ChatID            string // 会话 ID（群 ID / 私聊对方用户 ID）
	ChatName          string
	ParentID          string // 父容器 ID（频道场景的 guild_id / server_id）
	UserID            string // 发送者 ID（出站 = bot 身份；可配合 TriggeredBy）
	UserName          string
	UserRole          string
	Content           string // 消息文本内容
	// ReplyToID 遗留字段：平台回复目标消息 ID，仅兼容旧数据；
	// 新写入同时填充 ReplyToMessageID / ReplyToEventID。
	ReplyToID string
	// ReplyToEventID 回复目标（本系统 event_id）。
	ReplyToEventID string
	// ReplyToMessageID 回复目标（平台 message_id）。
	ReplyToMessageID string
	RawType          string // 平台原始事件类型字符串
	// 出站发送状态（入站消息为零值）。
	SendStatus SendStatus
	LastError  string // 出站失败 / unknown 时的错误摘要
	// TriggeredBy 出站消息的触发用户 ID（可选，用于关联「谁触发了这条回复」）。
	TriggeredBy string
	Mentions    []platform.UserInfo // @ 用户列表（平台提供时有效）
	// Attachments 附件元数据（不含二进制；二进制经 Fetch 获取）。
	// 热缓存中为记录时快照，最终状态以 message_attachments 表为准。
	Attachments []AttachmentMeta
	IsGroup     bool // 是否为群组/频道消息
	// IsOutbound 是否为机器人发出的出站消息。
	IsOutbound bool
	IsEdited   bool // 编辑墓碑标记
	IsRecalled bool // 撤回墓碑标记
}

// AttachmentMeta 查询/缓存中附件的元数据视图（与 message_attachments 行对应）。
type AttachmentMeta struct {
	ID         int64  // message_attachments 行 ID（Fetch 用）
	Type       string // image / audio / video / file
	Name       string
	MimeType   string
	Size       int64
	URL        string
	Status     string // pending/pending_retry/pending_lazy/ready/expired/failed/deleted
	StorageKey string // 内容哈希（Status=ready 时有效）
}

// EventPublisher 插件间事件发布接口（由 plugin.Manager 的 EventBus 注入）。
// 统计插件等派生数据消费者订阅 [TopicMessageRecorded]。
type EventPublisher interface {
	PublishContext(ctx context.Context, topic string, data any) error
}

// TopicMessageRecorded 消息事实落库后广播的主题。
const TopicMessageRecorded = "messagelog.message_recorded"

// MessageRecorded 一条消息事实落库事件（flush 提交成功后广播）。
//
// 派生数据消费者（统计/词云等）以此为准做增量聚合；事件丢失不影响事实层，
// 可通过 [Logger.ScanAfter] 全量重建。
type MessageRecorded struct {
	Record RecordEntry
}

// MessageRecord 对应 SQLite message_records 表的 GORM 模型。
//
// 新增列均为可空/默认值，保证旧库迁移安全。event_id 的唯一索引由迁移创建
// （不能依赖 AutoMigrate：对存量重复值建唯一索引会失败）。
type MessageRecord struct {
	RequestID string `gorm:"index;not null"`
	Platform  string `gorm:"index;not null"`
	Kind      string `gorm:"index;not null"`
	// EventID 消息逻辑身份（UUIDv7，唯一索引见 migrate.go）。
	EventID string `gorm:"not null"`
	// PlatformMessageID 平台消息 ID（迁移时由旧 event_id 回填）。
	PlatformMessageID string `gorm:"index"`
	ChatID            string `gorm:"index:idx_chat_time;not null"`
	ChatName          string
	ParentID          string
	UserID            string `gorm:"index;not null"`
	UserName          string
	UserRole          string
	Content           string `gorm:"type:text"`
	ReplyToID         string
	// ReplyToEventID / ReplyToMessageID 回复链：新行直接写入；
	// 旧行仅有 reply_to_id，读取时回退（见 modelToEntry）。
	ReplyToEventID   string
	ReplyToMessageID string
	RawType          string
	SendStatus       string `gorm:"index"`
	LastError        string
	TriggeredBy      string
	ID               int64 `gorm:"primaryKey;autoIncrement"`
	Timestamp        int64 `gorm:"index:idx_chat_time"`
	CreatedAt        int64
	EditedAt         int64
	RecalledAt       int64
	IsGroup          bool
	// IsOutbound 是否为机器人发出的出站消息（AI 回复等）。
	IsOutbound bool `gorm:"index"`
	IsEdited   bool
	IsRecalled bool
}

// MessageMention 对应 SQLite message_mentions 表的 GORM 模型。
// EventID 关联到 MessageRecord.EventID，支持通过平台事件 ID 追溯 @ 信息。
type MessageMention struct {
	ID          int64  `gorm:"primaryKey;autoIncrement"`
	EventID     string `gorm:"index;not null"`
	MentionID   string `gorm:"index;not null"`
	DisplayName string
	IsBot       bool
	IsSelf      bool
}

// AttachmentRecord 对应 SQLite message_attachments 表的 GORM 模型。
//
// 二进制存放于 AttachmentStore（文件存储），本表只存引用与元数据。
// Status 状态机：pending → pending_retry → pending_lazy → ready / expired / failed / deleted。
type AttachmentRecord struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	MessageID int64  `gorm:"index"` // 关联 message_records.id
	EventID   string `gorm:"index"` // 关联 message_records.event_id

	Type string // image / audio / video / file
	URL  string // 来源 URL（可能过期，仅作兜底线索）
	Name string
	// MimeType MIME 类型（入站平台提供时填充）。
	MimeType string
	Size     int64
	// Width / Height 图片/视频尺寸（平台提供时填充）。
	Width  int
	Height int

	SHA256     string `gorm:"index"` // 内容去重
	StorageKey string `gorm:"index"` // AttachmentStore 引用

	Status       string `gorm:"index"` // pending/pending_retry/pending_lazy/ready/expired/failed/deleted
	RetryCount   int
	LastError    string
	URLExpiresAt int64 // 可解析 URL 签名时记录（仅用于回填排序）
	FetchedAt    int64
	CreatedAt    int64
}

// TableName 固定为 message_attachments（避免 GORM 默认复数化出 attachment_records）。
func (AttachmentRecord) TableName() string { return "message_attachments" }
