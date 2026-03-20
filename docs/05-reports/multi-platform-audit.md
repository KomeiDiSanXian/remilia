# 多平台抽象层审计报告

> 生成日期：2026-03-20  
> 分支：`feature/multi-platform-abstraction`  
> 项目状态：未发布（未到 v1.0）

---

## 一、架构概述

```
remilia（根包）
 └─ platform/           ← 平台无关接口定义
     ├─ adapter.go      ← PlatformAdapter、Sender、Registry
     ├─ event.go        ← Event 接口、EventKind、UserInfo、ChatInfo
     ├─ message.go      ← OutboundMessage、Attachment、Embed、Button
     ├─ qq/             ← QQ 官方 Webhook 完整实现
     ├─ telegram/       ← 占位符（未实现）
     ├─ discord/        ← 占位符（未实现）
     └─ wechat/         ← 占位符（未实现）

core/
 ├─ context/            ← Context 结构体、平台无关规则、Reply()
 └─ engine/             ← ProcessPlatformEvent、COW 状态、路由
```

平台无关的事件流：  
`PlatformAdapter.StartPlatform → handler(Event) → engine.ProcessPlatformEvent → context.AcquireContextFromEvent → Matcher.Match + invokeHandler → ctx.Reply(OutboundMessage) → Sender.Send`

---

## 二、问题清单

### P0 — 编译失败（阻塞 CI）

#### P0-1：`captureTestSender.Send` 引用未定义变量 `chatID`
- **文件**：`core/context/platform_event_test.go:160`
- **现象**：`undefined: chatID` — 测试包无法编译
- **根因**：`captureTestSender.Send` 方法中 `s.fn(chatID, msg)` 使用了未声明的 `chatID`；应从 context 通过 `platform.ChatInfoFromContext` 提取 `chat.ID`
- **修复**：将 `ctx` 参数不再忽略，改为 `ctx stdctx.Context`，用 `platform.ChatInfoFromContext(ctx)` 读取 `chat.ID` 后传入 `fn`

#### P0-2：`broadcast_test.go` 类型不匹配
- **文件**：`builtin/broadcast/broadcast_test.go:31`
- **现象**：`cannot use []string as []platform.ChatInfo` — 测试包无法编译
- **根因**：`Plugin.Broadcast` API 已从 `([]string, msg)` 升级为 `([]platform.ChatInfo, msg)`，但测试代码未同步更新
- **修复**：测试改用已有的 `BroadcastToGroups([]string, msg)` 辅助函数，或直接构造 `[]platform.ChatInfo`

---

### P1 — 运行时测试失败

#### P1-1：`GroupDelRobot` 错误映射到 `EventKindMemberJoin`
- **文件**：`platform/qq/event.go:46`
- **现象**：`GroupAddRobot` 和 `GroupDelRobot` 在同一 `case` 下都映射到 `EventKindMemberJoin`；机器人**被移出群组**应为 `EventKindMemberLeave`
- **根因**：代码将两者归入同一 `case`，漏掉了 `GroupDelRobot` → `MemberLeave` 的语义区分
- **修复**：拆分为两个 `case`：`GroupAddRobot → MemberJoin`，`GroupDelRobot → MemberLeave`

#### P1-2：QQ 事件测试语义与实现不一致（测试需更新）
- **文件**：`platform/qq/event_test.go`
- **现象**：`TestNewEvent_Notice`、`TestNewEvent_NoticeGroupFields`、`TestNewEvent_NoticeUserFields`、`TestNewEvent_NoticeEmptyDetail` 均断言 `GroupAddRobot`、`GroupDelRobot`、`FriendAdd`、`FriendDel` 应返回 `EventKindNotice`，与实现冲突
- **根因**：实现语义正确（`MemberJoin/MemberLeave` 比 `Notice` 更精确），测试是早期草稿，未随实现更新
- **修复**：更新 4 个测试，使断言与当前实现语义一致：
  - `GroupAddRobot → MemberJoin`
  - `GroupDelRobot → MemberLeave`（P1-1 修复后）
  - `FriendAdd → MemberJoin`
  - `FriendDel → MemberLeave`
  - `GroupMsgReject`、`GroupMsgReceive`、`C2CMsgReject`、`C2CMsgReceive` → `EventKindNotice`（不变）

---

### P2 — 设计缺陷 / 需改进

#### P2-1：占位符适配器（telegram/discord/wechat）返回 `fmt.Errorf` 而非哨兵错误
- **文件**：`platform/telegram/adapter.go`、`platform/discord/adapter.go`、`platform/wechat/adapter.go`
- **描述**：`StartPlatform` 和 `Send` 返回动态 `fmt.Errorf("... not yet implemented")`，调用方无法用 `errors.Is` 检测
- **建议**：将 `noopSender.Send` 改为返回 `platform.ErrNotImplemented`（待在 `errutil` 中新增），`StartPlatform` 同理；这样 `Registry.StartAll` 的错误过滤可以精确排除占位符错误

#### P2-2：`webhook_server.go` 的 worker 启动逻辑有两套重复实现
- **文件**：`platform/qq/webhook_server.go` vs `platform/qq/adapter.go`
- **描述**：`WebhookServerAdapter.StartPlatform` 直接 `go func()` 启动 worker，而 `Adapter.StartPlatform` 使用 `wg.Go` + 无界工作 channel；两者实现逻辑类似但不共享；`WebhookServerAdapter` 的 worker 在 select 中同时监听 `ctx.Done()` 和事件 channel，导致每个事件都要竞争两个 channel，性能低于 `Adapter` 的集中分发模式
- **建议**：抽取公共 `runWorkerPool(ctx, eventStream, workers, handler)` 函数，两个适配器共用；同时使用集中分发（单 goroutine 读 stream → 投 workCh），消除每个 worker 单独 select 的开销

#### P2-3：`qqEvent.ID()` 在 payload 为 nil 时 panic
- **文件**：`platform/qq/event.go:207`
- **描述**：`func (e *qqEvent) ID() string { return string(e.payload.ID) }` — 若 `e.payload == nil`（如 `NewEvent(nil)`），会在 `populate()` 中提前设置 `kind = Unknown` 并返回，但 `ID()` 仍直接解引用 `e.payload`
- **建议**：添加 nil 检查：`if e.payload == nil { return "" }`

#### P2-4：`PlatformCapabilities` 无法在运行时动态查询
- **文件**：`platform/adapter.go`
- **描述**：`PlatformCapabilities` 是静态结构体，Handler 只能整体读取，无法按能力名称字符串动态查询（对插件系统不友好）
- **建议**：考虑添加 `Has(cap string) bool` 方法，或使用 bitmask + 常量枚举替代 struct bool 字段（性能更优）

#### P2-5：`broadcast.go` 包文档注释残留旧 API 示例
- **文件**：`builtin/broadcast/broadcast.go:9`
- **描述**：包注释示例 `bc.Broadcast([]string{"chat001", "chat002"}, ...)` 使用旧的 `[]string` 签名，与当前 API 不符
- **建议**：更新包注释示例使用 `[]platform.ChatInfo`，或改为调用 `BroadcastToGroups`

#### P2-6：多平台 `noopSender` 各包重复定义
- **文件**：`platform/telegram/adapter.go`、`platform/discord/adapter.go`、`platform/wechat/adapter.go`
- **描述**：三个占位符包各自定义了 `type noopSender struct{}`，与 `platform.NoopSender` 重复，且行为不同（占位符 sender 返回错误，而 `platform.NoopSender` 返回 nil）
- **建议**：删除三个包的私有 `noopSender`，占位符 `StartPlatform` 未实现时 `Sender()` 可直接返回 `&platform.NoopSender{}`（或新增 `platform.ErrNotImplementedSender`）

---

### P3 — 长期优化 / 未实现功能

#### P3-1：无多平台事件去重机制
- **描述**：当同一用户在 QQ 和 Discord 均绑定了账号时，一条指令可能在两个平台同时触发相同 handler；框架层目前无跨平台去重支持
- **建议**：可在 `platform.Event` 中增加 `CorrelationID` 可选字段，由上层业务实现去重

#### P3-2：`GetPlatformCapabilities()` 未在 `Context` 上暴露
- **描述**：`BotHandler` 中若要实现"渐进增强"（根据平台能力决定消息格式），需要能在 handler 中查询当前平台能力；目前 `Context` 只提供 `GetEventPlatform()` 字符串，无法直接获取 `PlatformCapabilities`
- **建议**：在 `Context` 上增加 `GetPlatformCapabilities() platform.PlatformCapabilities`，通过 `platformEvent.Platform()` 从 `Registry` 或适配器获取

#### P3-3：`OutboundMessage.Buttons` 的 `Row` 字段设计待完善
- **描述**：`Button.Row` 设计为 0=自动排列，但 Discord 允许同一行最多 5 个按钮且最多 5 行，当前结构体对此无边界约束；`Sender` 必须自己做截断/分行
- **建议**：补充文档说明各平台限制；或增加 `ButtonRow` 类型封装行内按钮列表，使结构更清晰

#### P3-4：Telegram/Discord/WeChat 适配器无实现
- **描述**：三个平台适配器均为占位符，`StartPlatform` 返回 "not yet implemented"
- **建议**：后续分别实现；当前状态已通过 `Registry.StartAll` 的错误过滤机制（非 ctx 取消错误会返回到调用方）正确处理，不影响 QQ 的正常运行

---

## 三、平台设计评估

### 优点
- ✅ 接口设计清晰：`PlatformAdapter` / `Sender` / `Event` 三层分离，扩展新平台只需实现三个接口
- ✅ `ChatInfo` + `WithChatInfo(ctx)` 模式优雅，避免了 Sender.Send 多余参数
- ✅ `OutboundMessage` 支持 Text/Markdown/Attachment/Embed/Button 多模态，覆盖主流平台能力
- ✅ `PlatformCapabilities` 使 Handler 可做渐进增强，设计正确
- ✅ `Registry` 支持多平台并发运行，生命周期管理完整
- ✅ QQ adapter 的 bounded worker pool 设计正确，防止事件积压时 goroutine 爆炸

### 待改进
- ⚠️ `PlatformCapabilities` 为静态结构体，动态查询不便（P2-4）
- ⚠️ Context 上缺少 `GetPlatformCapabilities()` 方法，Handler 渐进增强实现麻烦（P3-2）
- ⚠️ 占位符适配器错误类型不可感知（P2-1）
- ⚠️ WebhookServerAdapter 与 Adapter worker 代码重复，维护成本高（P2-2）

---

## 四、修复优先级总结

| 优先级 | 编号 | 简述 | 影响范围 |
|--------|------|------|----------|
| P0 | P0-1 | `captureTestSender` 引用未定义 `chatID` | CI 失败 |
| P0 | P0-2 | `broadcast_test` 类型不匹配 | CI 失败 |
| P1 | P1-1 | `GroupDelRobot` 映射错误 | 运行时事件路由错误 |
| P1 | P1-2 | QQ 事件测试语义过时 | 测试失败 |
| P2 | P2-3 | `qqEvent.ID()` nil panic | 边界情况崩溃 |
| P2 | P2-5 | 包注释残留旧 API | 文档误导 |
| P2 | P2-6 | 多包重复 `noopSender` | 代码冗余 |
| P2 | P2-1 | 占位符错误不可检测 | 可观测性 |
| P2 | P2-2 | Worker 代码重复 | 维护成本 |
| P2 | P2-4 | Capabilities 动态查询 | 扩展性 |
| P3 | P3-2 | Context 缺少能力查询 | 开发体验 |
| P3 | P3-1 | 无跨平台去重 | 功能缺失 |
| P3 | P3-3 | Button.Row 边界约束 | API 健壮性 |
| P3 | P3-4 | telegram/discord/wechat 未实现 | 计划中 |

