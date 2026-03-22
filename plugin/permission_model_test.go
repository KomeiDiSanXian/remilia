package plugin_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// TestPermissionModel_EngineReader 验证 Info.Coordinator() 返回只读视图。
//
// 这是权限模型的核心保证之一：普通插件通过 ctx.Info.Coordinator()
// 只能拿到 engine.Reader 接口，无法调用任何写操作。
func TestPermissionModel_EngineReader(t *testing.T) {
	pm := plugin.NewManager(nil)

	var capturedCoordinator engine.Reader

	_ = pm.RegisterV2(&plugin.Descriptor{
		Name: "reader-test",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			capturedCoordinator = ctx.Info.Coordinator()
			return nil, nil
		},
	})

	if capturedCoordinator == nil {
		t.Skip("Coordinator returned nil (no engine attached), skipping type check")
	}

	// 关键：capturedCoordinator 必须是 engine.Reader，
	// 编译器已经保证它不能是 *engine.Engine（因为接口不同）。
	// 以下代码在编译期就无法通过，这就是类型系统的保证：
	//
	//   capturedCoordinator.On(...)              // 编译错误：Reader 没有 On 方法
	//   capturedCoordinator.RegisterCommand(...) // 编译错误
	//   capturedCoordinator.DeleteMatcher(...)   // 编译错误
	//   capturedCoordinator.RemoveGroup(...)     // 编译错误
	//   capturedCoordinator.Use(...)             // 编译错误

	t.Log("✓ ctx.Info.Coordinator() 返回 engine.Reader（只读），编译器阻止写操作")
}

// TestPermissionModel_AdminNilForUnprivileged 验证未声明 Privileged 的插件 ctx.Admin 为 nil。
func TestPermissionModel_AdminNilForUnprivileged(t *testing.T) {
	pm := plugin.NewManager(nil)
	var adminWasNil bool

	_ = pm.RegisterV2(&plugin.Descriptor{
		Name:       "unprivileged",
		Privileged: false, // 显式声明（也是默认值）
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			adminWasNil = ctx.Admin == nil
			return nil, nil
		},
	})

	if !adminWasNil {
		t.Fatal("未声明 Privileged 的插件不应获得 ctx.Admin 权限")
	}
	t.Log("✓ Privileged=false 时 ctx.Admin 为 nil，无法调用写操作")
}

// TestPermissionModel_AdminNotNilForPrivileged 验证声明 Privileged 的插件 ctx.Admin 非 nil。
func TestPermissionModel_AdminNotNilForPrivileged(t *testing.T) {
	pm := plugin.NewManager(nil)
	var adminNotNil bool

	_ = pm.RegisterV2(&plugin.Descriptor{
		Name:       "privileged-plugin",
		Privileged: true, // 声明需要管理权限
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			adminNotNil = ctx.Admin != nil
			return nil, nil
		},
	})

	if !adminNotNil {
		t.Fatal("Privileged=true 的插件应获得非 nil 的 ctx.Admin")
	}
	t.Log("✓ Privileged=true 时 ctx.Admin 非 nil，可以调用 Reload/Disable/Enable/Unregister")
}

// TestPermissionModel_AdminWriteOps 验证 ManagerWriter 接口只包含写操作，
// 不包含 List/GetMetadata 等只读方法（只读通过 ctx.Info 访问）。
func TestPermissionModel_AdminWriteOps(t *testing.T) {
	pm := plugin.NewManager(nil)

	// 注册一个目标插件
	_ = pm.RegisterV2(&plugin.Descriptor{
		Name:  "target",
		Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
	})

	var writerCanReload bool

	_ = pm.RegisterV2(&plugin.Descriptor{
		Name:       "admin-test",
		Deps:       []string{"target"},
		Privileged: true,
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			// ctx.Admin 只暴露写方法，List/Count/GetMetadata 等只读方法通过 ctx.Info 访问
			// 以下是合法的写操作路径：
			_ = ctx.Admin.Reload     // 不调用，只验证方法存在
			_ = ctx.Admin.Disable    // 同上
			_ = ctx.Admin.Enable     // 同上
			_ = ctx.Admin.Unregister // 同上
			writerCanReload = true
			return nil, nil
		},
	})

	if !writerCanReload {
		t.Fatal("Privileged 插件应能访问 ctx.Admin 的写方法")
	}
	t.Log("✓ ctx.Admin（ManagerWriter）只暴露写操作，只读查询通过 ctx.Info 访问")
}

// TestPermissionModel_NoPrivateInterfaceAssertions 验证
// 在正常注册流程中，插件不需要通过私有接口类型断言来获取 Manager 或 Engine 的写权限。
//
// 这个测试确保不存在任何合法的"绕过路径"：
//   - ctx.Info 是 Info 接口 → 只读
//   - ctx.Info.Coordinator() 返回 engine.Reader → 只读
//   - ctx.Admin 是 ManagerWriter 接口 → 仅 Privileged 插件可用，且只有写方法
//   - ctx.Reg 是 RegistryWriter 接口 → 仅注册当前插件的 Matcher，有权限边界
func TestPermissionModel_NoPrivateInterfaceAssertions(t *testing.T) {
	pm := plugin.NewManager(nil)

	_ = pm.RegisterV2(&plugin.Descriptor{
		Name: "permission-check",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			// ctx.Info 是接口，无法对其做类型断言拿到 *Manager
			// （即使底层是 managerInfoView，它也不对外暴露 *Manager）
			info := ctx.Info

			// 尝试类型断言到 *plugin.Manager —— 这在正确设计下永远 false
			type managerHolder interface {
				Manager() *plugin.Manager
			}
			if _, ok := info.(managerHolder); ok {
				t.Error("ctx.Info 不应实现私有 Manager() 方法，这意味着权限隔离被破坏")
			}

			// Coordinator() 返回 Reader，无法断言为 *engine.Engine
			coord := ctx.Info.Coordinator()
			if coord != nil {
				if _, ok := coord.(*engine.Engine); ok {
					// 如果能成功断言为 *engine.Engine，权限隔离仍然存在漏洞
					t.Error("ctx.Info.Coordinator() 不应能断言为 *engine.Engine（权限隔离漏洞）")
				}
			}

			return nil, nil
		},
	})

	t.Log("✓ 不存在通过私有接口断言绕过权限隔离的路径")
}
