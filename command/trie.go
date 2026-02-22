package command

import (
	"sort"
	"sync"
)

// TrieNode represents a node in the prefix tree
type TrieNode struct {
	children map[rune]*TrieNode
	commands []*Meta
	isEnd    bool
}

// Trie is a prefix tree for efficient command lookup and completion
type Trie struct {
	root *TrieNode
	mu   sync.RWMutex
}

// NewTrie creates a new Trie
func NewTrie() *Trie {
	return &Trie{
		root: &TrieNode{
			children: make(map[rune]*TrieNode),
			commands: make([]*Meta, 0),
		},
	}
}

// Insert adds a command to the trie
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
		// Add command to every prefix node for efficient prefix search
		node.commands = append(node.commands, meta)
	}

	node.isEnd = true // Mark this as a complete command
}

// Remove removes a command from the trie
func (t *Trie) Remove(name string, meta *Meta) {
	t.mu.Lock()
	defer t.mu.Unlock()

	node := t.root
	runes := []rune(name)
	path := make([]*TrieNode, 0, len(runes)+1)

	// Navigate to the end node and collect path
	for _, r := range runes {
		if node.children[r] == nil {
			return // Not found
		}
		node = node.children[r]
		path = append(path, node)
	}

	// Remove command from all nodes in the path
	for _, n := range path {
		n.commands = removeCommandFromSlice(n.commands, meta)
	}

	node.isEnd = false
}

// Search finds all commands with the given prefix
func (t *Trie) Search(prefix string) []*Meta {
	t.mu.RLock()
	defer t.mu.RUnlock()

	node := t.root
	runes := []rune(prefix)

	for _, r := range runes {
		if node.children[r] == nil {
			return nil // No match
		}
		node = node.children[r]
	}

	// Return commands at this prefix node
	if len(node.commands) == 0 {
		return nil
	}

	result := make([]*Meta, len(node.commands))
	copy(result, node.commands)
	return result
}

// ExactMatch finds a command by exact name match
// Returns the command metadata if found, nil otherwise
// Time complexity: O(m) where m is the length of the command name
func (t *Trie) ExactMatch(name string) *Meta {
	t.mu.RLock()
	defer t.mu.RUnlock()

	node := t.root
	runes := []rune(name)

	// Navigate to the end of the command name
	for _, r := range runes {
		if node.children[r] == nil {
			return nil // No match
		}
		node = node.children[r]
	}

	// Check if this is a complete command (not just a prefix)
	if !node.isEnd || len(node.commands) == 0 {
		return nil
	}

	// Return the first command (should only be one for exact match)
	// The commands slice at an end node should contain the command itself
	for _, cmd := range node.commands {
		if cmd.Name == name {
			return cmd
		}
	}

	return nil
}

// collectCommands recursively collects all commands from a node and its children
func (t *Trie) collectCommands(node *TrieNode, seen map[*Meta]bool, result *[]*Meta) {
	if node == nil {
		return
	}

	// Add commands from this node if it's an end node
	if node.isEnd {
		for _, cmd := range node.commands {
			if !seen[cmd] {
				seen[cmd] = true
				*result = append(*result, cmd)
			}
		}
	}

	// Recursively collect from children
	for _, child := range node.children {
		t.collectCommands(child, seen, result)
	}
}

// Clear removes all commands from the trie
func (t *Trie) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.root = &TrieNode{
		children: make(map[rune]*TrieNode),
		commands: make([]*Meta, 0),
	}
}

// GetStats returns statistics about the trie
func (t *Trie) GetStats() TrieStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := TrieStats{}
	t.collectStats(t.root, 0, &stats)
	return stats
}

// TrieStats contains statistics about the trie
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

// removeCommandFromSlice removes a command from a slice
func removeCommandFromSlice(slice []*Meta, meta *Meta) []*Meta {
	result := make([]*Meta, 0, len(slice))
	for _, cmd := range slice {
		if cmd != meta {
			result = append(result, cmd)
		}
	}
	return result
}

// GetAllCommands returns all commands in the trie (sorted by priority)
func (t *Trie) GetAllCommands() []*Meta {
	t.mu.RLock()
	defer t.mu.RUnlock()

	seen := make(map[*Meta]bool)
	result := make([]*Meta, 0)

	t.collectAllCommands(t.root, seen, &result)

	// Sort by priority
	sort.Slice(result, func(i, j int) bool {
		return result[i].Priority > result[j].Priority
	})

	return result
}

func (t *Trie) collectAllCommands(node *TrieNode, seen map[*Meta]bool, result *[]*Meta) {
	if node == nil {
		return
	}

	// Add commands from this node
	for _, cmd := range node.commands {
		if !seen[cmd] && node.isEnd {
			seen[cmd] = true
			*result = append(*result, cmd)
		}
	}

	// Recursively collect from children
	for _, child := range node.children {
		t.collectAllCommands(child, seen, result)
	}
}
