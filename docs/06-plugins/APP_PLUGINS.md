# 应用级插件指南

本文档覆盖随 `cmd/bot`（完整可运行的机器人）与应用发行版附带的插件：
**updater / pic / sauce / welcome / messagelog / about** 等。
这些插件位于 `cmd/bot/plugins/` 或 `builtin/`，配置统一写在 `plugins.<name>` 节。

## 🔄 updater — 自动更新

位于 `cmd/bot/plugins/updater/`。从 GitHub Releases 检查、下载、校验、替换并重启机器人自身。

### 命令（superadmin 角色或 `updater.manage` 权限）

| 命令 | 说明 |
|------|------|
| `/update check` | 检查 GitHub Releases 是否有新版本 |
| `/update status` | 查看版本、更新源、上次检查时间、备份、容器环境 |
| `/update now [--force]` | 立即下载 → sha256 校验 → 替换 → 重启（`--force` 重装同版本） |
| `/update auto on\|off` | 切换后台自动检查（默认开启，仅检查、不自动应用） |
| `/update rollback` | 回滚到上一个备份版本并重启 |

### 配置（`plugins.updater`）

```yaml
plugins:
  updater:
    repo: "KomeiDiSanXian/remilia"    # 发布源仓库
    check_interval: "1h"              # 自动检查间隔（<10 分钟自动钳制，GitHub 匿名 API 限流）
    auto_apply: false                 # 自动应用更新（默认仅检查）
    backup: true                      # 替换前备份旧二进制（remilia.old.<版本>）
    allow_prerelease: false           # 是否接受预发布版本
    disable_in_container: true        # 容器（/.dockerenv）内自动禁用自更新
    proxy: ""                         # 代理地址（适配 GitHub 不可直连环境）
    timeout: "10m"                    # 下载/校验超时
    child_console: ""                 # 子进程控制台策略，见下
```

### `child_console` 子进程控制台策略

| 值 | 行为 | 适用场景 |
|----|------|----------|
| `""`（默认） | 子进程标准输出接 NUL，不继承父进程控制台 | 安全，但子进程终端输出不可见 |
| `"new"` | 为子进程创建独立控制台窗口（仅 Windows） | 日志可见且子进程存活，Windows 推荐 |
| `"file"` | 子进程输出重定向到 `data/updater/child.log` | 无窗口、服务化场景 |

> ⚠️ **不要**配置为"继承父进程控制台"——Windows 上子进程持有父控制台句柄会在父进程退出时被连带终止（曾为此发布修复并撤回该方案）。

### 回滚与自愈

- 替换前备份旧二进制，跨平台两步改名（Windows 运行中 exe 可改名不可覆盖）
- 新进程启动最早期校验版本：一致 → 确认成功并清理残留；不一致 → 自动回滚旧备份并重新执行
- 拉起新进程失败 → 自动回滚；备份不可用 → 清除标记继续启动，不阻塞

## 🖼️ pic — 按标签随机发图

位于 `cmd/bot/plugins/pic/`。聚合 Safebooru / Gelbooru / rule34 / Konachan / Yande.re 五个图库，按标签随机发送图片。

### 命令

| 用法 | 说明 |
|------|------|
| `/pic <标签>` | 随机发一张该标签的图 |
| `/pic <标签> x3` | 末尾 `xN` 数量后缀，发 3 张 |
| `/pic <标签> -count 3` | `-count N` 显式张数（想搜 `x3` 标签时用 `-count 1`） |
| `/pic <标签> -site gelbooru` | 指定站点 |
| `/pic <标签> -recent 30` | 只发近 30 天内上传的图（`0`/`all` = 不过滤；未指定用 `recent_days` 配置） |

### 内容分级（rating 区间模型）

`rating` 配置为**精确档位或区间**（v1.30.0 起）：

- 单档：`rating: "safe"` 只发安全级；`"explicit"` 只发露骨级
- 区间：`rating: "safe..questionable"` 发 safe+sensitive+questionable（不含 explicit）；`"questionable..explicit"` 不含安全图
- `"all"`：全部档位不限制

档位：`safe` / `sensitive`（轻度敏感，泳装/暗示）/ `questionable` / `explicit`。
站点仅在其可提供的档位与请求区间有交集时参与请求（如 `rating: "explicit"` 时 safebooru/konachan 自动不可用）。

### 时间过滤

随机图片默认取**近两年内上传**的内容（`recent_days: 730`，`0` = 不过滤），避免总是抽到老图：
- konachan / yande.re：服务端 `date:` 过滤
- safebooru / gelbooru / rule34：按上传时间客户端过滤（随机池放大 + 空结果自动重试）
- 单次命令可用 `/pic -recent <N>` 覆盖（`0`/`all` = 不过滤）

### 配置（`plugins.pic`）

```yaml
plugins:
  pic:
    rating: "safe"                    # 内容分级：档位或区间，如 "safe..questionable"
    sites: []                         # 站点白名单（空 = 全部可用）
    max_count: 3                      # 默认最大张数
    recent_days: 730                  # 只发近 N 天内上传的图片（0 = 不过滤）
    download_concurrency: 4           # "下载+压缩"全局并发上限（1..16）
    send_thumbnail_max_bytes: 5242880      # 发送前压缩到该体积内（字节，0 = 不限）
    send_thumbnail_max_dimension: 4096     # 发送前最长边上限（像素，0 = 不限）
    gelbooru_user_id: ""              # gelbooru.com 认证（可选）
    gelbooru_api_key: ""
    rule34_user_id: ""                # rule34.xxx 认证（可选）
    rule34_api_key: ""
    proxy: ""                         # 代理
```

大图处理：单张图片下载上限为 64MB，超限报"图片过大"；下载成功的大图
（如 konachan 的大尺寸 PNG）发送前自动压缩到 `send_thumbnail_max_bytes` /
`send_thumbnail_max_dimension` 内。`download_concurrency` 限制的是内存大户
（下载缓冲 + 图片解码/编码）的全局并发，大量用户同时触发 `/pic` 时排队等待，
不放大内存峰值。

凭据安全：传输错误 URL 中的 `api_key`/`user_id` 自动脱敏，不泄露进日志或回复。

## 🎨 aimage — 文生图

位于 `cmd/bot/plugins/aimage/`。根据文字描述生成图片，支持两种后端（`plugins.aimage.provider` 切换）：

- **openai**（默认）：OpenAI 兼容 `/images/generations` 端点（DALL-E、SiliconFlow 等中转）
- **sdwebui**：本地 Stable Diffusion WebUI `/sdapi/v1/txt2img`（AUTOMATIC1111 风格，适合本地部署）

生成的图片统一物化为二进制后直传会话（不走 URL 转发，规避 URL 过期与 SSRF 问题）。
发送前会按配置将图片压缩到体积/边长上限内（与 pic / sauce 共用
`infra/imagekit` 压缩逻辑，避免 QQ 等平台超限降级为文件类型或走分片上传）。

QQ 平台单图生成完成后会附带一条"变体建议"提示键盘（`qq_variant_keyboard`，
默认开启）：按钮为 type=2 指令型——点击后自动把 `/aimage` 变体命令（同款
重绘 / 风格变体 / 常用尺寸）填入输入框，由用户确认发送，不产生互动回调，
规避 QQ webhook 回调投递不可靠的问题。

### 命令

| 命令 | 说明 |
|------|------|
| `/aimage <提示词>` | 生成一张图（尺寸取配置，默认 1024x1024） |
| `/aimage <提示词> -size 512x512` | 指定尺寸（宽x高） |
| `/aimage <提示词> -n 2` | 一次生成多张（上限 `max_n`，默认 3） |

### AI 工具

| 工具 | 说明 |
|------|------|
| `generate_image(prompt, size?, n?)` | 文生图。生成成功后图片自动发送到当前会话，工具只返回简短结果文本 |

`generate_image` 标记 `RequiresApproval`：`tool_approval=restricted` 模式下生成需人工审批（`off` 默认行为不变）。

### 配置（`plugins.aimage`）

```yaml
plugins:
  aimage:
    enabled: false              # 未启用不注册工具与命令
    provider: "openai"          # openai（OpenAI 兼容 /images/generations）| sdwebui（Stable Diffusion WebUI）
    base_url: ""                # openai 为 API 根地址；sdwebui 为 WebUI 地址（如 http://127.0.0.1:7860）
    api_key: ""                 # openai 兼容需要；sdwebui 通常留空
    model: "dall-e-3"           # openai 模型名；sdwebui 留空用默认 checkpoint
    size: "1024x1024"           # 默认尺寸（宽x高）
    max_n: 3                    # 单次生成张数上限
    send_max_bytes: 5242880     # 发送前体积上限（字节，默认 5MB，0=不限制）
    send_max_dimension: 4096    # 发送前最长边上限（像素，默认 4096，0=不限制）
    qq_variant_keyboard: true   # QQ 平台生成后附带"变体建议"提示键盘（默认 true，仅 QQ 单图场景）
    timeout: "120s"             # 生成请求超时
    steps: 20                   # sdwebui 采样步数
    cfg_scale: 7                # sdwebui CFG 引导强度
    negative_prompt: ""         # sdwebui 负面提示词
    proxy: ""                   # 可选代理
```

## 🔎 sauce — 以图搜图

位于 `cmd/bot/plugins/sauce/`。聚合 **SauceNAO / IQDB / TraceMoe / AnimeTrace** 多引擎检索图片来源。

- 用法：
  - 回复图片并在消息中附带 `/sauce`，或 `图片 + /sauce` 同发
  - 直接发送 `/sauce` 后等待补发图片（`image_wait_timeout`，默认 60s；发送非图片消息即取消）
  - 引用一条含图片的消息并发送 `/sauce`（QQ 等平台可直接提取引用消息图片）
- 参数：`/sauce -engine <name>` 指定引擎（`saucenao / iqdb / tracemoe / animetrace / all`，默认 all）
- 多引擎并发检索，聚合展示结果；`SauceNAO` 请求失败时 URL 中的 api_key 自动脱敏
- 引擎请求统一走 `plugins.sauce.proxy` 代理（可选）；IQDB 高峰期排队时自动重试并提示队列状态

## 👋 welcome — 群欢迎/告别

位于 `builtin/welcome/`。

| 命令 | 说明 |
|------|------|
| `/welcome set <消息>` | 设置本群欢迎消息（支持 `{user}` `{group}` 占位符） |
| `/welcome off` | 关闭本群欢迎 |
| `/welcome global <set\|on\|off\|status>` | 设置全局默认欢迎（所有未单独配置的群生效，需 superadmin 或 `welcome.global` 权限） |
| `/farewell ...` | 同上，对应告别消息 |

回退语义：群内显式配置优先，未配置的群自动继承全局默认。群级设置权限为 `welcome.manage`。

QQ 平台注意：`event_id` 是官方认可的被动消息类型（文档列为「被动消息（响应事件）」），
但入群事件（`GROUP_MEMBER_ADD`）**不在**官方 `event_id` 支持清单内，平台会以
`40034025 请求参数event_id无效` 拒绝被动回复（2026-09-18 起，实测时而成时而败）。

实测（2026-09-20，webhook 路径 7 条真实入群事件 / 19 次尝试）：`40034025` 只出现在
**收到回调后的极短时间内**——"仍未登记"的最晚时刻是收到回调后 `623ms`，"已登记"的最早
时刻是 `771ms`，窗口落在 `(0.62s, 0.78s]`，没有反例。被拒请求 120 ~ 218ms 就返回（只在
早期做了一次校验），成功请求 480 ~ 771ms（真的投递）。即平台把本次推送的事件登记为
"可回复"比它把回调交到我们手里晚约 0.6~0.8 秒，"收到回调就回复"必然踩空；与消息内容、
`event_id` 写法均无关（裸 id 才是写法错误，原样 `GROUP_MEMBER_ADD:<uuid>` 正确）。

框架的对策是**用同一个 `event_id` 退避重试**（默认 `1.5s → 2s → 3s`，首档对实测上界
`0.771s` 留约 2 倍余量）。之所以不改成主动消息：主动消息是另一种消息类型，不带
`event_id` 授权、需要群内开启「允许主动在群聊内发言」（未开启时 `40034105`）、并且
另计主动消息配额——替插件作者改语义并不合适。重试仍失败会如实返回错误。
（退群告别 `GROUP_MEMBER_REMOVE` 是另一回事：该事件本身不支持回复，稳定返回
`40034027`，只能以主动消息发送。）

诊断用环境变量 `QQ_PASSIVE_REPLY_DELAYS`（逗号分隔秒，最多 4 个，如 `"0.25,0.5,1,2"`）
可覆盖重试间隔：每次重试失败记 `[qq.Sender] event_id 被动回复重试仍未通过`（DEBUG，带
`delay_ms`），重试成功记 `[qq.Sender] event_id 被拒后重试成功`（WARN），据此可还原
"事件到达后多久平台才开始接受该 event_id"。拿到稳定值后请留空，改用默认间隔。

## 📝 messagelog — 群消息历史记录

位于 `builtin/messagelog/`。记录群聊历史：内存环形缓冲 + SQLite 持久化（`IsOutbound=true` 记录 bot 出站消息），按 `chat_id + event_id` 可查询。

## 🤖 about — 机器人自我介绍

位于 `builtin/about/`。`/about`（别名 `/botinfo`）展示框架版本、Git 提交、构建时间、运行状态、命令统计及系统资源（含宿主机内存占用/百分比）。
详见 [文档首页](../README.md)。

## 📌 qqpanel — QQ 指令面板与自定义菜单

位于 `cmd/bot/plugins/qqpanel/`，仅 QQ 平台生效。基于 QQ 开放平台
`/v2/panels`（指令面板）与 `/v2/menu`（自定义菜单）接口，自动从引擎命令表
提取命令生成面板 / 菜单，无需手工在 QQ 管理后台逐条配置。

### 命令（需 `qqpanel.manage` 权限）

| 命令 | 说明 |
|------|------|
| `/qqpanel setup [scope]` | 自动提取前 20 条命令创建指令面板（scope 默认 `group`） |
| `/qqpanel list [scope]` | 列出指定场景的指令面板 |
| `/qqpanel rm <panel_id>` | 删除指定指令面板 |
| `/qqmenu synchelp` | 按命令表自动构建 C2C 自定义菜单（折叠菜单 5 子项 + 顶级项，共 ≤10 项） |
| `/qqmenu status` | 查询当前自定义菜单 |

`scope` 取值：`c2c` / `group` / `channel` / `dm`。面板元素名为点击后填入
聊天输入框的命令文本，受平台 14 字符（中文按 2 字符计）限制自动截断。

### 自动构建的排除规则

自动构建**不会**包含：

- 隐藏命令（`Hidden=true`）与插件自身命令（`/qqpanel`、`/qqmenu`）
- 命令定义中声明了 `Permissions` 的命令
- 未声明 `Description` 的内部指令（如 pluginctrl 动态注册的 `/开启`、`/封禁`
  等管理指令），避免面板出现空白描述项
- 默认的管理类命令（权限在 handler 内校验，如 `/plugin`、`/perm`、`/welcome`、
  `/mute`、`/update` 等），避免普通用户点击后收到"权限不足"

`/help` **固定置顶**：面板首位与菜单折叠项首项始终保留 `/help`（除非被
显式排除），保证新用户可发现。

可通过 `plugins.qqpanel.exclude` 追加排除项（默认清单始终生效）：

```yaml
plugins:
  qqpanel:
    exclude: ["/weather", "/genshin"]   # 追加不进入面板/菜单的命令
```

---

*各插件的完整命令帮助可在机器人内使用 `/help` 查看。*
