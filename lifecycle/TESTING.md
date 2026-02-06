# Lifecycle 测试文档

本文档说明 lifecycle 包的测试策略和测试用例。

---

## 📊 测试覆盖

### 核心测试

| 测试 | 覆盖功能 | 状态 |
|------|----------|------|
| TestManager_BasicLifecycle | 基本生命周期流程 | ✅ |
| TestManager_RunContextCancellation | 运行时 Context 取消 | ✅ |
| TestManager_StartError | 启动错误和回滚 | ✅ |
| TestManager_StopError | 停止错误处理 | ✅ |
| TestManager_MultipleComponents | 多组件管理 | ✅ |
| TestSimpleComponent | SimpleComponent 辅助类 | ✅ |
| TestResourceComponent | ResourceComponent 辅助类 | ✅ |

**覆盖率**: 100% 核心功能

---

## 🧪 测试用例

### 1. 基本生命周期

**测试**: `TestManager_BasicLifecycle`

**场景**:
```go
manager := NewManager()
manager.Register(comp1, comp2)
manager.Start(ctx)  // 按顺序启动
manager.Stop(ctx)   // 逆序停止
```

**验证**:
- OnStart 按注册顺序调用
- OnRun 在独立 goroutine 中执行
- OnStop 按逆序调用
- 状态转换正确

---

### 2. 运行时 Context

**测试**: `TestManager_RunContextCancellation`

**场景**:
```go
comp := NewSimpleComponent("test", nil,
    func(ctx context.Context) error {
        <-ctx.Done()  // 等待运行时 context 取消
        return nil
    }, nil)
```

**验证**:
- OnRun 接收到的 ctx 是运行时 context
- Stop() 时 ctx 被取消
- 组件能正确响应取消信号

---

### 3. 启动错误和回滚

**测试**: `TestManager_StartError`

**场景**:
```go
comp1.OnStart = success
comp2.OnStart = error  // 第二个组件失败
comp3.OnStart = ...    // 不应该被调用
```

**验证**:
- comp2 失败后立即停止
- comp1 的 OnStop 被调用（回滚）
- comp3 没有被启动
- Manager 状态回到 Created

---

### 4. 停止错误处理

**测试**: `TestManager_StopError`

**场景**:
```go
comp1.OnStop = error
comp2.OnStop = success
manager.Stop(ctx)
```

**验证**:
- 所有组件的 OnStop 都被调用
- 错误被返回但不中断流程
- 最终状态为 Stopped

---

## 🏃 运行测试

### 运行所有测试

```bash
go test ./lifecycle/... -v
```

### 运行特定测试

```bash
go test ./lifecycle -run TestManager_BasicLifecycle -v
```

### 测试覆盖率

```bash
go test ./lifecycle -cover
```

---

## 📝 测试最佳实践

### 1. 组件实现

```go
type testComponent struct {
    startCalled atomic.Bool
    runCalled   atomic.Bool
    stopCalled  atomic.Bool
}

func (c *testComponent) OnRun(ctx context.Context) error {
    c.runCalled.Store(true)
    <-ctx.Done()  // 等待取消
    return nil
}
```

### 2. 异步验证

```go
// 等待异步操作完成
time.Sleep(50 * time.Millisecond)

// 验证状态
assert.True(t, comp.startCalled.Load())
```

### 3. 超时保护

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
manager.Start(ctx)
```

---

## 📚 相关文档

- [lifecycle/README.md](./README.md) - 使用指南
- [设计文档](../docs/LIFECYCLE_DESIGN_EVALUATION_2026_02_05.md)
- [迁移报告](../docs/LIFECYCLE_MIGRATION_AND_CLEANUP_COMPLETE_2026_02_06.md)

---

**最后更新**: 2026年2月6日

