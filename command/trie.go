package command

import (
	"sort"
	"sync"
)

// TrieNode 表示前缀树中的一个节点
type TrieNode struct {
	children map[rune]*TrieNode
	commands []*Meta
	isEnd    bool
}

// Trie 是一棵前缀树，用于高效的命令查找和补全
type Trie struct {
	root *TrieNode
	mu   sync.RWMutex
}

// NewTrie 创建一棵新的前缀树
func NewTrie() *Trie {
	return &Trie{
		root: &TrieNode{
			children: make(map[rune]*TrieNode),
			commands: make([]*Meta, 0),
		},
	}
}

// Insert 向前缀树中插入一个命令
func (t *Trie) Insert(name string, meta *Meta) {
	t.mu.Lock()
	defer t.mu.Unlock()

	node := t.root
	runes := []rune(name)

	for _, r := range runes {
		if node.children[r] == nil {
			node.children[r] = &TrieNode{
				children: make(map[rune]*TrieNode),
				commands: make([]*Meta, 0),
			}
		}
		node = node.children[r]
	}

	node.commands = append(node.commands, meta)
	node.isEnd = true
}

// Remove 从前缀树中删除一个命令
func (t *Trie) Remove(name string, meta *Meta) {
	t.mu.Lock()
	defer t.mu.Unlock()

	node := t.root
	runes := []rune(name)
	path := make([]*TrieNode, 0, len(runes)+1)
	path = append(path, node)

	for _, r := range runes {
		if node.children[r] == nil {
			return
		}
		node = node.children[r]
		path = append(path, node)
	}

	// 仅从终端节点移除命令
	node.commands = removeCommandFromSlice(node.commands, meta)

	if len(node.commands) == 0 {
		node.isEnd = false
	}

	// 从叶节点向根节点修剪空节点
	for i := len(path) - 1; i > 0; i-- {
		n := path[i]
		if !n.isEnd && len(n.children) == 0 && len(n.commands) == 0 {
			parent := path[i-1]
			delete(parent.children, runes[i-1])
		} else {
			break
		}
	}
}

// Search 查找所有具有给定前缀的命令
func (t *Trie) Search(prefix string) []*Meta {
	t.mu.RLock()
	defer t.mu.RUnlock()

	node := t.root
	runes := []rune(prefix)

	for _, r := range runes {
		if node.children[r] == nil {
			return nil
		}
		node = node.children[r]
	}

	// DFS 收集该前缀下所有终端节点的命令
	var result []*Meta
	seen := make(map[*Meta]bool)
	t.collectTerminalCommands(node, seen, &result)
	if len(result) == 0 {
		return nil
	}
	return result
}

// collectTerminalCommands DFS 收集所有终端节点的命令
func (t *Trie) collectTerminalCommands(node *TrieNode, seen map[*Meta]bool, result *[]*Meta) {
	if node == nil {
		return
	}
	if node.isEnd {
		for _, cmd := range node.commands {
			if !seen[cmd] {
				seen[cmd] = true
				*result = append(*result, cmd)
			}
		}
	}
	for _, child := range node.children {
		t.collectTerminalCommands(child, seen, result)
	}
}

// ExactMatch 通过精确名称匹配查找命令
// 找到则返回命令元数据，否则返回 nil
// 时间复杂度：O(m)，m 为命令名称长度
func (t *Trie) ExactMatch(name string) *Meta {
	t.mu.RLock()
	defer t.mu.RUnlock()

	node := t.root
	runes := []rune(name)

	for _, r := range runes {
		if node.children[r] == nil {
			return nil
		}
		node = node.children[r]
	}

	if !node.isEnd || len(node.commands) == 0 {
		return nil
	}

	// 终端节点的第一个命令即为精确匹配
	return node.commands[0]
}

// Clear 清空前缀树中的所有命令
func (t *Trie) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.root = &TrieNode{
		children: make(map[rune]*TrieNode),
		commands: make([]*Meta, 0),
	}
}

// GetStats 返回前缀树的统计信息
func (t *Trie) GetStats() TrieStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := TrieStats{}
	t.collectStats(t.root, 0, &stats)
	return stats
}

// TrieStats 包含前缀树的统计信息
type TrieStats struct {
	NodeCount  int
	MaxDepth   int
	TotalEdges int
}

func (t *Trie) collectStats(node *TrieNode, depth int, stats *TrieStats) {
	if node == nil {
		return
	}

	stats.NodeCount++
	if depth > stats.MaxDepth {
		stats.MaxDepth = depth
	}

	stats.TotalEdges += len(node.children)

	for _, child := range node.children {
		t.collectStats(child, depth+1, stats)
	}
}

// removeCommandFromSlice 从切片中移除指定命令（原地过滤，避免分配）
func removeCommandFromSlice(slice []*Meta, meta *Meta) []*Meta {
	n := 0
	for _, cmd := range slice {
		if cmd != meta {
			slice[n] = cmd
			n++
		}
	}
	for i := n; i < len(slice); i++ {
		slice[i] = nil
	}
	return slice[:n]
}

// GetAllCommands 返回前缀树中的所有命令（按优先级排序）
func (t *Trie) GetAllCommands() []*Meta {
	t.mu.RLock()
	defer t.mu.RUnlock()

	seen := make(map[*Meta]bool)
	result := make([]*Meta, 0)

	t.collectAllCommands(t.root, seen, &result)

	// 按优先级排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].Priority > result[j].Priority
	})

	return result
}

func (t *Trie) collectAllCommands(node *TrieNode, seen map[*Meta]bool, result *[]*Meta) {
	if node == nil {
		return
	}

	// 从当前节点收集命令
	for _, cmd := range node.commands {
		if !seen[cmd] && node.isEnd {
			seen[cmd] = true
			*result = append(*result, cmd)
		}
	}

	// 递归收集子节点的命令
	for _, child := range node.children {
		t.collectAllCommands(child, seen, result)
	}
}
