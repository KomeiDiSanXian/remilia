package engine

// race_free_mutation_test.go — 运行期可变字段的读侧无锁回归测试
//
// 回归背景：
//   - middleware.go / process.go 直接读 m.group，而 SetGroup /
//     Engine.SetMatcherGroup 会在运行期持锁改写（插件重载是常态）；
//   - GetTempMatcherCleanInterval 无锁读 WriteMu 保护的清理间隔。

import (
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestEngine_GroupFieldRaceFree 验证 SetGroup 与事件派发并发时无数据竞争
// （-race 下必须干净）。
func TestEngine_GroupFieldRaceFree(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	m := eng.On(et)
	m.Handle(func(*context.Context) error { return nil })

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := range 200 {
			if i%2 == 0 {
				eng.SetMatcherGroup(m, "g1", "plugin:a")
			} else {
				eng.SetMatcherGroup(m, "g2", "plugin:b")
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := range 200 {
			if i%2 == 0 {
				m.SetGroup("g3")
			} else {
				m.SetGroup("g4")
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 200 {
			eng.ProcessEvent(context.NewContextFromEvent(
				newTestPlatformEventWithContent(platform.EventKindPrivateMessage, "hi"), nil))
		}
	}()
	wg.Wait()
}

// TestEngine_CleanIntervalRaceFree 验证清理间隔的读侧加锁。
func TestEngine_CleanIntervalRaceFree(t *testing.T) {
	eng := newEngineForTest(t)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 100 {
			eng.SetTempMatcherCleanInterval(time.Duration(i+1) * time.Millisecond)
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			_ = eng.GetTempMatcherCleanInterval()
		}
	}()
	wg.Wait()
	eng.SetTempMatcherCleanInterval(0)
}
