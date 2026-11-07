package engine

// state_command_cache_test.go — COW 状态与命令元数据缓存的一致性
//
// 覆盖：
//   - 命令桶重排不得就地改写旧 state 共享的底层数组（COW 契约）；
//   - 批量注册必须同步维护 commandInfoCache（否则 /help 看不到这些命令）；
//   - 链式元数据方法（SetDescription/SetUsage/SetHidden）即时生效。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestEngine_CommandReorderDoesNotMutateOldState 验证对命令 matcher 调用
// SetPriority 触发的命令桶重排不会就地改写旧 state 共享的底层数组。
func TestEngine_CommandReorderDoesNotMutateOldState(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	m1 := eng.OnCommand(et, "/prio")
	m1.Handle(func(ctx *context.Context) error { return nil })
	m2 := eng.OnCommand(et, "/prio")
	m2.Handle(func(ctx *context.Context) error { return nil })

	oldList := eng.state.Load().commandIndex["/prio"][et]
	if len(oldList) != 2 {
		t.Fatalf("expected 2 matchers in command bucket, got %d", len(oldList))
	}
	snapshot := append([]*Matcher(nil), oldList...)

	// 触发 withUpdatedMatcherIndex 的命令桶重排路径
	m2.SetPriority(1)

	// 旧 state 的切片必须保持原有元素与顺序（COW 契约：不得被就地排序改写）
	for i := range snapshot {
		if oldList[i] != snapshot[i] {
			t.Fatalf("old state command bucket mutated in place at index %d: COW broken", i)
		}
	}

	// 新 state 中优先级更小的 m2 应排在前面
	newList := eng.state.Load().commandIndex["/prio"][et]
	if len(newList) != 2 || newList[0] != m2 || newList[1] != m1 {
		t.Fatal("new state command bucket should be re-sorted with m2 first")
	}
}

// TestEngine_BatchRegisterMaintainsCommandInfoCache 验证批量注册的命令
// matcher 会进入 commandIndex 并同步维护命令元数据缓存（此前批量路径漏掉了
// commandInfoCache 更新，/help 看不到批量注册的命令）。
func TestEngine_BatchRegisterMaintainsCommandInfoCache(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	m := &Matcher{
		EventType:     et,
		Rules:         []context.Rule{context.OnCommand("/batchcmd")},
		coordinator:   eng,
		Source:        "global",
		triggerPrefix: "/",
		definition:    &command.Definition{Name: "batchcmd", Description: "批量注册"},
		Handler:       func(ctx *context.Context) error { return nil },
		execProfile:   newExecProfile(),
	}
	m.commandIndexed.Store(true)
	m.priority.Store(50)

	eng.BatchRegisterMatchers([]*Matcher{m})

	info := eng.FindCommand("/batchcmd")
	if info == nil {
		t.Fatal("batch-registered command missing from command info cache")
	}
	if info.Description != "批量注册" {
		t.Fatalf("Description = %q, want %q", info.Description, "批量注册")
	}
	if lst := eng.state.Load().commandIndex["/batchcmd"][et]; len(lst) != 1 || lst[0] != m {
		t.Fatal("batch-registered command matcher not present in commandIndex")
	}
}

// TestEngine_ChainedMetadataRefreshesCommandCache 验证文档推荐的
// OnCommand().SetDescription().Handle() 链式写法的元数据能立即反映到
// GetAllCommands/FindCommand（此前缓存停留在注册瞬间的空元数据）。
func TestEngine_ChainedMetadataRefreshesCommandCache(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	m := eng.OnCommand(et, "/desc")
	m.SetDescription("描述文本").SetUsage("/desc <x>")
	m.Handle(func(ctx *context.Context) error { return nil })

	info := eng.FindCommand("/desc")
	if info == nil {
		t.Fatal("command not found in cache")
	}
	if info.Description != "描述文本" || info.Usage != "/desc <x>" {
		t.Fatalf("stale command cache: Description=%q Usage=%q", info.Description, info.Usage)
	}

	// SetHidden(true) 应立即将命令从列表缓存移除
	m.SetHidden(true)
	if eng.FindCommand("/desc") != nil {
		t.Fatal("hidden command should disappear from command cache immediately")
	}
}
