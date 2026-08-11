package bilibili

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// watchFileName 开播订阅状态文件（位于 data/bilibili 目录）。
const watchFileName = "watch.json"

// watchEntry 一个会话（群/私聊）对某 UP 主的开播订阅。
type watchEntry struct {
	ChatID string `json:"chat_id"`
	UID    int64  `json:"uid"`
	Name   string `json:"name"`
	Living bool   `json:"living"`
}

// notifierFn 主动推送函数（chatID, 消息）→ error。
type notifierFn func(chatID, msg string) error

// watchManager 管理开播订阅的持久化与状态跟踪。
// 参照 updater 的 stateStore 模式：JSON 原子落盘 + 互斥锁。
type watchManager struct {
	mu       sync.Mutex
	path     string
	entries  []watchEntry
	interval time.Duration
	notifier atomic.Value // 保存 notifierFn（首次推送时注册）
}

func newWatchManager(dir string, interval time.Duration) *watchManager {
	w := &watchManager{
		path:     filepath.Join(dir, watchFileName),
		interval: interval,
	}
	w.load()
	return w
}

func (w *watchManager) load() {
	w.mu.Lock()
	defer w.mu.Unlock()
	data, err := os.ReadFile(w.path)
	if err != nil {
		return // 首次运行：空订阅
	}
	_ = json.Unmarshal(data, &w.entries)
}

func (w *watchManager) save() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(w.entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := w.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, w.path)
}

// add 添加订阅并持久化。
func (w *watchManager) add(chatID string, uid int64, name string, live *LiveInfo) {
	w.mu.Lock()
	w.entries = append(w.entries, watchEntry{
		ChatID: chatID,
		UID:    uid,
		Name:   name,
		Living: live != nil && live.IsLiving,
	})
	w.mu.Unlock()
	_ = w.save()
}

// remove 移除订阅，返回是否命中。
func (w *watchManager) remove(chatID string, uid int64) bool {
	w.mu.Lock()
	idx := -1
	for i, e := range w.entries {
		if e.ChatID == chatID && e.UID == uid {
			idx = i
			break
		}
	}
	if idx < 0 {
		w.mu.Unlock()
		return false
	}
	w.entries = append(w.entries[:idx], w.entries[idx+1:]...)
	w.mu.Unlock()
	_ = w.save()
	return true
}

// contains 判断某会话是否已订阅该 UID。
func (w *watchManager) contains(chatID string, uid int64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, e := range w.entries {
		if e.ChatID == chatID && e.UID == uid {
			return true
		}
	}
	return false
}

// list 返回某会话的全部订阅。
func (w *watchManager) list(chatID string) []watchEntry {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []watchEntry
	for _, e := range w.entries {
		if e.ChatID == chatID {
			out = append(out, e)
		}
	}
	return out
}

// all 返回全部订阅（跨会话去重后的原始列表）。
func (w *watchManager) all() []watchEntry {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]watchEntry, len(w.entries))
	copy(out, w.entries)
	return out
}

// byUID 返回订阅了指定 UID 的全部会话。
func (w *watchManager) byUID(uid int64) []watchEntry {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []watchEntry
	for _, e := range w.entries {
		if e.UID == uid {
			out = append(out, e)
		}
	}
	return out
}

// setLiving 更新某订阅的直播状态（初始化用，不触发推送）。
func (w *watchManager) setLiving(chatID string, uid int64, living bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range w.entries {
		if w.entries[i].ChatID == chatID && w.entries[i].UID == uid {
			w.entries[i].Living = living
		}
	}
}

// applyLive 应用一次轮询结果：状态由"未开播→开播"时回调 notify。
func (w *watchManager) applyLive(uid int64, live *LiveInfo, notify func(int64, *LiveInfo)) {
	if live == nil || !live.IsLiving {
		w.setAllLiving(uid, false)
		return
	}
	w.mu.Lock()
	wasLiving := false
	for i := range w.entries {
		if w.entries[i].UID == uid {
			wasLiving = wasLiving || w.entries[i].Living
			w.entries[i].Living = true
		}
	}
	w.mu.Unlock()
	if !wasLiving && notify != nil {
		notify(uid, live)
	}
}

func (w *watchManager) setAllLiving(uid int64, living bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range w.entries {
		if w.entries[i].UID == uid {
			w.entries[i].Living = living
		}
	}
}

// hasNotifier 判断主动推送能力是否已注册。
func (w *watchManager) hasNotifier() bool {
	return w.notifier.Load() != nil
}

// setNotifier 注册主动推送函数（幂等，多次调用仅首次生效）。
func (w *watchManager) setNotifier(fn notifierFn) {
	w.notifier.CompareAndSwap(nil, notifierFn(fn))
}
