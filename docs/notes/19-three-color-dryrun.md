# 19 — 三色标记法与依赖推断

## 问题

插件通过 `Service[T](ctx)` 在 Setup 闭包内声明依赖。在注册依赖未知前，无法进行拓扑排序。
需要一种方法"预跑"Setup 来发现依赖关系——但不能真的跑两次。

## 解决方案：三色标记法（Tri-color Marking）

将 GC 的三色抽象映射到依赖推断：

| 颜色 | GC 语义 | 依赖推断语义 |
|------|---------|-------------|
| **White** | 未扫描 | 未运行 Setup，依赖未知 |
| **Grey** | 扫描中 | Setup 已跑完，记录了 deps，但 dep 可能未就绪 |
| **Black** | 扫描完成（无外部引用） | 所有 dep 已就绪，可以拓扑排序 |

### 算法

```
Round 1:
  for each white 插件:
    跑 Setup → 收集 API 类型 + 追踪 deps → 标记 grey
    
  for each grey 插件:
    if 所有 dep 在容器中就绪 → 标记 black

Round N:
  for remaining grey 插件:
    if dep 现在已就绪 → 标记 black
    
  如果本轮无变化 → 结束
  剩余 grey = 循环依赖
```

### 关键设计细节

1. **每个 Setup 最多跑一次**。即使出现了依赖不满足的情况，`mustGet` 也已在 panic 前记录了 dep name。

2. **Type-based resolution**：`Service[T](ctx)` 不带 name 的调用，在类型未就绪时不会 panic。而是将 `reflect.Type` 记入 `pendingType`，等待后续轮次匹配新注册的 API。

3. **两阶段的职责分离**：
   - 三色 DryRun：发现 dep 边 + 检测循环
   - 拓扑排序（Kahn）：将 dep 图转为有序列表

### 与 GC 三色标记的对比

| 方面 | GC 三色标记 | 三色 DryRun |
|------|------------|-------------|
| **引用方向** | 从 root 沿指针向下走 | 从 dependent 沿 dep 边向上走 |
| **White 初始集** | 全体对象 | 全体插件 |
| **Grey 语义** | 已扫描但引用未扫描完 | 已跑完 Setup 但 dep 未就绪 |
| **Black 语义** | 无外部引用，可回收 | 所有 dep 就绪，可排序 |
| **循环检测** | Grey 始终不为空 → 强引用环 | Grey 始终不为空 → dep 环 |
| **终止条件** | Grey 队列为空 | 一轮无变化 |
| **推进方式** | 从 Grey 取一个，扫描其引用 | 检查所有 Grey 的 dep 是否已注册 |

最大的不同在于推进方式：

- GC 是**广度优先**：从 root 出发，逐个扫描对象，把遇到的新对象从 white 移到 grey
- DryRun 是**轮次驱动**：第一轮跑完所有 white，后续轮次只检查 dep 可用性

这是因为 GC 的"扫描"和"引用关系"在遍历时才知道，而 DryRun 的 dep 边在一次 Setup 后就全部确定了。

## 为什么需要这个（而非两遍方案）

两遍方案：每个插件 Setup 跑 2 次。

```
第一遍：A(跑) B(跑) C(跑)
第二遍：A(跑) B(跑) C(跑)
```

如果插件在 Setup 里做了 HTTP 调用且忘了 `if !ctx.DryRun { ... }` 守卫，就会跑两次。

三色方案：每个插件 Setup 跑 1 次。后续轮次只做 O(1) 的容器查 key。

```
第一轮：A(跑) B(跑) C(跑)
第二轮：A(查) B(查) C(查)  ← 没有 Setup
```

三色不要求插件作者记得检查 `ctx.DryRun`——无论如何只会执行一次。

## 循环依赖检测

三色天然支持循环检测：

```
初始: A(白) B(白)

第一轮:
  A: Setup, mustGet("B") → B 暂无 → track dep[B] → grey
  B: Setup, mustGet("A") → A 暂无 → track dep[A] → grey
  
解析轮:
  A: dep[B] 在容器? → B 是 grey(API未注册) → 否 → 保持 grey
  B: dep[A] 在容器? → A 是 grey(API未注册) → 否 → 保持 grey
  
  无变化 → 结束。A(灰) B(灰) → 循环依赖
```

对比显式 Deps 路径：`RegisterMultiple` 和 `RegisterMultipleAtomic` 直接在 `topologicalSort` 中用 Kahn 算法检测循环，不需要跑 DryRun。

## 复杂度

| 阶段 | 算法 | 复杂度 | 说明 |
|------|------|--------|------|
| 三色第一轮 | Setup × N | `N × O(Setup)` | 主导项 |
| 解析轮 | 查容器 × N × 轮数 | `O(N × D)` | D = 链深，通常 ≤ 3 |
| mergeInferredDeps | 线性 | `O(N × E)` | E = 边数 |
| 拓扑排序 | Kahn | `O(N + E)` | 声明边 + 推断边 |

实际量级：对于 ~20 个插件、链深 ≤ 3，解析轮仅 1-2 次（几十微秒），Setup 是唯一有意义的耗时。

## 历史

之前的实现是**两遍 DryRun**：第一遍收集类型，第二遍追踪依赖。后来发现：
1. 插件作者可能忘记检查 `ctx.DryRun`，导致 I/O 操作跑两次
2. 三色方案在理论上更优（每个 Setup 最多一次），且同样能覆盖所有场景

迁移到三色后，新增了 `pendingType` 机制处理反向顺序的类型解析依赖。
