package messagelog

import (
	"fmt"
	"uuid"

	"gorm.io/gorm"
)

// currentSchemaVersion 是当前数据库 schema 版本（PRAGMA user_version）。
//
// 版本历史：
//   - 0：遗留 v1.46.0 结构（无 user_version 记录）——旧 message_records /
//     message_mentions，event_id 为平台消息 ID，无附件表
//   - 1：开发期中间标记（从未发布，结构与 0 相同，仅 user_version 不同）
//   - 2：当前唯一版本。从遗留结构一步迁移：新列 + UUIDv7 event_id 升级 +
//     附件表 + 幂等唯一索引（开发期的多步中间版本未发布，合并为一步）
//
// 说明：列结构由 gorm AutoMigrate 负责（幂等加列），本迁移负责版本追踪、数据回填
// 与 raw 索引（gorm tag 不便表达或不安全的约束）。
const currentSchemaVersion = 2

// schemaVersion 读取当前 PRAGMA user_version。
func schemaVersion(db *gorm.DB) (int, error) {
	var v int
	if err := db.Raw("PRAGMA user_version").Row().Scan(&v); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return v, nil
}

// setSchemaVersion 写入 PRAGMA user_version（在事务内调用，随事务原子提交）。
func setSchemaVersion(tx *gorm.DB, v int) error {
	if err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", v)).Error; err != nil {
		return fmt.Errorf("set schema version %d: %w", v, err)
	}
	return nil
}

// migrate 在事务内将数据库从当前 user_version 迁移到 currentSchemaVersion。
// 所有 < currentSchemaVersion 的库（遗留 v1.46.0 或全新库）统一执行一次幂等升级，
// 由 OpenDB 在 AutoMigrate 之后调用。
func migrate(db *gorm.DB) error {
	current, err := schemaVersion(db)
	if err != nil {
		return err
	}
	if current >= currentSchemaVersion {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := migrateFromLegacy(tx); err != nil {
			return err
		}
		return setSchemaVersion(tx, currentSchemaVersion)
	})
}

// migrateFromLegacy 把遗留结构一步迁移到当前 schema（全新库上为幂等空转）：
//
//  1. message_records 补齐新列（platform_message_id / reply_to_event_id /
//     send_status / is_edited 等），回填 platform_message_id = 旧 event_id
//  2. event_id 从「平台消息 ID」升级为 messagelog 自生成 UUIDv7，平台消息 ID
//     保留在 platform_message_id；为存量行逐行生成新 event_id，并同步改写
//     message_mentions / message_attachments 引用与 reply_to_event_id 回复链；
//     空 event_id 行同样分配 UUID（避免唯一索引因重复空值失败）
//  3. 附件表 message_attachments 建表并补列，启用幂等唯一索引
//     （event_id / (event_id,mention_id) / (event_id,url,name)）与查询索引
func migrateFromLegacy(tx *gorm.DB) error {
	m := tx.Migrator()

	// message_records 新增列（AutoMigrate 通常已加列，这里兜底保证存在）
	for _, col := range []string{
		"platform_message_id", "reply_to_event_id", "reply_to_message_id",
		"send_status", "last_error", "triggered_by",
		"is_edited", "edited_at", "is_recalled", "recalled_at",
	} {
		if m.HasColumn(&MessageRecord{}, col) {
			continue
		}
		if err := m.AddColumn(&MessageRecord{}, col); err != nil {
			return fmt.Errorf("add column %s: %w", col, err)
		}
	}

	// 回填 platform_message_id：旧库的 event_id 即平台消息 ID，复制保留语义
	if err := tx.Exec(
		"UPDATE message_records SET platform_message_id = event_id WHERE platform_message_id IS NULL OR platform_message_id = ''",
	).Error; err != nil {
		return fmt.Errorf("backfill platform_message_id: %w", err)
	}

	// 附件表
	if err := tx.AutoMigrate(&AttachmentRecord{}); err != nil {
		return fmt.Errorf("migrate attachment table: %w", err)
	}

	// 查询索引（gorm tag 不便表达：复合/降序；request_id 已有 gorm tag 单列索引，
	// 不重复建，避免热写路径索引放大）
	for _, idx := range []string{
		// 最近 N 查询：WHERE chat_id = ? ORDER BY id DESC LIMIT n
		"CREATE INDEX IF NOT EXISTS idx_mr_chat_id_id ON message_records(chat_id, id DESC)",
		// 单条/回复链：WHERE chat_id = ? AND event_id = ?
		"CREATE INDEX IF NOT EXISTS idx_mr_chat_id_event ON message_records(chat_id, event_id)",
	} {
		if err := tx.Exec(idx).Error; err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}

	// 清理早期迭代误建的 attachment_records（AttachmentRecord 曾未显式声明
	// 表名，GORM 默认复数化）。当前固定为 message_attachments。
	if err := tx.Exec("DROP TABLE IF EXISTS attachment_records").Error; err != nil {
		return fmt.Errorf("drop legacy attachment table: %w", err)
	}

	// 临时映射表承载全部 old → new，跨批次解析回复链引用。
	// INSERT OR IGNORE：存量 event_id 可能跨平台重复（历史语义），首个映射生效。
	if err := tx.Exec(
		"CREATE TEMP TABLE IF NOT EXISTS ml_event_id_map(old_id TEXT PRIMARY KEY, new_id TEXT NOT NULL)",
	).Error; err != nil {
		return fmt.Errorf("create temp map: %w", err)
	}
	defer tx.Exec("DROP TABLE IF EXISTS ml_event_id_map")

	const batchSize = 500
	var lastID int64
	for {
		var rows []MessageRecord
		if err := tx.Where("id > ?", lastID).Order("id ASC").Limit(batchSize).Find(&rows).Error; err != nil {
			return fmt.Errorf("load records: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			newID := uuid.NewV7().String()
			if r.EventID != "" {
				if err := tx.Exec(
					"INSERT OR IGNORE INTO ml_event_id_map(old_id, new_id) VALUES (?, ?)", r.EventID, newID,
				).Error; err != nil {
					return fmt.Errorf("fill temp map: %w", err)
				}
			}
			if err := tx.Exec(
				"UPDATE message_records SET event_id = ? WHERE id = ?", newID, r.ID,
			).Error; err != nil {
				return fmt.Errorf("rewrite event_id: %w", err)
			}
		}
		lastID = rows[len(rows)-1].ID
	}

	// 关联表与回复链引用按映射改写（old_id 在映射中才改写）
	for _, stmt := range []string{
		`UPDATE message_mentions
		 SET event_id = (SELECT m.new_id FROM ml_event_id_map m WHERE m.old_id = message_mentions.event_id)
		 WHERE event_id IN (SELECT old_id FROM ml_event_id_map)`,
		`UPDATE message_attachments
		 SET event_id = (SELECT m.new_id FROM ml_event_id_map m WHERE m.old_id = message_attachments.event_id)
		 WHERE event_id IN (SELECT old_id FROM ml_event_id_map)`,
		`UPDATE message_records
		 SET reply_to_event_id = (SELECT m.new_id FROM ml_event_id_map m WHERE m.old_id = message_records.reply_to_event_id)
		 WHERE reply_to_event_id IN (SELECT old_id FROM ml_event_id_map)`,
		// 旧行 reply_to_event_id 为空：从 reply_to_id 回填（目标在记录内时）
		`UPDATE message_records
		 SET reply_to_event_id = (SELECT m.new_id FROM ml_event_id_map m WHERE m.old_id = message_records.reply_to_id)
		 WHERE reply_to_id IN (SELECT old_id FROM ml_event_id_map)
		   AND (reply_to_event_id IS NULL OR reply_to_event_id = '')`,
	} {
		if err := tx.Exec(stmt).Error; err != nil {
			return fmt.Errorf("rewrite event_id references: %w", err)
		}
	}

	// 回填 reply_to_message_id：旧行只有 reply_to_id（平台消息 ID）
	if err := tx.Exec(`
		UPDATE message_records
		SET reply_to_message_id = reply_to_id
		WHERE (reply_to_message_id IS NULL OR reply_to_message_id = '') AND reply_to_id != ''
	`).Error; err != nil {
		return fmt.Errorf("backfill reply_to_message_id: %w", err)
	}

	// 启用 event_id 唯一约束（幂等写入的前提）
	if err := tx.Exec(
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_mr_event_id ON message_records(event_id)",
	).Error; err != nil {
		return fmt.Errorf("create unique event_id index: %w", err)
	}

	// mentions 幂等：唯一索引 (event_id, mention_id) 保证 UPSERT/重放/重复记录
	// 不会产生重复 @ 行；先清理历史重复行再建索引。
	if err := tx.Exec(`
		DELETE FROM message_mentions
		WHERE id NOT IN (
			SELECT MIN(id) FROM message_mentions GROUP BY event_id, mention_id
		)
	`).Error; err != nil {
		return fmt.Errorf("dedupe mentions: %w", err)
	}
	if err := tx.Exec(
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_mm_event_mention ON message_mentions(event_id, mention_id)",
	).Error; err != nil {
		return fmt.Errorf("create mentions unique index: %w", err)
	}

	// 附件表补列（列结构由 AutoMigrate 在 OpenDB 时已加，此处兜底保证存在）、
	// (event_id, url, name) 唯一索引保证重放幂等、下载器索引支持 status 扫描
	// 与 url_expires_at 回填排序。
	for _, col := range []string{"mime_type", "width", "height"} {
		if m.HasColumn(&AttachmentRecord{}, col) {
			continue
		}
		if err := m.AddColumn(&AttachmentRecord{}, col); err != nil {
			return fmt.Errorf("add attachment column %s: %w", col, err)
		}
	}
	for _, idx := range []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_ma_event_url_name ON message_attachments(event_id, url, name)",
		"CREATE INDEX IF NOT EXISTS idx_ma_status_expires ON message_attachments(status, url_expires_at)",
	} {
		if err := tx.Exec(idx).Error; err != nil {
			return fmt.Errorf("create attachment index: %w", err)
		}
	}

	return nil
}
