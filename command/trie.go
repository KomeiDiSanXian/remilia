package command

import (
	"github.com/KomeiDiSanXian/remilia/infra/trie"
)

// Trie 面向命令的专用前缀树，基于泛型 trie.Trie[*Meta]。
type Trie = trie.Trie[*Meta]

// NewTrie 创建一棵新的命令前缀树。
func NewTrie() *Trie {
	return trie.New[*Meta]()
}

// TrieStats 前缀树统计信息，迁移至 infra/trie。
type TrieStats = trie.Stats
