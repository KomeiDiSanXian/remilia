// Package trie 提供泛型前缀树（Trie）与 Aho-Corasick 自动机。
//
// 适用于高效的前缀匹配（命令补全）和多关键词子串匹配（敏感词过滤）。
package trie

import (
	"sync"
	"sync/atomic"
)

// Node 前缀树节点。
type Node[V comparable] struct {
	children map[rune]*Node[V]
	values   []V
	isEnd    bool
	fail     *Node[V] // AC 自动机失败指针（未构建时为 nil）
}

// Trie 是一棵泛型前缀树。
type Trie[V comparable] struct {
	root  *Node[V]
	mu    sync.RWMutex
	built atomic.Bool
}

// New 创建一棵空的前缀树。
func New[V comparable]() *Trie[V] {
	return &Trie[V]{
		root: &Node[V]{
			children: make(map[rune]*Node[V]),
		},
	}
}

// Insert 插入一个键值对。相同的 key 可关联多个值。
func (t *Trie[V]) Insert(key string, val V) {
	t.mu.Lock()
	defer t.mu.Unlock()

	node := t.root
	for _, r := range key {
		if node.children[r] == nil {
			node.children[r] = &Node[V]{
				children: make(map[rune]*Node[V]),
			}
		}
		node = node.children[r]
	}

	node.values = append(node.values, val)
	node.isEnd = true
	t.built.Store(false)
}

// Remove 从指定 key 的节点中移除一个值。值不存在时为空操作。
func (t *Trie[V]) Remove(key string, val V) {
	t.mu.Lock()
	defer t.mu.Unlock()

	node := t.root
	runes := []rune(key)
	path := make([]*Node[V], 0, len(runes)+1)
	path = append(path, node)

	for _, r := range runes {
		if node.children[r] == nil {
			return
		}
		node = node.children[r]
		path = append(path, node)
	}

	node.values = removeFromSlice(node.values, val)
	if len(node.values) == 0 {
		node.isEnd = false
	}

	for i := len(path) - 1; i > 0; i-- {
		n := path[i]
		if !n.isEnd && len(n.children) == 0 && len(n.values) == 0 {
			parent := path[i-1]
			delete(parent.children, runes[i-1])
		} else {
			break
		}
	}
	t.built.Store(false)
}

// BuildAC 构建 Aho-Corasick 失败指针。通常无需手动调用，SearchAC 会自动构建。
func (t *Trie[V]) BuildAC() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buildACLocked()
}

func (t *Trie[V]) buildACLocked() {
	if t.built.Load() {
		return
	}

	t.root.fail = t.root
	queue := make([]*Node[V], 0)

	for _, child := range t.root.children {
		child.fail = t.root
		queue = append(queue, child)
	}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]

		for r, child := range node.children {
			fail := node.fail
			for fail != t.root && fail.children[r] == nil {
				fail = fail.fail
			}
			if fail.children[r] != nil && fail.children[r] != child {
				child.fail = fail.children[r]
			} else {
				child.fail = t.root
			}
			queue = append(queue, child)
		}
	}
	t.built.Store(true)
}

// Search 在文本中搜索所有已注册的模式（子串匹配）。若 AC 自动机尚未构建则自动构建。
//
// 返回去重后的匹配值列表。
func (t *Trie[V]) Search(text string) []V {
	if !t.built.Load() {
		t.mu.Lock()
		t.buildACLocked()
		t.mu.Unlock()
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	node := t.root
	seen := make(map[V]bool)
	result := make([]V, 0)

	for _, r := range text {
		for node != t.root && node.children[r] == nil {
			node = node.fail
		}
		if node.children[r] != nil {
			node = node.children[r]
		}

		for temp := node; temp != t.root; temp = temp.fail {
			if temp.isEnd {
				for _, v := range temp.values {
					if !seen[v] {
						seen[v] = true
						result = append(result, v)
					}
				}
			}
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// SearchPrefix 查找所有具有给定前缀的值。
func (t *Trie[V]) SearchPrefix(prefix string) []V {
	t.mu.RLock()
	defer t.mu.RUnlock()

	node := t.root
	for _, r := range prefix {
		if node.children[r] == nil {
			return nil
		}
		node = node.children[r]
	}

	var result []V
	seen := make(map[V]bool)
	collectTerminalValues(node, seen, &result)
	if len(result) == 0 {
		return nil
	}
	return result
}

// ExactMatch 精确匹配 key 对应的值。找到时返回该值和 true；否则返回零值和 false。
func (t *Trie[V]) ExactMatch(key string) (V, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	node := t.root
	for _, r := range key {
		if node.children[r] == nil {
			var zero V
			return zero, false
		}
		node = node.children[r]
	}

	if !node.isEnd || len(node.values) == 0 {
		var zero V
		return zero, false
	}

	return node.values[0], true
}

// GetAll 返回树中所有值（不排序）。
func (t *Trie[V]) GetAll() []V {
	t.mu.RLock()
	defer t.mu.RUnlock()

	seen := make(map[V]bool)
	result := make([]V, 0)
	collectAllValues(t.root, seen, &result)
	return result
}

// Clear 清空前缀树。
func (t *Trie[V]) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.root = &Node[V]{
		children: make(map[rune]*Node[V]),
	}
	t.built.Store(false)
}

// Stats 返回前缀树的统计信息。
func (t *Trie[V]) Stats() Stats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	s := Stats{}
	collectStats(t.root, 0, &s)
	return s
}

// Stats 前缀树统计信息。
type Stats struct {
	NodeCount  int
	MaxDepth   int
	TotalEdges int
}

func collectTerminalValues[V comparable](node *Node[V], seen map[V]bool, result *[]V) {
	if node == nil {
		return
	}
	if node.isEnd {
		for _, v := range node.values {
			if !seen[v] {
				seen[v] = true
				*result = append(*result, v)
			}
		}
	}
	for _, child := range node.children {
		collectTerminalValues(child, seen, result)
	}
}

func collectAllValues[V comparable](node *Node[V], seen map[V]bool, result *[]V) {
	if node == nil {
		return
	}
	for _, v := range node.values {
		if !seen[v] && node.isEnd {
			seen[v] = true
			*result = append(*result, v)
		}
	}
	for _, child := range node.children {
		collectAllValues(child, seen, result)
	}
}

func collectStats[V comparable](node *Node[V], depth int, s *Stats) {
	if node == nil {
		return
	}
	s.NodeCount++
	if depth > s.MaxDepth {
		s.MaxDepth = depth
	}
	s.TotalEdges += len(node.children)
	for _, child := range node.children {
		collectStats(child, depth+1, s)
	}
}

func removeFromSlice[V comparable](slice []V, val V) []V {
	n := 0
	for _, v := range slice {
		if v != val {
			slice[n] = v
			n++
		}
	}
	var zero V
	for i := n; i < len(slice); i++ {
		slice[i] = zero
	}
	return slice[:n]
}
