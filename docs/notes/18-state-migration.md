# 状态迁移——插件热重载时版本化状态转换

> 插件升级版本号时，旧版本的状态数据格式可能不兼容。
> `MigrateState` 提供声明式的状态迁移管线，自动处理版本差异。

## 问题场景

```go
// v1.0.0: 用户数据存在 map[string]int
type State struct { Counters map[string]int }

// v2.0.0: 改为结构化的 Counter 列表
type State struct { Counters []Counter }
// 热重载时，RestoreState 收到 v1 格式的数据 → panic!
```

## 解决方案

```go
Descriptor{
    Name:    "counter",
    Version: "2.0.0",  // 升级版本号
    Advanced: &plugin.Advanced{
        SaveState: func() (any, error) {
            return &StateV1{Counters: oldMap}, nil
        },
        // 框架自动检测版本变化，调用迁移函数
        MigrateState: func(oldState any, oldVer, newVer string) (any, error) {
            // oldVer = "1.0.0", newVer = "2.0.0"
            s := oldState.(*StateV1)
            // 迁移逻辑
            counters := make([]Counter, 0, len(s.Counters))
            for name, count := range s.Counters {
                counters = append(counters, Counter{Name: name, Value: count})
            }
            return &StateV2{Counters: counters}, nil
        },
        RestoreState: func(state any) error {
            // 收到的是迁移后的 StateV2
            s := state.(*StateV2)
            p.counters = s.Counters
            return nil
        },
    },
    Setup: func(ctx) (any, error) {
        return &StateV2{}, nil
    },
}
```

## 触发条件

`MigrateState` 仅在**同时满足**以下条件时调用：
1. `SaveState` 返回值非 nil
2. `RestoreState` 非 nil
3. 当前 `Descriptor.Version` ≠ 上次加载的版本号
4. `MigrateState` 非 nil

## 调用时序

```
reload()
  ├── SaveState()                    ← 保存旧状态
  ├── unload()                       ← 停止旧实例
  ├── load()                         ← 加载新实例（Setup 拿到新版本号）
  ├── MigrateState(state, old, new)  ← 迁移（仅版本变化时）
  └── RestoreState(migrated)         ← 恢复迁移后的状态
```

## 无迁移场景

当 `MigrateState` 为 nil 且版本变化时，框架直接调用 `RestoreState` 传入原始旧状态。
此行为向后兼容——未设置迁移函数的插件不会因版本升级而 panic，但需确保新旧状态格式兼容。

## 版本检测

框架通过 `Instance.LoadedVersion()` 追踪上次成功加载时 `Descriptor.Version` 的值。
若 `Descriptor.Version` 为空字符串（未填写），版本始终视为不变，永不触发迁移。
