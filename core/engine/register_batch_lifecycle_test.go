package engine

// register_batch_lifecycle_test.go — 批注册会话中 Delete/SetTemp 的结算回归测试
//
// 回归背景：会话期间 matcher 对事件不可见（未入 State），但 DeleteMatcher 对
// 未入 State 的 matcher 是空操作、SetTemp 会立即把它加进 TempManager。
// Flush 此前无条件提交批内收集：已删除的 matcher 变成 deleted=true 的僵尸
// 条目（计数/内存/命令表不回收），已迁移为临时的 matcher 与 State 双索引并存
// （同一事件执行两次）。

import (
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
)

func TestRegisterBatch_DeletedMatcherIsNotCommitted(t *testing.T) {
	et := string(platform.EventKindPrivateMessage)

	e := newEngineForTest(t)
	batch := e.BeginRegisterBatch()

	var calls atomic.Int32
	keep := e.On(et)
	keep.Handle(func(*context.Context) error {
		calls.Add(1)
		return nil
	})

	doomed := e.On(et)
	doomed.Handle(func(*context.Context) error {
		t.Error("deleted batched matcher must never run")
		return nil
	})
	e.DeleteMatcher(doomed)

	batch.Flush()

	assert.Equal(t, 1, e.GetMatcherCount(), "已删除的批内 matcher 不应留在 State")

	e.ProcessEvent(newTestCtx("hello"))
	assert.Equal(t, int32(1), calls.Load(), "存活的 matcher 仍应正常执行")
}

func TestRegisterBatch_TempMigrationIsNotDoubleIndexed(t *testing.T) {
	et := string(platform.EventKindPrivateMessage)

	e := newEngineForTest(t)
	batch := e.BeginRegisterBatch()

	var calls atomic.Int32
	m := e.On(et)
	m.Handle(func(*context.Context) error {
		calls.Add(1)
		return nil
	})
	// 批内迁移为临时 matcher：立即进入 TempManager
	m.SetTempWithMaxUse(5)

	batch.Flush()

	assert.Equal(t, 0, e.GetMatcherCount(), "迁移为临时的批内 matcher 不应同时留在 State")
	assert.True(t, e.internals.tempManager.HasAny(), "临时 matcher 应已入 TempManager")

	e.ProcessEvent(newTestCtx("hello"))
	assert.Equal(t, int32(1), calls.Load(), "迁移后的 matcher 每个事件只应执行一次")
}

func TestRegisterBatch_DeletedTempIsNotRegistered(t *testing.T) {
	et := string(platform.EventKindPrivateMessage)

	e := newEngineForTest(t)
	batch := e.BeginRegisterBatch()

	temp := e.OnTemp(et)
	temp.Handle(func(*context.Context) error {
		t.Error("deleted temp matcher must never run")
		return nil
	})
	temp.Delete()

	batch.Flush()

	assert.False(t, e.internals.tempManager.HasAny(), "已删除的批内临时 matcher 不应入 TempManager")
}
