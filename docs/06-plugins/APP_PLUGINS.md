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

### 内容分级（rating 区间模型）

`rating` 配置为**精确档位或区间**（v1.30.0 起）：

- 单档：`rating: "safe"` 只发安全级；`"explicit"` 只发露骨级
- 区间：`rating: "safe..questionable"` 发 safe+sensitive+questionable（不含 explicit）；`"questionable..explicit"` 不含安全图
- `"all"`：全部档位不限制

档位：`safe` / `sensitive`（轻度敏感，泳装/暗示）/ `questionable` / `explicit`。
站点仅在其可提供的档位与请求区间有交集时参与请求（如 `rating: "explicit"` 时 safebooru/konachan 自动不可用）。

### 配置（`plugins.pic`）

```yaml
plugins:
  pic:
    rating: "safe"                    # 内容分级：档位或区间，如 "safe..questionable"
    sites: []                         # 站点白名单（空 = 全部可用）
    max_count: 3                      # 默认最大张数
    gelbooru_user_id: ""              # gelbooru.com 认证（可选）
    gelbooru_api_key: ""
    rule34_user_id: ""                # rule34.xxx 认证（可选）
    rule34_api_key: ""
    proxy: ""                         # 代理
```

凭据安全：传输错误 URL 中的 `api_key`/`user_id` 自动脱敏，不泄露进日志或回复。

## 🔎 sauce — 以图搜图

位于 `cmd/bot/plugins/sauce/`。聚合 **SauceNAO / IQDB / TraceMoe / ASCII2D** 多引擎检索图片来源。

- 用法：回复图片并在消息中附带 `/sauce`，或 `图片 + /sauce` 同发
- 多引擎并发检索，聚合展示结果；`SauceNAO` 请求失败时 URL 中的 api_key 自动脱敏

## 👋 welcome — 群欢迎/告别

位于 `builtin/welcome/`。

| 命令 | 说明 |
|------|------|
| `/welcome set <消息>` | 设置本群欢迎消息（支持 `{user}` `{group}` 占位符） |
| `/welcome off` | 关闭本群欢迎 |
| `/welcome global <set\|on\|off\|status>` | 设置全局默认欢迎（所有未单独配置的群生效，需 superadmin 或 `welcome.global` 权限） |
| `/farewell ...` | 同上，对应告别消息 |

回退语义：群内显式配置优先，未配置的群自动继承全局默认。群级设置权限为 `welcome.manage`。

## 📝 messagelog — 群消息历史记录

位于 `builtin/messagelog/`。记录群聊历史：内存环形缓冲 + SQLite 持久化（`IsOutbound=true` 记录 bot 出站消息），按 `chat_id + event_id` 可查询。

## 🤖 about — 机器人自我介绍

位于 `builtin/about/`。`/about`（别名 `/botinfo`）展示框架版本、Git 提交、构建时间、运行状态、命令统计及系统资源（含宿主机内存占用/百分比）。
详见 [文档首页](../README.md)。

---

*各插件的完整命令帮助可在机器人内使用 `/help` 查看。*
