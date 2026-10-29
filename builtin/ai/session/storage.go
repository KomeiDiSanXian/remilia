// storage.go — 会话持久化的两个实现：GORM 存储与空实现。

package session

import (
	"errors"
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
	"gorm.io/gorm/clause"
)

// GormStore 基于 GORM 的会话持久化存储。
// 使用 storage 插件提供的 Client 接口进行数据库操作。
type GormStore struct {
	client *infrastorage.Plugin
}

// ErrNilStorageClient 表示调用方传入了 nil 存储客户端。
var ErrNilStorageClient = errors.New("ai: nil storage client")

// ErrNilSession 表示调用方传入了 nil 会话。
var ErrNilSession = errors.New("ai: nil session")

// legacyRecordTable 历史版本使用的表名（由 GORM 对 Record 的默认复数化简得来）。
const legacyRecordTable = "records"

// NewGormSessionStore 创建一个基于 GORM 的会话存储。
// 初始化时会自动创建 ai_session_records 表；若检测到历史版本遗留的
// records 表而新表尚不存在，则先原地改名（SQLite/PG/MySQL 的
// ALTER TABLE ... RENAME TO 都支持，不重建、不复制数据）。
//
// client 为 nil 时返回 [ErrNilStorageClient] 而非 panic：装配侧把非 nil error 视为
// 「无持久化，回退内存存储」，构造函数不该用 panic 处理调用方传参问题；
// 显式判空同时避免了一次真实的内存解引用——该访问违例在 Windows + -race 下偶发被
// 运行时升级为 fatal error（unexpected signal during runtime execution / 0xc0000005），
// 会使整个测试二进制崩溃。
func NewGormSessionStore(client *infrastorage.Plugin) (SessionStore, error) {
	if client == nil {
		return nil, ErrNilStorageClient
	}
	if err := migrateLegacyRecordTable(client); err != nil {
		return nil, err
	}
	if err := client.AutoMigrate(&Record{}); err != nil {
		return nil, fmt.Errorf("ai: migrate session table: %w", err)
	}
	dropLegacyIndexes(client)
	return &GormStore{client: client}, nil
}

// migrateLegacyRecordTable 把历史表 records 改名为 ai_session_records。
//
// 改名失败返回错误而不是继续建新表：一旦新表被建出来，下次启动就会
// 看到"新表已存在"而直接跳过迁移，旧表里的历史从此永久读不到（数据还在，
// 但应用看不见，也没有任何告警）。返回错误让装配侧退回纯内存并在日志里
// 说明，下次启动会重试改名——保住"要么改名成功、要么原表不动"这个不变式。
func migrateLegacyRecordTable(client *infrastorage.Plugin) error {
	migrator := client.DB().Migrator()
	if migrator.HasTable(SessionRecordTable) || !migrator.HasTable(legacyRecordTable) {
		return nil
	}
	if err := migrator.RenameTable(legacyRecordTable, SessionRecordTable); err != nil {
		return fmt.Errorf("ai: rename legacy session table %q to %q: %w "+
			"(history stays in %q; fix the cause and restart to retry)",
			legacyRecordTable, SessionRecordTable, err, legacyRecordTable)
	}
	logger.Infof("[AI] Migrated session table %q -> %q", legacyRecordTable, SessionRecordTable)
	return nil
}

// dropLegacyIndexes 删除历史版本在 user_id / chat_id 上建的索引。
//
// 没有任何查询按这两列过滤（会话一律按 ID 取），它们只是每次整记录 upsert
// 的额外写入开销。GORM 的 AutoMigrate 只增不删，所以这里显式清理。
//
// 索引名要按两张表都试一遍：历史版本的索引建于 records 表（名为
// idx_records_*），RENAME TABLE 之后索引名不会跟着改，只按新表名去找会漏掉。
// 失败只记日志——索引残留不影响正确性。
func dropLegacyIndexes(client *infrastorage.Plugin) {
	db := client.DB()
	for _, column := range []string{"UserID", "ChatID"} {
		col := db.NamingStrategy.ColumnName("", column)
		for _, table := range []string{SessionRecordTable, legacyRecordTable} {
			name := db.NamingStrategy.IndexName(table, col)
			if !db.Migrator().HasIndex(&Record{}, name) {
				continue
			}
			if err := db.Migrator().DropIndex(&Record{}, name); err != nil {
				logger.Warnf("[AI] Failed to drop legacy session index %q: %v", name, err)
				continue
			}
			logger.Infof("[AI] Dropped legacy session index %q", name)
		}
	}
}

// Load 从数据库加载会话。
// 会话不存在时返回 (nil, nil) 而非错误；行存在但历史损坏时返回错误，
// 由调用方决定降级策略（见 SessionManager.GetOrCreate）。
func (s *GormStore) Load(sessionID string) (*Session, error) {
	var rec Record
	if err := s.client.Where("id = ?", sessionID).First(&rec); err != nil {
		if errors.Is(err, infrastorage.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return rec.ToSession()
}

// Save 保存会话到数据库（单语句 upsert，避免 SELECT+INSERT/UPDATE 两次往返）。
// 主键存在时更新全部字段，不存在时插入。
func (s *GormStore) Save(session *Session) error {
	if session == nil {
		return ErrNilSession
	}
	rec, err := session.ToRecord()
	if err != nil {
		return err
	}
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

// PurgeExpired 删除 updated_at 早于 before 的全部会话行，返回删除条数。
//
// 这是 CleanupExpired 的库侧兜底：内存 LRU 只覆盖"当前驻留"的会话，
// 被 LRU 淘汰后再没被加载过的行永远不会经过内存判定，没有这一步数据库
// 会随运行时间单调增长。
//
// 实现 [ExpiredPurger]，由 SessionManager.CleanupExpired 按需调用。
func (s *GormStore) PurgeExpired(before time.Time) (int64, error) {
	result := s.client.DB().Where("updated_at < ?", before).Delete(&Record{})
	return result.RowsAffected, result.Error
}

// NoopStore 空实现，不进行任何持久化。
// 当 storage 插件不可用时使用。
type NoopStore struct{}

func (n *NoopStore) Load(_ string) (*Session, error) { return nil, nil }
func (n *NoopStore) Save(_ *Session) error           { return nil }
func (n *NoopStore) Delete(_ string) error           { return nil }

// PurgeExpired 空实现：没有持久化数据可清理。
func (n *NoopStore) PurgeExpired(_ time.Time) (int64, error) { return 0, nil }
