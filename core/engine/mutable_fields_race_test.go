package engine

// mutable_fields_race_test.go — Matcher 可变字段读侧无锁的回归测试
//
// 回归背景：Handler / Source / Rules 都可能被运行期调用改写
// （Handle、SetSource、链式规则方法），而框架内多处无锁读取它们
// （索引重建读 Handler、查询与错误路径读 Source、Match 读 Rules），
// 与并发写构成数据竞争。
//
// 修复后：Handler/Source 的框架内读取走锁下访问器；Rules 由链式规则
// 方法以 COW 方式原子发布快照，Match 只读快照（热路径零锁）。
// 因此下列场景在 -race 下必须干净。

import (
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatcher_RulesAppendRaceFree(t *testing.T) {
	et := string(platform.EventKindPrivateMessage)
	eng := newEngineForTest(t)

	var calls int
	m := eng.On(et)
	m.Handle(func(*context.Context) error {
		calls++
		return nil
	})

	// 一个 goroutine 持续追加规则，另一个持续派发事件
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 200 {
			// 允许规则：追加不应破坏匹配语义
			m.Where(func(*context.Context) bool { return true })
		}
	}()
	go func() {
		defer wg.Done()
		for range 200 {
			eng.ProcessEvent(newTestCtx("hello"))
		}
	}()
	wg.Wait()

	// COW 快照与公开字段保持一致，且追加的规则确实生效
	m.Where(func(*context.Context) bool { return false })
	assert.False(t, m.Match(newTestCtx("hello")), "追加的规则应参与匹配")
	assert.Len(t, m.Rules, 201, "公开字段视图应与快照同步")
}

func TestMatcher_HandleRaceWithIndexRebuild(t *testing.T) {
	et := string(platform.EventKindPrivateMessage)
	eng := newEngineForTest(t)

	// 正则 matcher：索引重建时按 Handler 过滤正则桶（读 Handler）
	m := eng.On(et)
	m.Regex("hello")
	m.Handle(func(*context.Context) error { return nil })

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 100 {
			// Handle 持锁写 Handler
			m.Handle(func(*context.Context) error { return nil })
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			// 全量索引重建会读取每个 matcher 的 Handler
			eng.UpdateMatcherIndex(nil)
		}
	}()
	wg.Wait()

	assert.Equal(t, 1, eng.GetMatcherCount())
	require.NotNil(t, m.handlerLocked(), "Handler 应保持可读")
}

func TestMatcher_SourceRaceWithQueries(t *testing.T) {
	et := string(platform.EventKindPrivateMessage)
	eng := newEngineForTest(t)

	m := eng.On(et)
	m.Handle(func(*context.Context) error { return nil })

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 500 {
			m.SetSource("plugin:race")
		}
	}()
	go func() {
		defer wg.Done()
		for range 500 {
			_ = eng.GetMatcherStats()
			_ = eng.GetAllCommands()
		}
	}()
	wg.Wait()

	stats := eng.GetMatcherStats()
	assert.Equal(t, 1, stats.ByPlugin["race"], "查询应看到最终 Source")
}
