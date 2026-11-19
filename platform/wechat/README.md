# platform/wechat — 企业微信智能机器人

企业微信侧有四种接入形态，只有**智能机器人（AI Bot）**能当通用聊天机器人用：

| 形态 | 单聊 | 群聊 | 主动推送 | 备注 |
|---|---|---|---|---|
| 群机器人（webhook） | ❌ | 只能发，不能收 | ✅ | 单向推送 |
| 自建应用 | ✅ | ❌ | ✅ | 群聊需应用自己创建 |
| 智能机器人 · URL 回调 | ✅ | 群内 @机器人 | ❌ | 回复走一次性 `response_url` |
| 智能机器人 · 长连接 | ✅ | 群内 @机器人 | ✅ | 无需公网地址、无需加解密 |

## 配置

```yaml
bot:
  wechat:
    # ---- 长连接（默认，推荐）----
    mode: "ws"                 # ws（默认）| webhook
    bot_id: "YOUR_AIBOT_ID"    # 智能机器人 BotID，两种模式共用
    secret: "..."              # 长连接专用密钥

    # ---- URL 回调 ----
    # mode: "webhook"
    # bot_id: "YOUR_AIBOT_ID"
    # token: "..."             # 回调签名 Token（与 secret 不是同一个值）
    # encoding_aes_key: "43 位" # 回调加密密钥
    # webhook:
    #   listen_addr: ":8080"
    #   path: "/wechat/aibot"  # 默认值
    #   shutdown_timeout: "10s"
```

两种模式的凭证**互不通用**：`secret` 只用于长连接，`token`/`encoding_aes_key`
只在 URL 回调存在。配反了会在订阅/验签阶段失败，因此配置层按 mode 分别校验。

## 事件映射

| 企业微信 | platform.EventKind | 说明 |
|---|---|---|
| `msgtype=text/image/voice/file/video/mixed`（`chattype=single`） | `PRIVATE_MESSAGE` | 会话 ID 用 `userid` |
| 同上（`chattype=group`） | `GROUP_MESSAGE` | 会话 ID 用 `chatid`，正文去掉开头 `@机器人` |
| `eventtype=enter_chat` | `NOTICE` | 可用 `ReplyWelcome` 回欢迎语 |
| `eventtype=template_card_event` | `INTERACTION` | 按钮 `key` 作为正文 |
| `eventtype=feedback_event` | `NOTICE` | 用户对回复的反馈 |
| `eventtype=disconnected_event` | `NOTICE` | 适配器内部消费，不交给业务 |

所有消息事件都实现 `platform.DirectedAtBotEvent`：智能机器人只会收到
"群里 @它"或"单聊发给它"的消息，两种语义上都是直接对话，因此
`OnMentionedBot` 可直接命中。

## 回复模型

框架层的 `Sender` 只认 `ChatInfo.Tokens`，由适配器在建立事件时填入：

| 模式 | Token | 有效期 | 限制 |
|---|---|---|---|
| ws | `reply_req_id`（回调帧头的 `req_id`） | 收到回调后 24 小时 | 必须原样透传到 `aibot_respond_msg` |
| webhook | `response_url` | 1 小时 | **只能用一次** |

缺少授权时 `Send` 返回 `SendErrInvalidTarget` 而不是静默成功——企业微信只能
"回复当前回调"，主动发言必须显式用 `SessionNotifier`（仅 ws 模式）。

## 平台专有能力

```go
api, _ := platform.GetPlatformAPIAs[*wechat.Sender](sender)
```

| 方法 | 用途 |
|---|---|
| `UploadMedia` | 分片上传素材换 `media_id`（仅 ws） |
| `DownloadMedia` | 下载入站媒体并**解密**（直链内容是密文） |
| `StreamReply` | 流式回复（长连接主动推刷新） |
| `ReplyWelcome` | 回复进入会话欢迎语 |
| `UpdateTemplateCard` | 原地更新模板卡片（5 秒内） |
| `NotifyMedia` | 主动推送已上传的媒体（仅 ws） |

## 已知坑

1. **同一 BotID 只能有一条长连接**。新连接订阅成功会踢掉旧连接，旧连接收到
   `disconnected_event`。适配器把它当致命错误并停止重连——多副本请用主备，
   不要多活，否则两个实例互相踢、消息随机丢。`HealthDetail` 会给出
   `kicked_by_new_connection`。
2. **回调加解密有两处非标准细节**：PKCS#7 填充块是 **32 字节**（不是 16），
   智能机器人场景 `receiveid` 是**空字符串**。用通用 AES 库直接解会在末尾残留
   填充字节，表现为 JSON 解析失败或图片尾部损坏。
3. **入站媒体直链 5 分钟过期且加密**，`aeskey` 每条链接唯一。拿到 URL 而不解密
   只能得到密文；要及时下载（`DownloadMedia`）而不是把 URL 持久化。
4. **URL 回调必须 5 秒内响应**。处理函数只做验签+解密+入队，业务在 worker 里跑；
   超时会触发重投（三次）。
5. **素材只能上传不能直接发 URL**。一条消息只能带一个媒体，正文与媒体是两条
   独立消息（`Caption=false`）。
6. **群消息正文的 `@机器人` 是纯文本**，没有结构化字段；适配器按"开头 `@` +
   空白"切分。若企业微信改了渲染格式，命令匹配会全部失配——`event_test.go`
   里的 `TestSplitLeadingMention` 固化了这个约定。
