# 泛型 Trie 与 Aho-Corasick 自动机

> 源码：[`infra/trie/trie.go`](../../infra/trie/trie.go)

## 目录

1. [为什么需要 Trie](#1-为什么需要-trie)
2. [基础 Trie 实现](#2-基础-trie-实现)
3. [Aho-Corasick 自动机](#3-aho-corasick-自动机)
4. [泛型设计](#4-泛型设计)
5. [并发安全](#5-并发安全)
6. [为什么不用双数组 Trie](#6-为什么不用双数组-trie)
7. [为什么不用 Bloom Filter](#7-为什么不用-bloom-filter)
8. [性能特征](#8-性能特征)
9. [在 Remilia 中的应用](#9-在-remilia-中的应用)

---

## 1. 为什么需要 Trie

### 问题

在 Remilia 中，有两类核心的前缀/匹配需求：

| 场景 | 输入 | 输出 | 说明 |
|------|------|------|------|
| **命令补全** | 用户输入 `/hel` | 匹配到 `/help`、`/hello` 等 | 前缀匹配 |
| **敏感词过滤** | 消息文本 | 匹配所有出现的敏感词 | 子串匹配 |

最朴素的实现是线性扫描：

```go
// 前缀匹配 — O(n*m)
for _, cmd := range allCommands {
    if strings.HasPrefix(cmd.Name, prefix) {
        results = append(results, cmd)
    }
}

// 子串匹配 — O(n*m)
for _, kw := range keywords {
    if strings.Contains(text, kw) {
        return kw
    }
}
```

当命令数量 > 100 或关键词 > 1000 时，线性扫描成为瓶颈。Trie（前缀树）将匹配复杂度从 `O(n*m)` 降至 `O(m)`。

### 为什么不直接用 map

Go 的 `map[string]T` 支持 O(1) 精确查找，但对**前缀匹配**和**子串匹配**无能为力——你需要遍历所有 key 做 `strings.HasPrefix`。Trie 的树状结构天然支持前缀遍历。

### 为什么不排序 + 二分

排序数组 + `sort.Search` 可以做前缀匹配（找 `prefix` 和 `prefix~` 之间的区间），但**动态增删**需要 O(n) 插入/删除。Trie 的插入/删除是 O(m)，不受已有数据量影响。

---

## 2. 基础 Trie 实现

### 数据结构

```go
type Node[V comparable] struct {
    children map[rune]*Node[V]  // 子节点（rune = Unicode 码点）
    values   []V                // 该节点关联的值
    isEnd    bool               // 是否有值在此节点结束
    fail     *Node[V]           // AC 失败指针（构建后非 nil）
}

type Trie[V comparable] struct {
    root  *Node[V]
    mu    sync.RWMutex
    built atomic.Bool
}
```

**为什么 children 用 `map[rune]*Node` 而不是 `[26]*Node`（数组）或 `map[byte]*Node`？**

- `[26]*Node` 仅支持英文小写字母，过滤中文、标点等 Unicode 字符无法处理
- `map[byte]*Node` 只能处理单字节，对中文等多字节字符需要调用方先编码
- `map[rune]*Node` 天然支持 Unicode，无论中英文、Emoji 都能正确处理，调用方直接传 `string` 即可

### Insert

```go
func (t *Trie[V]) Insert(key string, val V) {
    node := t.root
    for _, r := range key {          // 按 rune 逐字符遍历
        if node.children[r] == nil {
            node.children[r] = &Node[V]{
                children: make(map[rune]*Node[V]),
            }
        }
        node = node.children[r]
    }
    node.values = append(node.values, val)
    node.isEnd = true
}
```

**设计决策：一个 key 可以关联多个 value**

命令系统中，同一命令名可能注册了多个 Handler（不同事件类型如私聊/群聊）。允许 `values []V` 而非单值 `V`，多个值在同一节点累积。去重由上层负责（`Search`/`GetAll` 用 `seen map[V]bool`）。

### SearchPrefix（前缀匹配）

```go
func (t *Trie[V]) SearchPrefix(prefix string) []V {
    node := t.root
    for _, r := range prefix {
        if node.children[r] == nil {
            return nil  // 前缀不存在
        }
        node = node.children[r]
    }
    // DFS 收集该子树下所有终端节点的值
    var result []V
    seen := make(map[V]bool)
    collectTerminalValues(node, seen, &result)
    return result
}
```

算法：先沿前缀走到对应节点，然后 DFS 遍历该子树，收集所有 `isEnd == true` 的节点上的 values。

### ExactMatch（精确匹配）

```go
func (t *Trie[V]) ExactMatch(key string) (V, bool) {
    node := t.root
    for _, r := range key {
        if node.children[r] == nil {
            var zero V
            return zero, false
        }
        node = node.children[r]
    }
    if !node.isEnd {
        var zero V
        return zero, false
    }
    return node.values[0], true
}
```

返回 `(V, bool)` 而非 `*V` + nil 检查，是 Go 泛型中标准的安全查询模式，避免 zero value 与 "不存在" 的歧义。

### Remove

```go
func (t *Trie[V]) Remove(key string, val V) {
    // 1. 沿 key 走到叶节点
    // 2. 从 values 切片中移除指定 val
    // 3. 若 values 为空，清除 isEnd 标记
    // 4. 从叶到根修剪空节点（无 children、无 values、非 isEnd）
}
```

**修剪（pruning）** 很重要：删除后不修剪，节点会泄漏，导致内存膨胀和错误的 SearchPrefix 结果。修剪从叶到根，遇到第一个非空节点停止。

---

## 3. Aho-Corasick 自动机

### 为什么需要 AC

基础 Trie 只能做前缀匹配（`strings.HasPrefix`），无法做子串匹配（`strings.Contains`）。对于敏感词过滤，我们需要在文本中找出所有出现的关键词——无论它们在什么位置。

最朴素的做法：对每个关键词调用 `strings.Contains(text, kw)`，复杂度 O(m·n)。

AC 自动机的核心思想：**在 Trie 上加失败指针（fail link），匹配失败时沿 fail 链跳转，避免回溯**。复杂度 O(m + n + z)，其中 z 是匹配数量。

### 失败指针（Failure Link）

**定义**：节点 `u` 的 fail 指针指向 Trie 中另一个节点 `v`，满足 `v` 对应的字符串是 `u` 对应字符串的**最长真后缀**。

**示例**：插入 `she`、`he`、`his`、`hers` 后：

```
         root
        /    \
       h      s
      / \      \
     e   i      h
    /     \      \
   r*      s*     e*
   |
   s*
```

节点 `e(he)` 的 fail = root（"e" 的后缀没有其他完整关键词）。
节点 `r(her)` 的 fail = `e(he)`（"er" 的后缀中，"e" 匹配）。
节点 `s(hers)` 的 fail = `s(he)`（"ers" → "ers"、"rs"、"s" → "s" 匹配）。

### 构建算法（BFS）

```
BuildAC():
    root.fail = root
    queue = [root 的所有子节点]
    for each node in queue:
        for each child (r, childNode) of node:
            fail = node.fail
            while fail ≠ root AND fail.children[r] == nil:
                fail = fail.fail
            if fail.children[r] ≠ nil AND fail.children[r] ≠ childNode:
                childNode.fail = fail.children[r]
            else:
                childNode.fail = root
            queue ← childNode
```

BFS 保证当处理节点 `n` 时，`n` 的 fail 已经计算完成。这是因为 fail 指针总是指向更短的字符串，而 BFS 按层遍历，短字符串的节点总是先被访问。

### 匹配算法

```
Search(text):
    node = root
    for each r in text:
        while node ≠ root AND node.children[r] == nil:
            node = node.fail         // 沿 fail 链回溯
        if node.children[r] ≠ nil:
            node = node.children[r]
        // 沿 fail 链收集所有匹配
        for temp = node; temp ≠ root; temp = temp.fail:
            if temp.isEnd:
                collect temp.values
```

**关键点**：每次遇到一个字符，如果当前节点没有该字符的子节点，不是回到 root 重新匹配，而是沿 fail 链跳转。这保证了 O(m) 的扫描复杂度。

**为什么不用 output link（字典后缀链接）优化**？

output link 是 fail 链中跳过非终端节点的优化，直接指向下一个 `isEnd` 节点。当前实现直接遍历 fail 链，对于关键词数量 < 10000 的场景性能足够。若未来 profiling 证明 fail 链遍历是瓶颈，可以加上 output link 优化。

### 自动构建（Lazy Build）

```go
func (t *Trie[V]) Search(text string) []V {
    if !t.built.Load() {             // 原子读，无需锁
        t.mu.Lock()
        t.buildACLocked()            // 构建 fail 链
        t.mu.Unlock()
    }
    t.mu.RLock()
    defer t.mu.RUnlock()
    return t.searchLocked(text)
}
```

`Insert`/`Remove`/`Clear` 都会 `built.Store(false)`。Search 首次调用时自动构建，之后直接使用。double-check 模式：先用 `atomic.Load` 快路径，再写锁 + double-check 构建。

---

## 4. 泛型设计

```go
type Trie[V comparable] struct { ... }
```

为什么 `V` 必须是 `comparable`：

| 操作 | 为什么需要 comparable |
|------|----------------------|
| `Remove(key, val V)` | 需要 `v != val` 过滤切片 |
| `Search` 去重 | 需要 `seen map[V]bool` |
| `GetAll` 去重 | 同上 |

`V` 的实际类型：`*Meta`（命令系统）、`string`（关键词过滤）。两者都是 comparable。

为什么不把 `K`（key 类型）也泛型化：

当前 `children` 是 `map[rune]*Node[V]`。如果做成 `map[K]*Node[K, V]`，调用方必须 `Insert([]K("text"), val)`——常见场景（字符串）反而变得更复杂。`rune` 天然处理 Unicode，是目前所有场景的最优解。

---

## 5. 并发安全

`sync.RWMutex` + `atomic.Bool`：

| 方法 | 锁 | 说明 |
|------|----|------|
| `Insert` | `Lock` | 修改树结构 |
| `Remove` | `Lock` | 修改树结构 |
| `Clear` | `Lock` | 替换 root |
| `Search` | 无锁 → `Lock` → `RLock` | 先 atomic 检查 built，需构建则 Lock，搜索用 RLock |
| `SearchPrefix` | `RLock` | 只读遍历 |
| `ExactMatch` | `RLock` | 只读遍历 |
| `GetAll` | `RLock` | 只读遍历 |
| `Stats` | `RLock` | 只读遍历 |

**读写锁的选择**：搜索是主要负载（读多写少），RLock 允许多个 Search 并发执行。Insert/Remove 频率低，短时间 Lock 不会成为瓶颈。

**删除重置 built**：`Remove` 设置 `built = false`，因为修剪（pruning）可能改变节点拓扑，原有的 fail 链可能指向已移除的节点。

---

## 6. 为什么不用双数组 Trie

双数组 Trie（Double-Array Trie）用两个平行数组 `BASE` 和 `CHECK` 代替 `map[rune]*Node`：

```
// 从节点 s 读取字符 c 的下一个节点
next = BASE[s] + c
if CHECK[next] == s:
    return next  // 匹配成功
```

**优点**：紧凑的内存布局（两个 int 数组）、缓存友好、查找快（数组索引 vs map 哈希）。

**不采用的原因**：

| 考量 | map[rune]*Node | 双数组 |
|------|----------------|--------|
| 动态插入 | O(m)，天然支持 | **可能需要大范围重建 BASE 数组** |
| 删除 | O(m)，天然支持 | **同样需要重建** |
| 命令热重载 | 频繁增删，树结构动态变化 | 不可行 |
| 关键词动态增删 | 运行时 AddKeyword | 不可行 |
| 节点数量 | < 10000，map 足够快 | 优势不明显 |

**结论**：双数组适用于"构建一次、查询多次"的静态场景（如分词词典）。Remilia 的 Trie 需要动态增删（命令热重载、运行时增删关键词），`map[rune]*Node` 是正确选择。若未来 profiling 证明 map 是瓶颈，可提供 `ArrayTrie` 作为可选实现。

---

## 7. 为什么不用 Bloom Filter

Bloom Filter 的价值："某个元素**一定不存在**于集合中"，用于跳过昂贵的查询。

对 Trie 不需要的原因：

| 操作 | 复杂度 | Bloom Filter 能否加速 |
|------|--------|----------------------|
| `SearchPrefix` | O(\|prefix\|) | 不能——已经是最快路径 |
| `Search(text)` | O(\|text\|) | **不能**——必须扫描全文，无法用 BF 跳过 |
| `ExactMatch` | O(\|key\|) | 能——但 map 已经 O(1) |

对于 `Search(text)`（AC 子串匹配），不能把整段文本当成一个 key 去查 BF——文本不等同于关键词。需要逐字符位置检查子串，这等价于 AC 扫描本身。

**Bloom Filter 只在底层存储不在内存时有用**（如磁盘 KV、远端数据库）。Trie 全在内存，`map[rune]*Node` 是 O(1) 哈希表访问，加 BF 徒增复杂度。

---

## 8. 性能特征

| 操作 | 平均复杂度 | 最坏 |
|------|-----------|------|
| `Insert(key, val)` | O(\|key\|) | O(\|key\|) |
| `Remove(key, val)` | O(\|key\| + 修剪) | O(\|key\|) |
| `ExactMatch(key)` | O(\|key\|) | O(\|key\|) |
| `SearchPrefix(p)` | O(\|p\| + 子树大小) | O(n) |
| `Search(text)` | O(\|text\| + z) | O(\|text\| + z) |
| `BuildAC` | O(总字符数) | O(总字符数) |

其中 z = 匹配数量（AC 收集时需要遍历 fail 链）。

### 内存

每个节点：`map[rune]*Node`（平均 ~8 个键值对）+ `values []V` + 两个 bool + fail 指针。

经验值：1000 个英文命令约占用 ~200KB，10000 个中文关键词约 ~5MB。远低于 Go 进程的典型内存限制。

---

## 9. 在 Remilia 中的应用

### 命令补全（`command` 包）

```go
// command/trie.go — 类型别名，零开销桥接
type Trie       = trie.Trie[*Meta]
type TrieStats  = trie.Stats
func NewTrie()  = trie.New[*Meta]()

// command/registry.go
func (cr *Registry) Complete(prefix string) []*Meta {
    return cr.trie.SearchPrefix(prefix)
}
```

### 敏感词过滤（待接入）

```go
// 将来 keywordfilter 可以这样实现：
t := trie.New[string]()
for _, kw := range keywords {
    t.Insert(kw, kw)
}

// Check 方法从 O(n*m) 降到 O(m)
func (p *Plugin) Check(text string) string {
    matches := t.Search(text)
    if len(matches) > 0 {
        return matches[0]
    }
    return ""
}
```

### 插件状态持久化（`kv` 包）

`infra/kv` 的 LevelDB 封装提供了 KV 存储，`pluginstore` 用它替代 JSON 文件读写——这是存储层的 KV 用法，与 Trie 无关，但共享了"用 LevelDB 做 KV 比 JSON 文件更高效"的设计理念。
