package engine

// temp_manager_index_test.go — TempManager 索引与过期堆的一致性
//
// 回归背景：
//   - Add 不查 byID，同一 matcher 重复入表会使 count 虚高（水位线与
//     HasAny 失真），并可能在迁移路径（Remove+Add）叠加重复条目；
//   - 过期堆比较的是可变的 expiresAt 字段，SetExpiration 就地改写后
//     堆不变式失效——已过期项可能沉在堆深处，CleanExpired 见到未过期
//     就 break，永不回收。

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestTempManager_AddIdempotent 验证重复 Add 不会重复计数/重复候选。
func TestTempManager_AddIdempotent(t *testing.T) {
	tm := newTempMatcherManager()
	et := EventType("dup_event")
	m := &Matcher{EventType: et}
	m.priority.Store(50)

	tm.Add(m)
	tm.Add(m)

	assert.Equal(t, 1, tm.Count(), "重复 Add 不得虚增计数")
	assert.Len(t, tm.Get(et), 1, "重复 Add 不得产生重复候选（否则 handler 执行两次）")
}

// TestTempManager_SetExpirationRebuildsHeap 验证延长超时后旧堆条目不会
// 让 matcher 被提前清理，缩短超时又能被及时清理。
func TestTempManager_SetExpirationRebuildsHeap(t *testing.T) {
	tm := newTempMatcherManager()
	et := EventType("exp_event")

	m := &Matcher{EventType: et}
	m.priority.Store(50)
	now := time.Now()
	tm.SetExpiration(m, now, now.Add(50*time.Millisecond))
	tm.Add(m)

	// 延长超时：cleanup 不得因旧堆条目而提前删除
	tm.SetExpiration(m, now, now.Add(time.Hour))
	assert.Empty(t, tm.CleanExpired(), "延长超时后不得提前清理")
	assert.Equal(t, 1, tm.Count())

	// 缩短到已过期：必须能立刻回收（堆序正确）
	tm.SetExpiration(m, now, now.Add(-time.Second))
	expired := tm.CleanExpired()
	assert.Len(t, expired, 1, "缩短为已过期后必须被清理")
	assert.Equal(t, 0, tm.Count())
	assert.True(t, m.IsDeleted())
}

// TestTempManager_RemoveGroupOnlyMatchesGroup 验证按分组清理只摘除同组条目。
func TestTempManager_RemoveGroupOnlyMatchesGroup(t *testing.T) {
	tm := newTempMatcherManager()
	et := EventType("grp_event")

	inGroup := &Matcher{EventType: et}
	inGroup.priority.Store(50)
	inGroup.SetGroup("plug")
	other := &Matcher{EventType: et}
	other.priority.Store(50)
	other.SetGroup("other")

	tm.Add(inGroup)
	tm.Add(other)

	removed := tm.RemoveGroup("plug")

	assert.Len(t, removed, 1)
	assert.Equal(t, 1, tm.Count(), "不相关分组的 matcher 必须保留")
	assert.True(t, inGroup.IsDeleted())
	assert.False(t, other.IsDeleted())
	assert.Len(t, tm.Get(et), 1)
}
