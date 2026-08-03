package engine

// merge_iter.go — 惰性 K 路归并迭代器
//
// 替代 mergeKSortedMatchers + priorityHeap 的组合。
// 核心差异：
//
//   - 零 alloc：不产生合并后的 []*Matcher 大切片
//   - 惰性归并：消费到哪归并到哪，支持 isBlocking 提前终止
//   - 内联 K 路线性扫描：K≤16 时 N 次 K 路比较通常快于
//     heap 的 siftUp/siftDown + interface{} 装箱
//
// 归并算法实现细节：由 RoutingStrategy.Plan 驱动，执行层不直接接触。
// 相关设计：docs/notes/25-routing-strategy.md
//
// 用法：
//
//	it := acquireMergeIter()
//	defer releaseMergeIter(it)
//	it.add(l1)
//	it.add(l2)
//	for it.Next() {
//	    m := it.Matcher()
//	    // ...
//	}
//
// K 上限 mergeIterMaxStreams 与 Source Budget 的关系：
// 归并输入规模 K_act 由 add() 跳过空列表自然衰减（非命令事件时命令索引输出 0 流），
// 但架构预算 K_reg（注册索引数）不允许超过 16——超限首先意味着路由设计
// 需要重新审视（职责过碎），其次才意味着需要考虑 heap 归并（见 25 D4）。

import (
	"sync"
)

// mergeIterMaxStreams 是 MergeIter 支持的有序流数量上限。
// 对应 Source Budget 的硬上限：每个索引最多贡献 2 路（specific/generic），
// 16 路 ≈ 8 个索引，正落在"推荐 K_reg ≤ 8"的预算内。
const mergeIterMaxStreams = 16

// matcherMergeIter 对最多 16 个已按优先级升序排列的 Matcher 列表进行惰性合并。
//
// 每流可携带与列表 1:1 对齐的 Meta 数组（hasMeta 标记），
// 供慢带索引（regexIndex 捕获组）随候选传递匹配结果；
// 快带流不携带 Meta（hasMeta=false），与常规路径完全一致、零成本。
type matcherMergeIter struct {
	lists   [mergeIterMaxStreams][]*Matcher
	metas   [mergeIterMaxStreams][]any
	hasMeta [mergeIterMaxStreams]bool
	n       int
	idx     [mergeIterMaxStreams]int
	cur     *Matcher
	curMeta any
}

// add 追加一个有序候选流（无 Meta）。空列表直接跳过（K_act 自然衰减）。
func (it *matcherMergeIter) add(list []*Matcher) {
	if len(list) > 0 && it.n < mergeIterMaxStreams {
		it.lists[it.n] = list
		it.hasMeta[it.n] = false // 覆写旧槽位的 meta 标记（池化复用）
		it.n++
	}
}

// addMeta 追加一个携带逐条 Meta 的有序候选流（metas 与 list 1:1 对齐）。
func (it *matcherMergeIter) addMeta(list []*Matcher, metas []any) {
	if len(list) > 0 && len(metas) == len(list) && it.n < mergeIterMaxStreams {
		it.lists[it.n] = list
		it.metas[it.n] = metas
		it.hasMeta[it.n] = true
		it.n++
	}
}

// Next 前进到下一个优先级最低的 Matcher。
// 返回 false 表示所有列表均已遍历完毕。
//
// 用 bestIdx==-1 判定首个候选而非 MaxUint 哨兵：
// 哨兵写法会让优先级恰为 MaxUint 的 matcher 被跳过并提前终止整个归并。
func (it *matcherMergeIter) Next() bool {
	bestIdx := -1
	var bestPrio uint64

	for i := range it.n {
		if it.idx[i] < len(it.lists[i]) {
			p := it.lists[i][it.idx[i]].getPriority()
			if bestIdx == -1 || p < bestPrio {
				bestPrio = p
				bestIdx = i
			}
		}
	}
	if bestIdx == -1 {
		it.cur = nil
		it.curMeta = nil
		return false
	}

	it.cur = it.lists[bestIdx][it.idx[bestIdx]]
	if it.hasMeta[bestIdx] {
		it.curMeta = it.metas[bestIdx][it.idx[bestIdx]]
	} else {
		it.curMeta = nil
	}
	it.idx[bestIdx]++
	return true
}

// Matcher 返回当前 Matcher（Next 后有效）。
func (it *matcherMergeIter) Matcher() *Matcher { return it.cur }

// Meta 返回当前 Matcher 携带的匹配结果 Meta（Next 后有效；无 Meta 时为 nil）。
func (it *matcherMergeIter) Meta() any { return it.curMeta }

var mergeIterPool sync.Pool

// acquireMergeIter 从池中获取并初始化一个 MergeIter。
// 零 alloc 路径：池中已有对象时仅重置游标（idx/n/cur/curMeta），
// lists/metas/hasMeta 槽位由 add/addMeta 覆写、Next 只读 < n，无需清零
// （热路径每事件省去 ~500B 的数组 memset）。
func acquireMergeIter() *matcherMergeIter {
	v := mergeIterPool.Get()
	if v != nil {
		it := v.(*matcherMergeIter)
		it.cur = nil
		it.curMeta = nil
		it.n = 0
		it.idx = [mergeIterMaxStreams]int{}
		return it
	}
	return &matcherMergeIter{}
}

// releaseMergeIter 将 MergeIter 归还池中。
// 仅清除已使用槽位的引用（GC 卫生：释放对逐事件过滤切片的持有），
// 其余数组由 acquire/add 覆写，无需全量清零。
func releaseMergeIter(it *matcherMergeIter) {
	for i := range it.n {
		it.lists[i] = nil
		it.metas[i] = nil
		it.hasMeta[i] = false
	}
	it.n = 0
	it.cur = nil
	it.curMeta = nil
	mergeIterPool.Put(it)
}
