# platform/satori — Satori 协议适配器

本包为 [Satori 协议](https://satori.chat/zh-CN/protocol/) 提供完整的 `platform.Adapter` 实现，
使框架能够接入任何实现了 Satori 协议的 SDK，例如：

- [Chronocat](https://github.com/chrononeko/chronocat)（QQ / NTQQ）
- [Lagrange.Core](https://github.com/LagrangeDev/Lagrange.Core)
- [Koishi](https://koishi.chat/)

---

## 目录

1. [快速开始](#快速开始)
2. [WebSocket 模式（推荐）](#websocket-模式推荐)
3. [WebHook 模式（可选）](#webhook-模式可选)
4. [直接调用 Satori API](#直接调用-satori-api)
5. [协议实现范围](#协议实现范围)
6. [架构说明](#架构说明)

---

## 快速开始

```go
package main

import (
	"log"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/platform/satori"
)

func main() {
	// 1. 创建 Satori 适配器（WebSocket 模式）
	adapter, err := satori.NewAdapter(satori.DefaultConfig(
		"http://localhost:5140", // Satori SDK 服务地址
		"chronocat",             // 平台标识符
		"1234567890",            // 机器人用户 ID
	))
	if err != nil {
		log.Fatal(err)
	}

	// 2. 构建 bot
	bot, err := remilia.NewBotBuilder().
		WithPlatformAdapter(adapter).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	// 3. 注册处理器
	bot.OnPrivateMessage(func(ctx *remilia.BotContext) {
		ctx.Reply(platform.TextMessage("pong"))
	})

	// 4. 运行
	log.Fatal(bot.Run())
}
```

---

## WebSocket 模式（推荐）

WebSocket 模式下，适配器主动连接到 Satori SDK，接收事件推送，并通过 HTTP API 发送消息。

### 配置

```go
cfg := satori.Config{
    ServerURL:  "http://localhost:5140", // SDK HTTP/WS 服务地址
    Token:      "my-secret-token",       // 鉴权 token（SDK 无鉴权时留空）
    Platform:   "chronocat",             // 平台标识
    UserID:     "1234567890",            // 机器人 ID
    Version:    "v1",                    // Satori API 版本，默认 "v1"
    
    // 自动重连配置
    ReconnectDelay:    2 * time.Second,
    MaxReconnectDelay: 60 * time.Second,
    MaxReconnects:     0, // 0 = 无限重连
    
    PingInterval:    10 * time.Second, // 心跳间隔（协议要求 10s）
    HTTPTimeout:     15 * time.Second, // API 调用超时
    EventBufferSize: 256,
}

adapter, err := satori.NewAdapter(cfg)
```

### 会话恢复

适配器会自动追踪最后接收到的事件序列号（`sn`），在断连重连时携带该序列号发送 `IDENTIFY`，
SDK 将重放断开期间的未推送事件。

---

## WebHook 模式（可选）

WebHook 模式下，SDK 向应用暴露的 HTTP 地址 POST 事件，应用无需维持长连接。

```go
// 创建 WebHook 适配器
adapter := satori.NewWebhookAdapter(satori.DefaultWebhookConfig(
    ":8080",      // 监听地址
    "chronocat",  // 平台标识
    "1234567890", // 机器人 ID
))

// 如需发送消息，需额外配置 HTTP API client
adapter.WithSendConfig(satori.Config{
    ServerURL: "http://localhost:5140",
    Token:     "my-secret-token",
})

bot, _ := remilia.NewBotBuilder().
    WithPlatformAdapter(adapter).
    Build()
```

SDK 需配置 WebHook URL 为 `http://<your-host>:8080/satori/webhook`。

### 反向鉴权

若 SDK 配置了 WebHook 鉴权 token，设置 `WebhookConfig.Token` 字段，
适配器会验证每个请求的 `Authorization: Bearer {token}` 头。

---

## 直接调用 Satori API

通过 `adapter.Client()` 获取底层 HTTP API 客户端，调用所有 Satori 标准 API：

```go
client := adapter.Client()

// 发送消息
msgs, err := client.MessageCreate(ctx, channelID, "<at id=\"123\"/>Hello!")

// 获取消息
msg, err := client.MessageGet(ctx, channelID, messageID)

// 撤回消息
err = client.MessageDelete(ctx, channelID, messageID)

// 获取频道列表
list, err := client.ChannelList(ctx, guildID, "")

// 获取群组成员
member, err := client.GuildMemberGet(ctx, guildID, userID)

// 踢出成员
err = client.GuildMemberKick(ctx, guildID, userID, false)

// 添加表情回应
err = client.ReactionCreate(ctx, channelID, messageID, "👍")
```

---

## 协议实现范围

### 事件服务
| 功能 | 状态 |
|------|------|
| WebSocket 连接 (IDENTIFY/PING/PONG/READY/EVENT) | ✅ |
| 会话恢复 (sn 追踪 + IDENTIFY with sn) | ✅ |
| WebHook 接收 | ✅ |
| META 信令 (实验性) | 已接收，暂忽略 |

### 消息编码
| 功能 | 状态 |
|------|------|
| 文本编码 / HTML 转义 | ✅ |
| `<at>` 提及用户 | ✅ |
| `<img>` / `<audio>` / `<video>` / `<file>` 资源元素 | ✅ |
| `<a>` 链接 | ✅ |
| `<quote>` 引用回复 | ✅ |
| `<button>` 按钮（实验性）| ✅ |
| `<b/i/u/s/code>` 修饰元素（解析）| ✅ |
| 消息内容解析（XML → 纯文本 + 附件）| ✅ |

### API
| 资源 | 方法 | 状态 |
|------|------|------|
| message | create / get / delete / update / list | ✅ |
| channel | get / list / create / update / delete / mute | ✅ |
| user.channel | create | ✅ |
| guild | get / list / approve | ✅ |
| guild.member | get / list / kick / mute / approve | ✅ |
| guild.member.role | set / unset | ✅ |
| guild.role | list / create / update / delete | ✅ |
| friend | list / approve | ✅ |
| login | get | ✅ |
| reaction | create / delete / list | ✅ |
| interaction | respond | ✅ |

### 事件类型映射
| Satori 事件 | platform.EventKind |
|------------|-------------------|
| `message-created` (私聊) | `EventKindPrivateMessage` |
| `message-created` (群聊) | `EventKindGroupMessage` |
| `message-updated` | `EventKindMessageUpdate` |
| `message-deleted` | `EventKindMessageDelete` |
| `guild-added` | `EventKindBotAdded` |
| `guild-removed` | `EventKindBotRemoved` |
| `guild-updated` | `EventKindGuildChange` |
| `guild-member-added` | `EventKindMemberJoin` |
| `guild-member-removed` | `EventKindMemberLeave` |
| `guild-member-updated` | `EventKindMemberUpdate` |
| `channel-added/updated/removed` | `EventKindChannelChange` |
| `reaction-added/removed` | `EventKindReaction` |
| `interaction/button/command` | `EventKindInteraction` |
| `friend-request` | `EventKindRequest` |
| `login-added/updated/removed` | `EventKindSystem` |

---

## 架构说明

```
platform/satori/
├── types.go          # Satori 协议数据类型（Channel, User, Guild, Login, Message...）
├── config.go         # 配置结构（Config, WebhookConfig）
├── client.go         # HTTP API 客户端（SatoriClient）
├── message_element.go# 消息元素编解码（XML ↔ platform.OutboundMessage）
├── event.go          # 事件转换（Satori Event → platform.Event）
├── ws.go             # WebSocket 连接管理（wsConn）
├── sender.go         # 消息发送器（satoriSender）
├── adapter.go        # 主适配器（Adapter，WebSocket 模式）
└── webhook.go        # WebHook 适配器（WebhookAdapter）
```

### 数据流（WebSocket 模式）

```
Satori SDK ──WS──► wsConn.readLoop()
                       │
                       ▼
                  convertEvent()         ← event.go
                       │
                       ▼
              platform.Event (satoriEvent)
                       │
                       ▼
              framework engine / handlers

Handler ──► ctx.Reply() ──► satoriSender.Send()
                                │
                                ▼
                       SatoriClient.MessageCreate()
                                │
                                ▼
                     HTTP POST /v1/message.create ──► Satori SDK
```

