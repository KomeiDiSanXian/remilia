# Remilia vs FloatTech/ZeroBot-Plugin 对比分析报告

> 生成时间：2026-04-06  
> ZeroBot-Plugin 参考版本：`9a7690f`（最新 main 分支）  
> Remilia 参考版本：当前工作区

---

## 一、项目定位差异（先确立基准）

在逐条列举差距之前，必须先明确两者**根本定位不同**：

| 维度         | ZeroBot-Plugin                       | Remilia                                  |
|--------------|--------------------------------------|------------------------------------------|
| **类型**     | 完整可部署的 QQ Bot 应用             | 多平台 Bot 框架 / SDK                    |
| **交付物**   | 一个二进制可执行文件，开箱即用        | 一个 Go 模块，供他人引用构建 Bot         |
| **平台**     | 仅 QQ（via OneBot / WebSocket）       | QQ · Discord · Telegram · WeChat（均适配）|
| **插件数量** | 90+ 业务插件（游戏/AI/工具/娱乐等）  | 18 个基础内置模块（安全/管理/基础设施）  |
| **代码行数** | ~100k+ 行（含所有插件数据和逻辑）    | 工程代码质量更高，测试覆盖率更完善        |
| **核心依赖** | wdvxdr1123/ZeroBot + FloatTech系列库  | 自研多平台抽象层 + zerolog/viper/otel 等 |

**结论**：两者不是同类项目的竞争关系，而是"上游框架"与"下游应用"的关系。ZeroBot-Plugin 构建在 ZeroBot 这个框架之上，而 Remilia 本身就是在扮演框架角色。直接功能对比需要区分**框架层**和**应用层**分别讨论。

---

## 二、Remilia 缺失的能力

### 2.1 持久化存储层（高优先级 ⚠️）

**ZeroBot-Plugin 的做法**  
使用 `FloatTech/sqlite` + `jinzhu/gorm` 提供 SQLite ORM，所有插件通过 `gorm.DB` 持久化数据（用户积分、群配置、游戏状态、日志等）。每个插件独立管理自己的表。

**Remilia 的现状**  
完全没有数据库/存储抽象层，`infra/` 目录下没有 `storage/`、`db/` 或任何持久化相关包。目前的 `builtin/` 模块（如 cooldown、antispam）全部使用内存状态，进程重启后丢失。

**影响**  
- 无法开发需要持久化的功能（积分、签到、历史记录等）  
- 每个插件开发者需自己引入存储，且没有统一抽象，导致依赖混乱

---

### 2.2 逐群/逐用户的插件开关与权限持久化（高优先级 ⚠️）

**ZeroBot-Plugin 的做法**  
使用 `FloatTech/zbpctrl` 库，实现以下能力：  
- 管理员可以在群内开关某个功能模块  
- 超级管理员可以全局启用/禁用某插件  
- 配置持久化到 SQLite，重启不丢失  
- 提供标准指令 `/服务列表`、`/开启 xxx`、`/关闭 xxx`

**Remilia 的现状**  
`builtin/acl/` 和 `core/permission/` 提供了权限模型，但：  
- 权限配置是内存状态，无持久化  
- 没有现成的"逐群开关插件"的指令集  
- `plugin.Manager` 提供了 Enable/Disable API，但没有绑定到聊天指令

**影响**  
线上部署时，机器人管理者无法通过消息指令管理各群的功能开关，这是 QQ Bot 最基础的运营需求。

---

### 2.3 富文本/图片生成能力（中优先级）

**ZeroBot-Plugin 的做法**  
使用 `FloatTech/gg`（基于 fogleman/gg 定制）和 `FloatTech/rendercard` 实现：  
- 自定义字体渲染任意文字到图片（`plugin/font`）  
- 渲染精美卡片（群聊统计、游戏结果、运势等）  
- GIF 动图合成（`plugin/gif`）  
- 图表绘制（`wcharczuk/go-chart`）  
- 图片超分辨率（`plugin/realcugan`，通过 wasm 运行）  
- Emoji 合成（`plugin/emojimix`）

**Remilia 的现状**  
`infra/textimage/` 只支持在图片上渲染纯文字，依赖 `golang.org/x/image`。无法绘制卡片、图表、GIF 动图。

**影响**  
限制了可构建的插件种类，现代 Bot 的绝大多数娱乐/游戏功能依赖图片生成。

---

### 2.4 音频/TTS 支持（中优先级）

**ZeroBot-Plugin 有**  
- `plugin/ahsai`：基于 AH-SAI TTS 合成语音
- `plugin/alipayvoice`：支付宝到账语音合成
- `plugin/airecord`：AI 语音聊天记录
- `plugin/midicreate`：MIDI 音乐制作
- `plugin/jptingroom`：日语听力材料（含音频文件处理）

**Remilia 无任何音频处理**  
没有 TTS、语音合成、音频格式转换的能力，也没有相关接口定义。

---

### 2.5 Web 管理界面（低优先级，视目标用户而定）

**ZeroBot-Plugin 的做法**  
代码中预留了 `webctrl "github.com/FloatTech/zbputils/control/web"` 接口（虽默认注释掉），提供基于 Web 的管理 UI，包括服务列表管理、日志查看、插件配置修改。

**Remilia 的现状**  
`pprof.go` 暴露了 pprof 性能分析端点，`infra/server/` 提供了 HTTP Server 基础，但没有面向运营者的管理 UI。

---

### 2.6 内容数据管理（中优先级）

**ZeroBot-Plugin 的做法**  
使用独立的 `zbpdata` Git 子模块管理运营数据（图片资源、文本模板、词库等），支持通过 `file.SkipOriginal` 镜像加速数据下载，并有懒加载机制（首次调用时自动下载）。

**Remilia 的现状**  
没有内容数据管理方案，插件的数据文件（如词库、图片素材）需要开发者自行管理。没有懒加载、镜像加速等机制。

---

### 2.7 群管理 API 抽象（中优先级）

**ZeroBot-Plugin 的做法**  
通过 OneBot 标准 API 支持：  
- 踢出群员 / 禁言群员 / 设置管理员  
- 自动撤回消息（`plugin/autowithdraw`）  
- 好友/群邀请自动审批（`plugin/event`）  

**Remilia 的现状**  
`platform.Adapter` 接口定义了 `Sender` 和可选的 `MessageDeleter/MessageEditor`，但没有定义群管理接口：  
- 没有 `GroupManager` 接口（踢人/禁言/设置管理员）  
- 没有 `FriendManager` 接口（接受好友/群邀请）  
- 没有 `AutoModerationAction` 接口  

跨平台标准化这部分确实更难，但至少应在 `platform/` 中预留可选接口声明。

---

### 2.8 RSS / 推送订阅系统（低优先级）

**ZeroBot-Plugin 有**  
- `plugin/rsshub`：全功能 RSSHub 订阅，支持群/私聊推送
- `plugin/bilibilipush`：B站 UP 主开播/更新推送
- `plugin/minecraftobserver`：MC 服务器状态推送
- `plugin/steam`：Steam 游戏状态

**Remilia 的现状**  
`builtin/scheduler/` 提供了 cron 定时调度，但没有配套的"订阅-推送"管理框架（无持久化订阅列表、无多目标推送路由）。

---

### 2.9 具体业务插件生态（应用层差距）

下表列出 ZeroBot-Plugin 中 remilia 明显缺失的业务类插件类别（不建议全部添加，详见第四节建议）：

| 类别       | ZeroBot-Plugin 插件示例                                                 | Remilia 对应情况    |
|------------|-------------------------------------------------------------------------|---------------------|
| 娱乐游戏   | 国际象棋、钓鱼模拟器、猜歌、猜成语、牛牛大作战、扑克、原神抽卡、塔罗牌 | 无                  |
| AI功能     | AI聊天（aichat）、AI画图、LLM群聊总结、NSFW识别、百度内容审核           | 无                  |
| 工具       | 翻译、以图搜图（saucenao）、搜番（tracemoe）、在线运行代码               | 无                  |
| 资讯       | 今日早报、B站解析、RSS订阅、GitHub仓库搜索                              | 无                  |
| 社交       | 随机老婆、漂流瓶、CP短打、运势、摸鱼日历、一言                          | 无                  |
| 统计       | 聊天时长、聊天热词、睡眠管理、聊天计数                                  | builtin/stats 存在  |
| 内容审核   | 违禁词检测+自动处理、NSFW自动检测                                       | 仅 keywordfilter    |

---

## 三、Remilia 与 ZeroBot-Plugin 相比的不足之处

这些是 Remilia **有但质量/完整性不如**对方的地方（非完全缺失）。

### 3.1 权限系统深度不足

ZeroBot-Plugin 通过 `zbpctrl` 实现了完整的权限分层：  
`超级管理员 → 全局管理 → 群管理员 → 群员 → 黑名单用户`，且每层都对应具体指令。

Remilia 的 `core/permission/` 定义了角色枚举，但：  
- 没有将权限层级与具体业务操作绑定的中间层  
- 没有开箱即用的权限检查 middleware  
- 没有超级用户特权指令集（如紧急关闭所有插件、全局广播）

### 3.2 命令系统缺少"分群配置"能力

Remilia 的 `command/` 包提供了优秀的 Trie 前缀匹配和命令解析，但：  
- 没有按群/用户维度配置命令别名  
- 没有命令执行频率的群级配置（ZeroBot-Plugin 可以每群设不同冷却时间）  
- 没有命令的"每群独立状态"存储（需要开发者自行实现）

### 3.3 冷却（Cooldown）模块功能单一

Remilia `builtin/cooldown/` 存在，但：  
- ZeroBot-Plugin 的冷却系统细粒度到每个插件注册时可自定义 `GroupLimit`/`UserLimit`/`GlobalLimit`  
- Remilia 的 cooldown 是全局性的，无法在插件注册时声明各自的冷却策略

### 3.4 消息类型丰富度不足

ZeroBot-Plugin 通过 OneBot CQ 码支持：  
- 回复指定消息（引用回复）  
- 转发消息合并（longmessage / forward）  
- 语音/视频消息  
- 自定义音乐卡片  
- 戳一戳（poke）  
- JSON / XML 富媒体卡片  

Remilia 的 `platform.OutboundMessage` 支持文本、附件、Markdown、Embeds、Buttons，但：  
- 没有"引用回复特定消息 ID"的通用接口（仅 Discord 有 ReplyTo 概念）  
- 没有"合并转发"消息类型（QQ 特有）  
- 没有"戳一戳"类交互事件的标准抽象

### 3.5 中间件缺少"群级启停"控制点

Remilia 的 middleware 系统（rate limit / circuit breaker / adaptive 等）全部是全局性的，没有在中间件层面区分不同群的配置。ZeroBot-Plugin 在每个插件注册时可以声明是否"只在开启该功能的群生效"。

### 3.6 健康检查/监控深度有限

Remilia 有 `infra/health/` + OpenTelemetry + Prometheus，这是明显**优于** ZeroBot-Plugin 的地方。但：  
- 没有预构建的 Grafana Dashboard 模板  
- pprof 端点没有鉴权保护（任何能访问 HTTP 端口的人都能看到 pprof）  
- 没有 Discord/Telegram 等平台的连接延迟监控指标

---

## 四、建议：该加什么？如何改进？

### 4.1 必须添加（强烈建议）

#### ① 持久化存储抽象层（`infra/storage/`）

**方案**：定义统一的 `Store` 接口，提供 SQLite（轻量本地）和 Redis（分布式）两种实现，供插件按需选用。

```go
// 建议接口草案
type KVStore interface {
    Get(ctx context.Context, key string) ([]byte, error)
    Set(ctx context.Context, key string, val []byte) error
    Delete(ctx context.Context, key string) error
}

type TableStore interface {
    Query(ctx context.Context, table string, filter map[string]any) ([]map[string]any, error)
    Upsert(ctx context.Context, table string, row map[string]any) error
    Delete(ctx context.Context, table string, filter map[string]any) error
}
```

**理由**：几乎所有有实用价值的功能（签到/积分/配置/历史）都需要存储，这是基础设施短板。

---

#### ② 逐群功能开关 + 持久化（`builtin/pluginctrl/`）

**方案**：在现有 `builtin/core/` 基础上，添加：  
- 指令：`开启 <插件名>`、`关闭 <插件名>`、`服务列表`  
- 权限：群管理员/超级管理员才能操作  
- 存储：依赖存储层 ① 持久化每群配置  
- 集成：在 plugin.Manager 的 middleware 层自动检查

**理由**：不能逐群管理的 Bot 无法在多群场景下实际部署，这是商用必备。

---

#### ③ 群管理可选接口（`platform/`）

**方案**：在 `platform/adapter.go` 中新增可选接口：

```go
type GroupManager interface {
    KickMember(ctx context.Context, groupID, userID string) error
    BanMember(ctx context.Context, groupID, userID string, duration time.Duration) error
    SetAdmin(ctx context.Context, groupID, userID string, isAdmin bool) error
}

type InvitationHandler interface {
    AcceptGroupInvite(ctx context.Context, inviteID string) error
    AcceptFriendRequest(ctx context.Context, requestID string) error
}
```

**理由**：已有消息撤回接口的设计先例（`MessageDeleter`），此类扩展完全符合现有架构模式，成本低。

---

### 4.2 建议添加（优先级中等）

#### ④ 富文本图片生成（`infra/canvas/` 或引入 `gg`）

扩展 `textimage` 为更完整的 2D 渲染能力，或引入 `github.com/fogleman/gg` 作为可选依赖。为需要图片生成的插件提供统一画布 API。

重点支持：  
- 自定义字体文字渲染（非图片水印风格）  
- 简单卡片布局（圆角矩形、头像、多行文字）  
- 图表（条形/折线图，可引入 `go-chart`）

---

#### ⑤ 推送订阅框架（`builtin/subscription/`）

定义通用的"订阅-推送"接口：  
- 用户/群订阅某个数据源（RSS / 自定义）  
- 定时检测更新  
- 推送到订阅的目标（群 / 私聊）  
- 依赖存储层管理订阅列表

这样 RSS 插件、Bilibili 推送等都能复用同一框架。

---

#### ⑥ 中间件权限层扩展

在 `builtin/cooldown` 和 ACL 中添加：  
- 群级冷却（同一条命令在同一群内的冷却，与全局冷却分离）  
- 插件注册时声明默认冷却策略（参考 zbpctrl 的 `Control.Manager`）

---

### 4.3 不建议添加（保持框架定位）

以下 ZeroBot-Plugin 有，但 remilia **不应照搬**：

| 不建议添加的内容          | 原因                                                                                           |
|---------------------------|-----------------------------------------------------------------------------------------------|
| 90+ 具体业务插件          | remilia 是框架，业务插件应放在独立仓库。框架仓库塞满业务代码会破坏架构清晰度                  |
| QQ 专属特性（CQ码/合并转发）| remilia 是多平台框架，应保持平台无关性，QQ 特有功能放在 qq 适配器中                           |
| 写死的 NickName / 角色配置 | ZeroBot-Plugin main.go 写死了"椛椛/ATRI"等，框架层不应有 opinionated 的内容配置                |
| gomod2nix.toml / NixOS 构建| 特定发行版的部署配置与框架无关，增加维护负担                                                  |
| TTS/音频处理               | 涉及重量级 native 依赖（cgo/wasm），除非有明确需求，不建议框架层引入                          |

---

## 五、综合评估

| 维度               | ZeroBot-Plugin | Remilia       | 备注                                 |
|--------------------|:--------------:|:-------------:|--------------------------------------|
| 开箱即用性         | ★★★★★          | ★★☆☆☆         | ZBP 对最终用户友好，remilia 面向开发者 |
| 多平台支持         | ★☆☆☆☆          | ★★★★★         | remilia 明显领先                     |
| 架构工程质量       | ★★★☆☆          | ★★★★★         | remilia 在 lifecycle/DI/可观测性上领先|
| 测试完备性         | ★★☆☆☆          | ★★★★☆         | remilia 测试覆盖更系统                |
| 插件生态丰富度     | ★★★★★          | ★★☆☆☆         | ZBP 拥有大量现成业务插件              |
| 持久化能力         | ★★★★☆          | ★☆☆☆☆         | remilia **需要补齐**                 |
| 运营管理能力       | ★★★★☆          | ★★☆☆☆         | remilia 逐群开关、Web UI 需加强       |
| 图像/媒体处理      | ★★★★☆          | ★★☆☆☆         | remilia 仅基础文字图像               |
| 可观测性/监控      | ★★☆☆☆          | ★★★★★         | remilia 明显领先（OTel + Prometheus）  |
| 配置热重载         | ★★☆☆☆          | ★★★★★         | remilia 有完整的热重载系统            |

---

## 六、行动优先级建议

```
P0（立即）：
  - 设计并实现 infra/storage/ 持久化存储抽象
  - 基于存储层补全 builtin/acl 的持久化

P1（近期）：
  - builtin/pluginctrl：逐群开关 + 指令集
  - platform/ 补充 GroupManager / InvitationHandler 可选接口
  - builtin/cooldown 支持群级冷却策略声明

P2（中期）：
  - infra/canvas/ 或扩展 textimage 为富图渲染
  - builtin/subscription/ 推送订阅框架
  - pprof 端点增加鉴权保护

P3（长期/可选）：
  - Web 管理 UI（若目标用户需要运营控制台）
  - 预构建 Grafana Dashboard 模板
  - 内容数据管理（懒加载 / 镜像机制）
```

---

*本报告基于对两个仓库代码的静态分析，如有偏差请结合实际使用场景做调整。*

