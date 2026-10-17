// Package ai taskapi.go — 任务记录的查询与取消：面向管理面（REST API）的导出方法。
//
// 与 /ai task 命令共用同一份任务存储；这里只做参数校验与约定（上限、空会话
// 拒绝），不复制任何查询逻辑。
package ai

import (
	"errors"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/task"
)

// defaultTaskQueryLimit 管理面单次查询的默认条数上限。
const defaultTaskQueryLimit = 50

// ErrTaskSessionRequired 查询任务时未提供会话标识。
var ErrTaskSessionRequired = errors.New("ai: session id is required")

// TasksBySession 返回指定会话的任务记录，按创建时间倒序。
// limit <= 0 时取默认上限。
func (p *Plugin) TasksBySession(sessionID string, limit int) ([]*task.Task, error) {
	if p == nil {
		return nil, task.ErrNotFound
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, ErrTaskSessionRequired
	}
	list, err := p.taskStore().ListBySession(sessionID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultTaskQueryLimit
	}
	if len(list) > limit {
		list = list[:limit]
	}
	return list, nil
}

// TaskByID 按任务 ID 查询任务；不存在时返回 [task.ErrNotFound]。
func (p *Plugin) TaskByID(id string) (*task.Task, error) {
	if p == nil {
		return nil, task.ErrNotFound
	}
	return p.taskStore().Get(strings.TrimSpace(id))
}

// CancelTaskByID 取消指定任务；会话仍在内存中时同步取消其计划，
// 避免计划继续驱动已取消的任务。
func (p *Plugin) CancelTaskByID(id string) error {
	if p == nil {
		return task.ErrNotFound
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return task.ErrNotFound
	}

	p.taskMu.Lock()
	defer p.taskMu.Unlock()

	t, err := p.taskStore().Get(id)
	if err != nil {
		return err
	}
	if err := t.Cancel(); err != nil {
		return err
	}
	recordTaskStatus(t.Status)
	if err := p.taskStore().Save(t); err != nil {
		return err
	}

	if t.SessionID != "" && p.sm != nil {
		if sess := p.sm.Peek(t.SessionID); sess != nil && sess.CancelPlan() {
			p.sm.SaveSession(sess)
		}
	}
	return nil
}
