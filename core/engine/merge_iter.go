package engine

// merge_iter.go — 惰性 K-way 归并迭代器
//
// 替代 mergeKSortedMatchers + priorityHeap 的组合。
// 核心差异：
//
//   - 零 alloc：不产生合并后的 []*Matcher 大切片
//   - 惰性归并：消费到哪归并到哪，支持 isBlocking 提前终止
//   - 内联 6-way 线性扫描：K=6 时 N 次 6 路比较通常快于
//     heap 的 siftUp/siftDown + interface{} 装箱
//
// 用法：
//
//	it := acquireMergeIter(l1, l2, l3, l4, l5, l6)
//	defer releaseMergeIter(it)
//	for it.Next() {
//	    m := it.Matcher()
//	    // ...
//	}

import (
	"math"
	"sync"
)

// mergeIterListCount 是 MergeIter 支持的有序流数量。
// 对应 processEventMatchers 中的 6 个列表：
//
//	permSpecific, cmdSpecific, tempSpecific,
//	permGeneric,  cmdGeneric,  tempGeneric
const mergeIterListCount = 6

// matcherMergeIter 对最多 6 个已按优先级升序排列的 Matcher 列表进行惰性合并。
type matcherMergeIter struct {
	lists [mergeIterListCount][]*Matcher
	idx   [mergeIterListCount]int
	cur   *Matcher
}

// Next 前进到下一个优先级最低的 Matcher。
// 返回 false 表示所有列表均已遍历完毕。
func (it *matcherMergeIter) Next() bool {
	bestIdx := -1
	var bestPrio uint = math.MaxUint

	for i := range mergeIterListCount {
		if it.idx[i] < len(it.lists[i]) {
			p := it.lists[i][it.idx[i]].getPriority()
			if p < bestPrio {
				bestPrio = p
				bestIdx = i
			}
		}
	}
	if bestIdx == -1 {
		it.cur = nil
		return false
	}

	it.cur = it.lists[bestIdx][it.idx[bestIdx]]
	it.idx[bestIdx]++
	return true
}

// Matcher 返回当前 Matcher（Next 后有效）。
func (it *matcherMergeIter) Matcher() *Matcher { return it.cur }

var mergeIterPool sync.Pool

// acquireMergeIter 从池中获取并初始化一个 MergeIter。
// 零 alloc 路径：池中已有对象时仅重置 idx 数组，不触发任何分配。
func acquireMergeIter(l1, l2, l3, l4, l5, l6 []*Matcher) *matcherMergeIter {
	v := mergeIterPool.Get()
	var it *matcherMergeIter
	if v != nil {
		it = v.(*matcherMergeIter)
		it.cur = nil
		it.idx = [mergeIterListCount]int{}
	} else {
		it = &matcherMergeIter{}
	}
	it.lists = [mergeIterListCount][]*Matcher{l1, l2, l3, l4, l5, l6}
	return it
}

// releaseMergeIter 将 MergeIter 归还池中，并释放对列表切片的引用。
func releaseMergeIter(it *matcherMergeIter) {
	it.lists = [mergeIterListCount][]*Matcher{}
	it.idx = [mergeIterListCount]int{}
	it.cur = nil
	mergeIterPool.Put(it)
}
