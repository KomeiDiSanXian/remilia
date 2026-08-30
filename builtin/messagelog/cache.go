package messagelog

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"
)

// cacheRing 单个会话的有界热缓存（环形缓冲，保留最近 cap_ 条）。
// lastAccessNano 用于指标与未来扩展；淘汰顺序由 cache.order 写序 LRU 决定。
type cacheRing struct {
	buf            []RecordEntry
	head           int
	size           int
	cap_           int
	lastAccessNano atomic.Int64
}

func newCacheRing(cap int) *cacheRing {
	if cap <= 0 {
		cap = DefaultCapacity
	}
	return &cacheRing{buf: make([]RecordEntry, cap), cap_: cap}
}

func (r *cacheRing) touch() {
	r.lastAccessNano.Store(time.Now().UnixNano())
}

// add 写入一条记录；ring 满时覆盖最旧条目。
func (r *cacheRing) add(e RecordEntry) {
	r.buf[r.head] = e
	r.head = (r.head + 1) % r.cap_
	if r.size < r.cap_ {
		r.size++
	}
	r.touch()
}

// snapshot 返回最近 n 条（旧→新）。
func (r *cacheRing) snapshot(n int) []RecordEntry {
	if n <= 0 || r.size == 0 {
		return nil
	}
	if n > r.size {
		n = r.size
	}
	out := make([]RecordEntry, n)
	start := (r.head - n + r.cap_) % r.cap_
	for i := range out {
		out[i] = r.buf[(start+i)%r.cap_]
	}
	r.touch()
	return out
}

// collect 返回满足 pred 的最近最多 n 条（旧→新）。
// 从最新向旧扫描，O(n + 跳过条数)，热路径查询避免整环快照。
func (r *cacheRing) collect(n int, pred func(RecordEntry) bool) []RecordEntry {
	if n <= 0 || r.size == 0 {
		return nil
	}
	found := make([]RecordEntry, 0, n)
	// 从最新（head-1）向最旧扫描
	for i := 0; i < r.size && len(found) < n; i++ {
		idx := (r.head - 1 - i + 2*r.cap_) % r.cap_
		if e := r.buf[idx]; pred(e) {
			found = append(found, e)
		}
	}
	if len(found) <= 1 {
		return found
	}
	// 逆序为旧→新
	for i, j := 0, len(found)-1; i < j; i, j = i+1, j-1 {
		found[i], found[j] = found[j], found[i]
	}
	r.touch()
	return found
}

// trimOldest 移除最旧的 k 条（全局预算回收）。返回实际移除条数。
func (r *cacheRing) trimOldest(k int) int {
	if k <= 0 || r.size == 0 {
		return 0
	}
	if k > r.size {
		k = r.size
	}
	keep := r.size - k
	if keep == 0 {
		r.head = 0
		r.size = 0
		return k
	}
	// 环形结构裁剪需重建缓冲（O(cap) 拷贝；淘汰频率由全局预算摊销控制）
	out := make([]RecordEntry, r.cap_)
	start := (r.head - r.size + r.cap_) % r.cap_ // 最旧下标
	for i := range keep {
		out[i] = r.buf[(start+k+i)%r.cap_]
	}
	r.buf = out
	r.head = keep % r.cap_
	r.size = keep
	return k
}

// retain 仅保留满足 pred 的条目（Clear 时间裁剪用）。返回保留条数。
func (r *cacheRing) retain(pred func(RecordEntry) bool) int {
	if r.size == 0 {
		return 0
	}
	all := r.snapshot(r.size)
	out := newCacheRing(r.cap_)
	kept := 0
	for _, e := range all {
		if pred(e) {
			out.add(e)
			kept++
		}
	}
	// 逐字段重建，避免拷贝 atomic.Int64（go vet noCopy 检查）
	r.buf = out.buf
	r.head = out.head
	r.size = out.size
	r.touch()
	return kept
}

// cache 有界热缓存：per-chat ring + 全局条目预算 + 写序近似 LRU 淘汰。
//
// 内存与群数/用户数解耦的关键：global > 0 时，总条目数超过预算即从
// 最近未写入的会话批量回收最旧条目（Ephemeral 层，淘汰可接受）。
//
// 淘汰顺序由 order 写序链表维护（front = 最近写入，back = 最久未写入），
// 定位回收目标 O(1)，淘汰成本不随群数增长。
type cache struct {
	mu         sync.RWMutex
	chats      map[string]*cacheRing
	order      *list.List               // 写序 LRU：元素值为 chatID
	orderIdx   map[string]*list.Element // chatID → order 元素
	total      int
	perChat    int
	global     int
	idleEvict  bool
	evictBatch int
	// lastReported 上次上报 cacheEntries gauge 的 total（摊销：热路径不逐条 Set）
	lastReported int
}

func newCache(perChat, global int, idleEvict bool) *cache {
	if perChat <= 0 {
		perChat = DefaultCapacity
	}
	return &cache{
		chats:      make(map[string]*cacheRing),
		order:      list.New(),
		orderIdx:   make(map[string]*list.Element),
		perChat:    perChat,
		global:     global,
		idleEvict:  idleEvict,
		evictBatch: 50,
	}
}

// add 写入 chatID 的一条热缓存记录；超全局预算时触发近似 LRU 淘汰。
func (c *cache) add(chatID string, e RecordEntry) {
	if chatID == "" {
		return
	}
	c.mu.Lock()
	r, ok := c.chats[chatID]
	if !ok {
		r = newCacheRing(c.perChat)
		c.chats[chatID] = r
		c.orderIdx[chatID] = c.order.PushFront(chatID)
	} else {
		c.order.MoveToFront(c.orderIdx[chatID])
	}
	before := r.size
	r.add(e)
	if r.size > before {
		c.total++
	}
	// 预算摊销：超过 global + evictBatch 才批量回收，避免每次写入都扫描
	evicted := false
	if c.global > 0 && c.total > c.global+c.evictBatch {
		evicted = c.evictLocked()
	}
	// 摊销上报：发生淘汰或 total 变化超过 256 才更新 gauge（避免每次 Gauge.Set）
	if evicted || c.total-c.lastReported >= 256 {
		mlMetricsInst.cacheEntries.Set(float64(c.total))
		c.lastReported = c.total
	}
	c.mu.Unlock()
}

// evictLocked 批量回收直到 total <= global。
// 从写序链表尾部（最久未写入的会话）开始回收，定位 O(1)。
func (c *cache) evictLocked() bool {
	evicted := 0
	for c.total > c.global {
		el := c.order.Back()
		if el == nil {
			break
		}
		chatID := el.Value.(string)
		target := c.chats[chatID]
		if target == nil {
			// 防御：order 与 chats 失步，清理该元素
			c.order.Remove(el)
			delete(c.orderIdx, chatID)
			continue
		}
		if target.size <= c.evictBatch {
			// 空闲/小会话整环回收，彻底释放该会话的缓存
			delete(c.chats, chatID)
			c.order.Remove(el)
			delete(c.orderIdx, chatID)
			c.total -= target.size
			evicted += target.size
			continue
		}
		n := target.trimOldest(c.evictBatch)
		if n == 0 {
			c.order.Remove(el)
			delete(c.orderIdx, chatID)
			continue
		}
		c.total -= n
		evicted += n
		// 该会话仍是最久未写入，留在链表尾部，下次继续回收
	}
	if evicted > 0 {
		mlMetricsInst.cacheEvictions.Add(float64(evicted))
	}
	return evicted > 0
}

// snapshotAll 返回 chatID 全部缓存条目（旧→新，数量受 perChat 限制）。
func (c *cache) snapshotAll(chatID string) []RecordEntry {
	c.mu.RLock()
	r := c.chats[chatID]
	c.mu.RUnlock()
	if r == nil {
		return nil
	}
	return r.snapshot(r.size)
}

// collect 返回 chatID 满足 pred 的最近最多 n 条（旧→新）。
func (c *cache) collect(chatID string, n int, pred func(RecordEntry) bool) []RecordEntry {
	c.mu.RLock()
	r := c.chats[chatID]
	c.mu.RUnlock()
	if r == nil {
		return nil
	}
	return r.collect(n, pred)
}

// find 在 chatID 的缓存中按逻辑身份（event_id）或平台消息 ID 查找。
func (c *cache) find(chatID, eventID string) (RecordEntry, bool) {
	c.mu.RLock()
	r := c.chats[chatID]
	c.mu.RUnlock()
	if r == nil {
		return RecordEntry{}, false
	}
	for _, e := range r.snapshot(r.size) {
		if e.EventID == eventID || e.PlatformMessageID == eventID {
			return e, true
		}
	}
	return RecordEntry{}, false
}

// pruneBefore 移除时间早于 before 的缓存条目；空会话整环删除。
func (c *cache) pruneBefore(before time.Time) {
	c.mu.Lock()
	for id, r := range c.chats {
		beforeSize := r.size
		kept := r.retain(func(e RecordEntry) bool { return !e.Timestamp.Before(before) })
		c.total -= beforeSize - kept
		if kept == 0 {
			delete(c.chats, id)
			if el, ok := c.orderIdx[id]; ok {
				c.order.Remove(el)
				delete(c.orderIdx, id)
			}
		}
	}
	mlMetricsInst.cacheEntries.Set(float64(c.total))
	c.lastReported = c.total
	c.mu.Unlock()
}

// chatCount 返回有缓存条目的会话数。
func (c *cache) chatCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.chats)
}

// entryCount 返回 chatID 的缓存条目数。
func (c *cache) entryCount(chatID string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r := c.chats[chatID]
	if r == nil {
		return 0
	}
	return r.size
}
