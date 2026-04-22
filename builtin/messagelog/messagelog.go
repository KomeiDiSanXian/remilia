// Package messagelog 提供轻量的群消息历史记录功能（修复框架问题 #9）。
//
// 消息以环形缓冲区方式存储于内存，按群/用户分片，支持：
//   - [Record] 记录一条消息
//   - [QueryGroup] 查询群最近 N 条消息
//   - [QueryUser] 查询用户最近 N 条消息
//   - [WordFreq] 统计群内词频（用于词频/词云插件）
//   - [Clear] 清理指定时间前的消息（防止内存无限增长）
//
// 默认全局实例通过包级函数访问；也可用 [New] 创建独立实例。
//
// 典型使用流程（在插件 Setup 中注册被动监听，记录消息）：
//
//	// Setup
//	ctx.Reg.RegisterMatcher("").Handle(func(c *eventctx.Context) error {
//	    messagelog.Record(messagelog.Message{
//	        GroupID:   c.GetGroupID(),
//	        UserID:    c.GetSenderID(),
//	        Content:   c.GetMessageContent(),
//	        Timestamp: time.Now(),
//	    })
//	    return nil
//	})
//
//	// 词频统计
//	freq := messagelog.WordFreq(groupID, 1000) // 最近1000条消息的词频
package messagelog

import (
	"strings"
	"sync"
	"time"
	"unicode"
)

// DefaultCapacity 每个群/用户的默认环形缓冲区大小
const DefaultCapacity = 1000

// Message 一条消息记录。
type Message struct {
	// GroupID 群组 ID；私聊场景留空
	GroupID string
	// UserID 发送者 ID
	UserID string
	// Content 消息文本内容
	Content string
	// Timestamp 消息接收时间
	Timestamp time.Time
}

// ring 单个维度（群或用户）的环形缓冲区
type ring struct {
	buf  []Message
	head int // 下一次写入位置
	size int // 当前有效元素数
	cap_ int // 缓冲区容量
}

func newRing(cap int) *ring {
	if cap <= 0 {
		cap = DefaultCapacity
	}
	return &ring{buf: make([]Message, cap), cap_: cap}
}

func (r *ring) add(m Message) {
	r.buf[r.head] = m
	r.head = (r.head + 1) % r.cap_
	if r.size < r.cap_ {
		r.size++
	}
}

// snapshot 返回最近 n 条消息（从旧到新）
func (r *ring) snapshot(n int) []Message {
	if n <= 0 || r.size == 0 {
		return nil
	}
	if n > r.size {
		n = r.size
	}
	out := make([]Message, n)
	// 起始位置：从最旧的一条往后读 n 条
	start := (r.head - n + r.cap_) % r.cap_
	for i := range out {
		out[i] = r.buf[(start+i)%r.cap_]
	}
	return out
}

// Logger 消息日志记录器。
//
// 使用独立实例时通过 [New] 创建。
type Logger struct {
	cap      int
	groupMu  sync.RWMutex
	groupBuf map[string]*ring // groupID → ring
	userMu   sync.RWMutex
	userBuf  map[string]*ring // userID → ring
}

// New 创建一个新的 Logger，cap 为每个群/用户的环形缓冲区容量。
// cap <= 0 时使用 [DefaultCapacity]。
func New(cap int) *Logger {
	if cap <= 0 {
		cap = DefaultCapacity
	}
	return &Logger{
		cap:      cap,
		groupBuf: make(map[string]*ring),
		userBuf:  make(map[string]*ring),
	}
}

// Record 记录一条消息。
//
// GroupID 为空时跳过群维度索引；UserID 为空时跳过用户维度索引。
// 两者均不为空时同时更新两个索引。
func (l *Logger) Record(m Message) {
	if m.Content == "" {
		return
	}
	if m.GroupID != "" {
		l.groupMu.Lock()
		r, ok := l.groupBuf[m.GroupID]
		if !ok {
			r = newRing(l.cap)
			l.groupBuf[m.GroupID] = r
		}
		r.add(m)
		l.groupMu.Unlock()
	}
	if m.UserID != "" {
		l.userMu.Lock()
		r, ok := l.userBuf[m.UserID]
		if !ok {
			r = newRing(l.cap)
			l.userBuf[m.UserID] = r
		}
		r.add(m)
		l.userMu.Unlock()
	}
}

// QueryGroup 返回群 groupID 最近 n 条消息（从旧到新）。
// n <= 0 或群无记录时返回 nil。
func (l *Logger) QueryGroup(groupID string, n int) []Message {
	if groupID == "" || n <= 0 {
		return nil
	}
	l.groupMu.RLock()
	r := l.groupBuf[groupID]
	l.groupMu.RUnlock()
	if r == nil {
		return nil
	}
	l.groupMu.RLock()
	defer l.groupMu.RUnlock()
	return r.snapshot(n)
}

// QueryUser 返回用户 userID 最近 n 条消息（从旧到新）。
func (l *Logger) QueryUser(userID string, n int) []Message {
	if userID == "" || n <= 0 {
		return nil
	}
	l.userMu.RLock()
	r := l.userBuf[userID]
	l.userMu.RUnlock()
	if r == nil {
		return nil
	}
	l.userMu.RLock()
	defer l.userMu.RUnlock()
	return r.snapshot(n)
}

// WordFreq 统计群 groupID 最近 n 条消息中的词频。
//
// 分词策略：按 Unicode 空白分割，过滤长度 < 2 的词及纯标点词。
// 返回 map[词]出现次数，可直接用于词云/排行展示。
//
// 示例：
//
//	freq := logger.WordFreq("group123", 500)
//	// freq = map["大家":12 "你好":8 ...]
func (l *Logger) WordFreq(groupID string, n int) map[string]int {
	msgs := l.QueryGroup(groupID, n)
	freq := make(map[string]int)
	for _, m := range msgs {
		for _, word := range tokenize(m.Content) {
			freq[word]++
		}
	}
	return freq
}

// Clear 删除所有群/用户中时间戳早于 before 的消息记录。
//
// 建议定期调用（如每天凌晨）防止内存无限增长。
// 对于不再活跃的群/用户，其缓冲区也会被完全清除。
func (l *Logger) Clear(before time.Time) {
	l.groupMu.Lock()
	for gid, r := range l.groupBuf {
		pruned := pruneRing(r, before, l.cap)
		if pruned.size == 0 {
			delete(l.groupBuf, gid)
		} else {
			l.groupBuf[gid] = pruned
		}
	}
	l.groupMu.Unlock()

	l.userMu.Lock()
	for uid, r := range l.userBuf {
		pruned := pruneRing(r, before, l.cap)
		if pruned.size == 0 {
			delete(l.userBuf, uid)
		} else {
			l.userBuf[uid] = pruned
		}
	}
	l.userMu.Unlock()
}

// GroupCount 返回已记录消息的群数量。
func (l *Logger) GroupCount() int {
	l.groupMu.RLock()
	defer l.groupMu.RUnlock()
	return len(l.groupBuf)
}

// UserCount 返回已记录消息的用户数量。
func (l *Logger) UserCount() int {
	l.userMu.RLock()
	defer l.userMu.RUnlock()
	return len(l.userBuf)
}

// GroupMessageCount 返回群 groupID 已记录的消息数量。
func (l *Logger) GroupMessageCount(groupID string) int {
	l.groupMu.RLock()
	r := l.groupBuf[groupID]
	l.groupMu.RUnlock()
	if r == nil {
		return 0
	}
	return r.size
}

// pruneRing 返回只保留时间 >= before 的消息的新环形缓冲区
func pruneRing(r *ring, before time.Time, cap int) *ring {
	all := r.snapshot(r.size)
	out := newRing(cap)
	for _, m := range all {
		if !m.Timestamp.Before(before) {
			out.add(m)
		}
	}
	return out
}

// tokenize 简单分词：按空白切割，过滤纯标点/长度<2 的 token
func tokenize(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if len([]rune(f)) < 2 {
			continue
		}
		// 过滤全为标点/符号的词
		allPunct := true
		for _, r := range f {
			if !unicode.IsPunct(r) && !unicode.IsSymbol(r) {
				allPunct = false
				break
			}
		}
		if !allPunct {
			out = append(out, f)
		}
	}
	return out
}

// --- 全局默认实例 ---

var defaultLogger = New(DefaultCapacity)

// Record 使用全局默认实例记录消息。
func Record(m Message) { defaultLogger.Record(m) }

// QueryGroup 使用全局默认实例查询群消息。
func QueryGroup(groupID string, n int) []Message { return defaultLogger.QueryGroup(groupID, n) }

// QueryUser 使用全局默认实例查询用户消息。
func QueryUser(userID string, n int) []Message { return defaultLogger.QueryUser(userID, n) }

// WordFreq 使用全局默认实例统计群词频。
func WordFreq(groupID string, n int) map[string]int { return defaultLogger.WordFreq(groupID, n) }

// Clear 使用全局默认实例清理旧消息。
func Clear(before time.Time) { defaultLogger.Clear(before) }

// Default 返回全局默认 Logger 实例。
func Default() *Logger { return defaultLogger }
