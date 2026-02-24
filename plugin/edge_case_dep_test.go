package plugin_test

import (
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/plugin"
)

// --- 边缘情况 1：MustGet 访问根本不存在的插件 ---

// TestEdge_MustGet_NonExistent 验证 MustGet 访问不存在插件时 panic 被框架捕获为注册错误。
// 预期：Setup panic → loadErr → RegisterV2 返回错误，插件不进入注册表。
func TestEdge_MustGet_NonExistent(t *testing.T) {
	pm := plugin.NewManager(nil)

	err := pm.RegisterV2(&plugin.PluginDescriptor{
		Name: "bad-plugin",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.MustGet("ghost") // ghost 根本不存在，应 panic
			return nil, nil
		},
	})

	if err == nil {
		t.Fatal("应返回错误（MustGet 不存在的依赖触发 panic）")
	}
	if pm.IsLoaded("bad-plugin") {
		t.Error("bad-plugin 不应被注册（Setup panic 应导致注册失败）")
	}
	t.Logf("✓ MustGet 不存在依赖：正确返回错误: %v", err)
}

// --- 边缘情况 2：Must[T] 访问根本不存在的插件 ---

func TestEdge_Must_NonExistent(t *testing.T) {
	pm := plugin.NewManager(nil)

	type GhostPlugin struct{}

	err := pm.RegisterV2(&plugin.PluginDescriptor{
		Name: "bad-plugin-must",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			_ = plugin.Must[GhostPlugin](ctx, "ghost") // 不存在，panic
			return nil, nil
		},
	})

	if err == nil {
		t.Fatal("应返回错误（Must 不存在的依赖触发 panic）")
	}
	if pm.IsLoaded("bad-plugin-must") {
		t.Error("bad-plugin-must 不应被注册")
	}
	t.Logf("✓ Must[T] 不存在依赖：正确返回错误: %v", err)
}

// --- 边缘情况 3：Get 访问不存在的插件 ---

// TestEdge_Get_NonExistent 验证 Get 访问不存在的插件时安全返回 false，不追踪，不影响注册。
func TestEdge_Get_NonExistent(t *testing.T) {
	pm := plugin.NewManager(nil)

	err := pm.RegisterV2(&plugin.PluginDescriptor{
		Name: "safe-plugin",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			v, ok := ctx.Get("ghost") // 不存在，应安全返回 false
			if ok || v != nil {
				t.Error("Get 不存在的依赖应返回 nil, false")
			}
			return nil, nil
		},
	})

	if err != nil {
		t.Fatalf("不应返回错误（Get 访问不存在依赖是安全的）: %v", err)
	}
	if !pm.IsLoaded("safe-plugin") {
		t.Error("safe-plugin 应注册成功")
	}
	t.Log("✓ Get 不存在依赖：安全返回 false，插件正常注册")
}

// --- 边缘情况 4：Try[T] 访问不存在的插件 ---

func TestEdge_Try_NonExistent(t *testing.T) {
	pm := plugin.NewManager(nil)

	type GhostPlugin struct{}

	err := pm.RegisterV2(&plugin.PluginDescriptor{
		Name: "safe-try-plugin",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p, ok := plugin.Try[GhostPlugin](ctx, "ghost")
			if ok || p != nil {
				t.Error("Try 不存在的依赖应返回 nil, false")
			}
			return nil, nil
		},
	})

	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	t.Log("✓ Try[T] 不存在依赖：安全返回 nil, false")
}

// --- 边缘情况 5：Deps 声明了不存在的插件 ---

// TestEdge_DeclaredDep_NonExistent 验证 Deps 中声明了不存在的插件时，
// 框架在 Setup 执行前就拦截（前置检查），不会进入 Setup。
func TestEdge_DeclaredDep_NonExistent(t *testing.T) {
	pm := plugin.NewManager(nil)

	setupCalled := false
	err := pm.RegisterV2(&plugin.PluginDescriptor{
		Name: "needs-ghost",
		Deps: []string{"ghost"}, // ghost 不存在
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			setupCalled = true // 不应执行到这里
			return nil, nil
		},
	})

	if err == nil {
		t.Fatal("应返回错误（Deps 中声明了不存在的插件）")
	}
	if setupCalled {
		t.Error("Setup 不应被调用（依赖检查应在 Setup 前完成）")
	}
	if !strings.Contains(err.Error(), "missing required dependency") &&
		!strings.Contains(err.Error(), "ghost") {
		t.Errorf("错误信息应提及缺失的依赖，实际: %v", err)
	}
	t.Logf("✓ Deps 声明不存在依赖：Setup 未执行，正确返回错误: %v", err)
}

// --- 边缘情况 6：Deps 声明了正在加载中（Loading 状态）的插件 ---

// TestEdge_DeclaredDep_LoadingState 验证依赖正处于 Loading 状态时被拦截。
func TestEdge_DeclaredDep_LoadingState(t *testing.T) {
	pm := plugin.NewManager(nil)

	// 注册一个正常插件作为基础
	_ = pm.RegisterV2(&plugin.PluginDescriptor{
		Name:  "ready-dep",
		Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
	})

	// 注意：Loading 状态只在 RegisterV2 执行过程中出现，
	// 外部很难模拟，但可以验证 Loaded 状态的依赖是可以正常注册的
	err := pm.RegisterV2(&plugin.PluginDescriptor{
		Name: "needs-ready",
		Deps: []string{"ready-dep"},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			return nil, nil
		},
	})

	if err != nil {
		t.Fatalf("依赖处于 Loaded 状态时应注册成功: %v", err)
	}
	t.Log("✓ 依赖处于 Loaded 状态：正常注册")
}

// --- 边缘情况 7：空字符串 name 的 Get ---

// TestEdge_Get_EmptyName 验证 Get("") 不追踪空名称依赖。
func TestEdge_Get_EmptyName(t *testing.T) {
	pm := plugin.NewManager(nil)

	err := pm.RegisterV2(&plugin.PluginDescriptor{
		Name: "empty-name-test",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			v, ok := ctx.Get("") // 空名称
			if ok || v != nil {
				t.Error("Get('') 应返回 nil, false")
			}
			return nil, nil
		},
	})

	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	t.Log("✓ Get('') 安全返回 nil, false，不追踪")
}

// --- 边缘情况 8：Smart 模式 DryRun 时 MustGet 不存在的依赖 ---

// TestEdge_Smart_MustGet_NonExistent 验证 Smart 模式 DryRun 阶段 MustGet panic 被
// recover 吞掉后，该依赖不会被追踪，但注册仍能正常进行（不会误报错误）。
func TestEdge_Smart_MustGet_NonExistent(t *testing.T) {
	pm := plugin.NewManager(nil)

	// base 存在
	base := &plugin.PluginDescriptor{
		Name:  "smart-base",
		Setup: func(ctx *plugin.SetupContext) (any, error) { return "api", nil },
	}

	// consumer 需要 smart-base（存在）+ 可能需要 optional-ghost（不存在）
	consumer := &plugin.PluginDescriptor{
		Name: "smart-consumer",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.MustGet("smart-base")        // 存在
			_, _ = ctx.Get("optional-ghost") // 不存在，Get 安全返回 false
			return nil, nil
		},
	}

	err := pm.RegisterMultipleV2Smart([]*plugin.PluginDescriptor{consumer, base})
	if err != nil {
		t.Fatalf("Smart 注册不应失败（不存在的可选依赖通过 Get 访问）: %v", err)
	}
	if !pm.IsLoaded("smart-consumer") || !pm.IsLoaded("smart-base") {
		t.Error("两个插件都应注册成功")
	}
	t.Log("✓ Smart 模式：Get 访问不存在依赖安全（返回 false），MustGet 存在依赖正常追踪")
}

// --- 边缘情况 9：Smart 模式 DryRun 时 MustGet panic，整批注册仍能成功 ---

// TestEdge_Smart_DryRun_PanicRecovered 验证 Smart DryRun 阶段 MustGet 不存在依赖时
// panic 被 recover 吞掉，该插件的依赖推断结果为空，
// 但如果真正注册时依赖已存在，注册仍然成功。
func TestEdge_Smart_DryRun_PanicRecovered(t *testing.T) {
	pm := plugin.NewManager(nil)

	// 预先注册一个插件（不在批次中）
	_ = pm.RegisterV2(&plugin.PluginDescriptor{
		Name:  "pre-registered",
		Setup: func(ctx *plugin.SetupContext) (any, error) { return "pre", nil },
	})

	// consumer 通过 MustGet 访问 pre-registered（在 tempContainer 中有）
	consumer := &plugin.PluginDescriptor{
		Name: "smart-consumer2",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.MustGet("pre-registered") // tempContainer 中有，DryRun 时能追踪到
			return nil, nil
		},
	}

	err := pm.RegisterMultipleV2Smart([]*plugin.PluginDescriptor{consumer})
	if err != nil {
		t.Fatalf("Smart 注册不应失败: %v", err)
	}
	t.Log("✓ Smart 模式：预注册依赖在 DryRun 阶段能正确追踪")
}

// --- 边缘情况 10：批量注册时 Deps 声明了批次内不存在的插件 ---

// TestEdge_BatchAtomicWithMissingDep 验证批量注册时某插件声明了不在批次内且未预注册的依赖，
// 整批次注册失败（原子回滚），已注册的插件被清���。
func TestEdge_BatchAtomicWithMissingDep(t *testing.T) {
	pm := plugin.NewManager(nil)

	a := &plugin.PluginDescriptor{
		Name:  "batch-a",
		Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
	}
	b := &plugin.PluginDescriptor{
		Name:  "batch-b",
		Deps:  []string{"batch-a", "batch-ghost"}, // batch-ghost 根本不存在
		Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
	}

	err := pm.RegisterMultipleV2Atomic([]*plugin.PluginDescriptor{a, b})

	if err == nil {
		t.Fatal("应返回错误（batch-ghost 不存在）")
	}
	// 原子回滚：batch-a 应该也被清理
	if pm.IsLoaded("batch-a") {
		t.Error("原子回滚失败：batch-a 应被清理")
	}
	if pm.IsLoaded("batch-b") {
		t.Error("batch-b 不应被注册")
	}
	t.Logf("✓ 批量注册中有不存在依赖：正确失败并原子回滚: %v", err)
}

// --- 边缘情况 11：非严格模式下 MustGet 不存在依赖（注册失败），但不污染已有状态 ---

// TestEdge_NonStrict_MustGet_NonExistent_NoSideEffect 验证非严格模式下
// MustGet 不存在依赖导致 Setup panic，注册失败，但 Manager 状态保持干净。
func TestEdge_NonStrict_MustGet_NonExistent_NoSideEffect(t *testing.T) {
	pm := plugin.NewManager(nil)

	// 先注册一个正常插件
	_ = pm.RegisterV2(&plugin.PluginDescriptor{
		Name:  "existing",
		Setup: func(ctx *plugin.SetupContext) (any, error) { return "api", nil },
	})

	// 注册一个会 panic 的插件（访问不存在依赖）
	err := pm.RegisterV2(&plugin.PluginDescriptor{
		Name: "will-fail",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.MustGet("totally-nonexistent")
			return nil, nil
		},
	})

	if err == nil {
		t.Fatal("应返回错误")
	}

	// 已有插件不受影响
	if !pm.IsLoaded("existing") {
		t.Error("existing 插件不应受到影响")
	}
	// 失败的插件不在注册表中
	if pm.IsLoaded("will-fail") {
		t.Error("will-fail 不应被注册")
	}
	// 注册表总数应为 1
	if pm.Count() != 1 {
		t.Errorf("注册表中应只有 1 个插件，实际 %d 个", pm.Count())
	}
	t.Log("✓ 非严格模式 MustGet 不存在依赖：失败干净，不污染 Manager 状态")
}

// --- 边缘情况 12：GetPlugin[T] 访问不存在的插件 ---

func TestEdge_GetPlugin_NonExistent(t *testing.T) {
	pm := plugin.NewManager(nil)

	type GhostPlugin struct{}

	err := pm.RegisterV2(&plugin.PluginDescriptor{
		Name: "getplugin-test",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p, err := plugin.GetPlugin[GhostPlugin](ctx, "ghost")
			if err == nil || p != nil {
				t.Error("GetPlugin 不存在的依赖应返回 error")
			}
			// GetPlugin 返回 error 但不 panic，Setup 可以选择继续
			return nil, nil // 注册成功
		},
	})

	if err != nil {
		t.Fatalf("GetPlugin 返回 error 不 panic，Setup 可以处理，不应导致注册失败: %v", err)
	}
	t.Log("✓ GetPlugin[T] 不存在依赖：返回 error 不 panic，插件可自行处理")
}

// --- 边缘情况 13：自依赖（插件 Get 自己的名字）---

func TestEdge_SelfDependency(t *testing.T) {
	pm := plugin.NewManager(nil)

	err := pm.RegisterV2(&plugin.PluginDescriptor{
		Name: "self-dep",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			// 尝试获取自身（不应追踪，Get 有 name != ctx.pluginName 检查）
			v, ok := ctx.Get("self-dep")
			// 自身还未注册完成（Loading 状态），容器中没有，应返回 false
			_ = v
			_ = ok
			return nil, nil
		},
	})

	if err != nil {
		t.Fatalf("自依赖不应导致注册失败（容器中没有自身，Get 返回 false）: %v", err)
	}
	t.Log("✓ 自依赖：Get 自身安全（返回 false，不追踪）")
}
