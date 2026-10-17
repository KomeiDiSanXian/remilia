// store.go — 任务持久化：存储端口与三种实现（GORM、内存、空实现）。
//
// 与会话存储同构（builtin/ai/session/storage.go）：
//   - storage 插件可用时使用 GORM 存储，任务随进程重启保留；
//   - storage 不可用时退回内存存储，任务仍可在本进程内查询；
//   - DryRun 装配使用空实现，不触碰任何存储。
package task

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
	"gorm.io/gorm/clause"
)

// ErrNotFound 任务不存在。
var ErrNotFound = errors.New("task: not found")

// ErrNilStorageClient 调用方传入了 nil 存储客户端。
var ErrNilStorageClient = errors.New("task: nil storage client")

// Store 任务持久化端口。
type Store interface {
	// Save 保存任务（主键存在时整记录覆盖）。
	Save(t *Task) error
	// Get 按任务 ID 查询；不存在时返回 [ErrNotFound]。
	Get(id string) (*Task, error)
	// ActiveBySession 返回会话最近创建的活动（非终态）任务；没有时返回 [ErrNotFound]。
	ActiveBySession(sessionID string) (*Task, error)
	// ListBySession 返回会话的全部任务，按创建时间倒序；无记录时返回空切片。
	ListBySession(sessionID string) ([]*Task, error)
	// ListActive 返回全部活动（非终态）任务，按创建时间正序；无记录时返回空切片。
	// 供进程启动时恢复中断任务使用。
	ListActive() ([]*Task, error)
}

// Record 对应数据库表 ai_tasks 的任务持久化记录。
//
// 表名显式声明：GORM 对名为 Record 的结构体默认映射到 records 表，
// 而该表已被会话持久化占用（builtin/ai/session 的 Record 未声明表名）。
type Record struct {
	ID string `gorm:"primaryKey"`
	// SessionID 产生任务的会话标识，用于按会话查询。
	SessionID string `gorm:"index"`
	OwnerID   string
	ChatID    string
	Platform  string
	// Goal 用户目标（会话计划的任务描述）。
	Goal string `gorm:"type:text"`
	// Status 任务状态（字符串存储，便于人工排查与跨版本兼容）。
	Status string `gorm:"index"`
	// PlanVersion 计划内容修订号。
	PlanVersion uint64
	// Steps 步骤快照（JSON 序列化）。
	Steps     string `gorm:"type:text"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName 固定任务表名，避免与同库其他 Record 结构体冲突。
func (Record) TableName() string { return "ai_tasks" }

// ToRecord 将任务转换为数据库记录。
func (t *Task) ToRecord() *Record {
	var stepsJSON string
	if len(t.Steps) > 0 {
		if data, err := json.Marshal(t.Steps); err == nil {
			stepsJSON = string(data)
		} else {
			logger.Errorf("[AI] Failed to marshal task steps for %s: %v", t.ID, err)
		}
	}
	return &Record{
		ID:          t.ID,
		SessionID:   t.SessionID,
		OwnerID:     t.OwnerID,
		ChatID:      t.ChatID,
		Platform:    t.Platform,
		Goal:        t.Goal,
		Status:      string(t.Status),
		PlanVersion: t.PlanVersion,
		Steps:       stepsJSON,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

// ToTask 将数据库记录还原为任务。状态或步骤数据损坏时返回错误，
// 由调用方决定跳过该条记录还是整体失败。
func (r *Record) ToTask() (*Task, error) {
	status, err := ParseStatus(r.Status)
	if err != nil {
		return nil, err
	}
	t := &Task{
		ID:          r.ID,
		SessionID:   r.SessionID,
		OwnerID:     r.OwnerID,
		ChatID:      r.ChatID,
		Platform:    r.Platform,
		Goal:        r.Goal,
		Status:      status,
		PlanVersion: r.PlanVersion,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
	if r.Steps != "" {
		if err := json.Unmarshal([]byte(r.Steps), &t.Steps); err != nil {
			return nil, fmt.Errorf("task: unmarshal steps for %s: %w", r.ID, err)
		}
	}
	return t, nil
}

// GormStore 基于 GORM 的任务持久化存储。
type GormStore struct {
	client *infrastorage.Plugin
}

// NewGormStore 创建 GORM 任务存储并自动迁移任务表。
// client 为 nil 时返回 [ErrNilStorageClient] 而非 panic（与会话存储一致）。
func NewGormStore(client *infrastorage.Plugin) (Store, error) {
	if client == nil {
		return nil, ErrNilStorageClient
	}
	if err := client.AutoMigrate(&Record{}); err != nil {
		return nil, fmt.Errorf("task: migrate task table: %w", err)
	}
	return &GormStore{client: client}, nil
}

// Save 保存任务：单语句 upsert，主键存在时更新全部字段。
func (s *GormStore) Save(t *Task) error {
	if t == nil {
		return errors.New("task: nil task")
	}
	return s.client.DB().
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			UpdateAll: true,
		}).
		Create(t.ToRecord()).Error
}

// Get 按 ID 查询任务。
func (s *GormStore) Get(id string) (*Task, error) {
	var rec Record
	if err := s.client.Where("id = ?", id).First(&rec); err != nil {
		if errors.Is(err, infrastorage.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return rec.ToTask()
}

// ActiveBySession 返回会话最近创建的活动任务。
func (s *GormStore) ActiveBySession(sessionID string) (*Task, error) {
	var recs []Record
	err := s.client.
		Where("session_id = ? AND status NOT IN ?", sessionID, terminalStatusStrings()).
		Order("created_at DESC").
		Find(&recs)
	if err != nil {
		return nil, err
	}
	return firstValidTask(recs)
}

// ListBySession 返回会话的全部任务，按创建时间倒序。
func (s *GormStore) ListBySession(sessionID string) ([]*Task, error) {
	var recs []Record
	if err := s.client.
		Where("session_id = ?", sessionID).
		Order("created_at DESC").
		Find(&recs); err != nil {
		return nil, err
	}
	return recordsToTasks(recs), nil
}

// ListActive 返回全部活动任务，按创建时间正序。
func (s *GormStore) ListActive() ([]*Task, error) {
	var recs []Record
	if err := s.client.
		Where("status NOT IN ?", terminalStatusStrings()).
		Order("created_at ASC").
		Find(&recs); err != nil {
		return nil, err
	}
	return recordsToTasks(recs), nil
}

// recordsToTasks 转换记录集；单条损坏（跨版本降级、手工改库）只跳过该条，
// 不让整个查询失败。
func recordsToTasks(recs []Record) []*Task {
	out := make([]*Task, 0, len(recs))
	for i := range recs {
		t, err := recs[i].ToTask()
		if err != nil {
			logger.Warnf("[AI] Skipping corrupted task record %s: %v", recs[i].ID, err)
			continue
		}
		out = append(out, t)
	}
	return out
}

// firstValidTask 返回记录集中的首条有效任务；全部损坏时返回 [ErrNotFound]。
func firstValidTask(recs []Record) (*Task, error) {
	for i := range recs {
		t, err := recs[i].ToTask()
		if err != nil {
			logger.Warnf("[AI] Skipping corrupted task record %s: %v", recs[i].ID, err)
			continue
		}
		return t, nil
	}
	return nil, ErrNotFound
}

// terminalStatusStrings 终态集合（SQL IN 查询用）。
func terminalStatusStrings() []string {
	out := make([]string, 0, len(allStatuses))
	for _, s := range allStatuses {
		if s.IsTerminal() {
			out = append(out, string(s))
		}
	}
	return out
}

// MemStore 进程内任务存储，storage 插件不可用时使用。
// 任务在进程重启后丢失，语义与内存态会话一致。
type MemStore struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

// NewMemStore 创建空的内存任务存储。
func NewMemStore() *MemStore {
	return &MemStore{tasks: make(map[string]*Task)}
}

// Save 保存任务副本。
func (m *MemStore) Save(t *Task) error {
	if t == nil {
		return errors.New("task: nil task")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[t.ID] = t.Clone()
	return nil
}

// Get 按 ID 返回任务副本。
func (m *MemStore) Get(id string) (*Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[id]
	if !ok {
		return nil, ErrNotFound
	}
	return t.Clone(), nil
}

// ActiveBySession 返回会话最近创建的活动任务。
func (m *MemStore) ActiveBySession(sessionID string) (*Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var best *Task
	for _, t := range m.tasks {
		if t.SessionID != sessionID || !t.Active() {
			continue
		}
		if best == nil || t.CreatedAt.After(best.CreatedAt) {
			best = t
		}
	}
	if best == nil {
		return nil, ErrNotFound
	}
	return best.Clone(), nil
}

// ListBySession 返回会话的全部任务，按创建时间倒序。
func (m *MemStore) ListBySession(sessionID string) ([]*Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		if t.SessionID == sessionID {
			out = append(out, t.Clone())
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return strings.Compare(out[i].ID, out[j].ID) > 0
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// ListActive 返回全部活动任务，按创建时间正序。
func (m *MemStore) ListActive() ([]*Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		if t.Active() {
			out = append(out, t.Clone())
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return strings.Compare(out[i].ID, out[j].ID) < 0
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

// NoopStore 空实现，不进行任何持久化（DryRun 装配使用）。
type NoopStore struct{}

// Save 空实现，始终成功。
func (NoopStore) Save(*Task) error { return nil }

// Get 空实现，始终返回 [ErrNotFound]。
func (NoopStore) Get(string) (*Task, error) { return nil, ErrNotFound }

// ActiveBySession 空实现，始终返回 [ErrNotFound]。
func (NoopStore) ActiveBySession(string) (*Task, error) { return nil, ErrNotFound }

// ListBySession 空实现，始终返回空切片。
func (NoopStore) ListBySession(string) ([]*Task, error) { return nil, nil }

// ListActive 空实现，始终返回空切片。
func (NoopStore) ListActive() ([]*Task, error) { return nil, nil }
