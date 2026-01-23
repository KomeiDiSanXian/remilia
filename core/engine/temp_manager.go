package engine

import (
	"container/heap"
	"sort"
	"sync"
	"time"
	"unsafe"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

const tempMatcherShardCount = 8

// tempMatcherShard holds a subset of temp matchers
type tempMatcherShard struct {
	mu           sync.RWMutex
	matcherIndex map[dto.EventType][]*Matcher // Sorted by priority
	expiration   *matcherHeap                 // Min-heap for expiration
	byID         map[*Matcher]struct{}        // Fast existence check
}

func newTempMatcherShard() *tempMatcherShard {
	return &tempMatcherShard{
		matcherIndex: make(map[dto.EventType][]*Matcher),
		expiration:   &matcherHeap{},
		byID:         make(map[*Matcher]struct{}),
	}
}

// tempMatcherManager manage temporary matchers with sharding and optimized insertion
type tempMatcherManager struct {
	shards [tempMatcherShardCount]*tempMatcherShard
}

func newTempMatcherManager() *tempMatcherManager {
	tm := &tempMatcherManager{}
	for i := 0; i < tempMatcherShardCount; i++ {
		tm.shards[i] = newTempMatcherShard()
	}
	return tm
}

func (m *tempMatcherManager) getShard(matcher *Matcher) *tempMatcherShard {
	// Simple pointer hashing
	// We use the pointer address to distribute matchers across shards uniformly
	ptr := uintptr(unsafe.Pointer(matcher))
	idx := ptr % tempMatcherShardCount
	return m.shards[idx]
}

// Add adds a temp matcher using insertion sort (O(N)) and sharded lock
func (m *tempMatcherManager) Add(matcher *Matcher) {
	shard := m.getShard(matcher)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Add to index with Insertion Sort
	list := shard.matcherIndex[matcher.EventType]
	priority := matcher.getPriority()

	// Find insertion point to maintain stable sort (insert after items with <= priority)
	// We want list[i] <= list[i+1]. Stable: if equal, new one comes after old ones.
	// So we need to find first index where list[i].priority > priority.

	// Optimization: Check if it should be appended (common case, appending default priority)
	insertIdx := len(list)
	if len(list) > 0 && list[len(list)-1].getPriority() <= priority {
		// Appending is O(1)
	} else {
		// Binary search for insertion point
		insertIdx = sort.Search(len(list), func(i int) bool {
			return list[i].getPriority() > priority
		})
	}

	// Insert
	if insertIdx == len(list) {
		list = append(list, matcher)
	} else {
		// Grow capacity
		list = append(list, nil)
		// Shift elements to the right
		copy(list[insertIdx+1:], list[insertIdx:])
		list[insertIdx] = matcher
	}
	shard.matcherIndex[matcher.EventType] = list

	// Add to heap if it has expiration
	if !matcher.rt.expiresAt.IsZero() {
		heap.Push(shard.expiration, matcher)
	}

	shard.byID[matcher] = struct{}{}
}

// Count returns the number of temporary matchers
func (m *tempMatcherManager) Count() int {
	count := 0
	for i := 0; i < tempMatcherShardCount; i++ {
		shard := m.shards[i]
		shard.mu.RLock()
		count += len(shard.byID)
		shard.mu.RUnlock()
	}
	return count
}

// Remove removes a temp matcher
func (m *tempMatcherManager) Remove(matcher *Matcher) {
	shard := m.getShard(matcher)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	m.removeLocked(shard, matcher)
}

// removeLocked removes matcher assuming lock is held
func (m *tempMatcherManager) removeLocked(shard *tempMatcherShard, matcher *Matcher) {
	if _, ok := shard.byID[matcher]; !ok {
		return
	}
	delete(shard.byID, matcher)

	list := shard.matcherIndex[matcher.EventType]
	for i, v := range list {
		if v == matcher {
			// Delete preserving order
			copy(list[i:], list[i+1:])
			list[len(list)-1] = nil
			shard.matcherIndex[matcher.EventType] = list[:len(list)-1]
			break
		}
	}
	// Lazy delete from heap (handled in CleanExpired)
}

// Get returns sorted matchers for an event type
// Consolidates results from all shards
func (m *tempMatcherManager) Get(eventType dto.EventType) []*Matcher {
	// Collect from all shards
	lists := make([][]*Matcher, 0, tempMatcherShardCount)
	totalLen := 0

	// Lock one by one and copy list to avoid holding all locks
	for i := 0; i < tempMatcherShardCount; i++ {
		shard := m.shards[i]
		shard.mu.RLock()
		src := shard.matcherIndex[eventType]
		if len(src) > 0 {
			// Copy to avoid race after unlock
			dst := make([]*Matcher, len(src))
			copy(dst, src)
			lists = append(lists, dst)
			totalLen += len(src)
		}
		shard.mu.RUnlock()
	}

	if totalLen == 0 {
		return nil
	}

	// Merge K sorted lists
	return mergeKLists(lists, totalLen)
}

// mergeKLists merges multiple sorted matcher lists into one
func mergeKLists(lists [][]*Matcher, totalLen int) []*Matcher {
	res := make([]*Matcher, 0, totalLen)
	indices := make([]int, len(lists))

	// Since K is small (8), linear scan for min is efficient enough
	for {
		minP := uint(999999999)
		winner := -1

		for k, list := range lists {
			if indices[k] < len(list) {
				p := list[indices[k]].getPriority()
				// Stable sort: if priorities are equal, we should strictly speaking preserve order based on... shards?
				// Since shards are random, order between shards is arbitrary but consistent.
				// We pick the first one encountered (lowest k).
				if p < minP {
					minP = p
					winner = k
				}
			}
		}

		if winner == -1 {
			break
		}

		res = append(res, lists[winner][indices[winner]])
		indices[winner]++
	}
	return res
}

// CleanExpired removes expired matchers and returns them
func (m *tempMatcherManager) CleanExpired() []*Matcher {
	var expired []*Matcher
	now := time.Now()

	// Iterate all shards
	for i := 0; i < tempMatcherShardCount; i++ {
		shard := m.shards[i]
		shard.mu.Lock()

		for shard.expiration.Len() > 0 {
			// Peek first
			matcher := (*shard.expiration)[0]

			// If expired or deleted
			if matcher.rt.deleted || (!matcher.rt.expiresAt.IsZero() && now.After(matcher.rt.expiresAt)) {
				heap.Pop(shard.expiration)

				// Verify it'services still in this shard and in index before removal
				// (Handling race where it might have been removed concurrently?
				// Lock protects us, but deleted flag might be set by Remove)
				if _, ok := shard.byID[matcher]; ok {
					m.removeLocked(shard, matcher)
					expired = append(expired, matcher)
				}
			} else {
				break
			}
		}
		shard.mu.Unlock()
	}
	return expired
}

// matcherHeap implements heap.Interface
type matcherHeap []*Matcher

func (h matcherHeap) Len() int { return len(h) }
func (h matcherHeap) Less(i, j int) bool {
	// Min-heap based on expiresAt
	return h[i].rt.expiresAt.Before(h[j].rt.expiresAt)
}
func (h matcherHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *matcherHeap) Push(x interface{}) {
	*h = append(*h, x.(*Matcher))
}

func (h *matcherHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}
