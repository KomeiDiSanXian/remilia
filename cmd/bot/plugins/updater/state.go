package updater

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// stateFileName 状态文件（位于 data 目录）。
const stateFileName = "state.json"

// updaterState 更新器的持久化状态。
type updaterState struct {
	LastCheck   time.Time `json:"last_check"`   // 上次检查时间
	LastVersion string    `json:"last_version"` // 上次检查到的远端版本
	Applied     string    `json:"applied"`      // 最近一次成功应用的版本
	AutoCheck   bool      `json:"auto_check"`   // 自动检查开关（/update auto on|off）
}

// stateStore 负责状态文件的并发安全读写。
type stateStore struct {
	dir  string
	path string
	mu   sync.Mutex
}

func newStateStore(dir string) *stateStore {
	return &stateStore{dir: dir, path: filepath.Join(dir, stateFileName)}
}

func (s *stateStore) load() *updaterState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *stateStore) loadLocked() *updaterState {
	st := &updaterState{}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			// 首次运行：自动检查默认开启（auto_apply 仍默认关闭）
			st.AutoCheck = true
		}
		return st
	}
	if err := json.Unmarshal(data, st); err != nil {
		return &updaterState{AutoCheck: true}
	}
	return st
}

func (s *stateStore) saveLocked(st *updaterState) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// update 原子地读取-修改-落盘（持锁进行，避免并发读写交错）。
func (s *stateStore) update(fn func(*updaterState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.loadLocked()
	fn(st)
	_ = s.saveLocked(st)
}
