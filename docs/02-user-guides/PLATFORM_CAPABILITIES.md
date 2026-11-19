# 平台能力矩阵

本文档列出各平台适配器对 platform 包可选接口与能力声明的实现情况。
插件作者在调用平台能力前，应先用对应的 `Get*` 辅助函数做运行时检查
（而非依赖本文档，因为框架版本可能滞后于适配器实现）。

## 双通道模型

平台能力通过两条互补通道暴露：

| 通道 | 入口 | 用途 | 示例 |
|---|---|---|---|
| **A：平台无关** | `platform.Sender` + 可选接口 | 消息收发、撤回、群管理、历史消息、公告等跨平台操作 | `ctx.GetPlatformSender()` → `platform.GetGroupManager(adapter)` |
| **B：平台特有** | `platform.APIProvider` | 平台独有 API（OneBot 扩展动作、QQ 频道管理、Milky 群管理动作等） | `platform.GetPlatformAPIAs[*onebot.Sender](sender)` |

规则：平台无关操作**始终**从通道 A 走；通道 B 仅用于平台特有 API。
两者互补，拿到通道 B 的句柄不影响通道 A 的使用（事件处理器中
`ctx.GetPlatformSender()` 始终可用）。

## 发送与消息操作

| 可选接口 | 辅助函数 | onebot | milky | qq | satori | discord | telegram |
|---|---|---|---|---|---|---|---|
| `Sender`（核心） | — | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `MessageDeleter` | `GetDeleter` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `MessageEditor` | `GetEditor` | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ |
| `ReactionSender` | `GetReactionSender` | ❌¹ | ✅ | ❌¹ | ✅ | ✅ | ❌ |
| `TypingNotifier` | `GetTypingNotifier` | ❌ | ❌ | ❌ | ❌ | ✅ | ❌ |
| `SessionNotifier` | `GetSessionNotifier` | ✅ | ❌ | ✅ | ❌ | ❌ | ✅ |

¹ OneBot/QQ 的表情回应通过平台 API 门面（`APIProvider`）暴露，不走标准接口。

## 群组管理

| 可选接口 | 辅助函数 | onebot | milky | qq | satori | discord | telegram |
|---|---|---|---|---|---|---|---|
| `GroupManager` | `GetGroupManager` | ✅ | ✅ | ✅¹ | ❌ | ❌ | ❌ |
| `GroupSettings` | `GetGroupSettings` | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| `GroupInfoProvider` | `GetGroupInfoProvider` | ❌ | ❌ | ✅² | ❌ | ❌ | ❌ |
| `AnnouncementManager` | `GetAnnouncementManager` | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| `MessageHistoryProvider` | `GetMessageHistoryProvider` | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |

¹ QQ：`KickMember` 走 2026-09 群成员批量移除接口（`permanent=true` 同时加入群黑名单），
`BanMember` 走 2026-08 群禁言接口；`SetAdmin` 暂不支持。相关接口目前仅白名单机器人可用。
² QQ：`GetGroupMemberList`/`GetGroupMember`/`GetGroupInfo` 均已支持；群黑名单查询/操作
属平台特有能力，通过通道 B（`openapi.OpenAPI`）调用。`GroupInfo.UnionID` /
`GroupMemberInfo.UnionID` / `GroupMemberInfo.AvatarURL` 由 2026-11 起的
`union_group_openid` / `union_openid` / 头像字段填充（需绑定 3 方应用才有 union 值）。

## 请求处理与账号

| 可选接口 | 辅助函数 | onebot | milky | qq | satori | discord | telegram |
|---|---|---|---|---|---|---|---|
| `InvitationHandler` | `GetInvitationHandler` | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| `AutoModerator` | `GetAutoModerator` | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| `AvatarProvider` | `GetAvatarProvider` | ✅¹ | ❌ | ✅² | ✅³ | ✅⁴ | ✅⁵ |
| `UserInfoProvider` | `GetUserInfoProvider` | ✅¹ | ✅⁶ | ✅² | ✅³ | ✅⁴ | ✅⁵ |

¹ OneBot：头像是 LLB/NapCat 扩展动作 `get_qq_avatar`（未实现的协议端返回 404）；
资料用标准动作 `get_stranger_info`，没有任何 union/主动推送语义。
² QQ：走 2026-11 新增的「获取用户基本信息」接口（`GET /v2/users/{user_openid}/info`），
一次返回昵称、头像、`union_openid` 与 `allow_proactive_msg`；用户与机器人无好友关系时
平台返回 40184001。`AvatarProvider` 复用同一接口，仅取 `avatar` 字段。
³ Satori：`user.get`，名称优先级 Nick → Name，头像取 `avatar` 字段。
⁴ Discord：`GET /users/{id}`；头像由 SDK 的 `AvatarURL` 拼接，仅在用户设置了自定义
头像时填充（否则为空，不回退默认头像），没有 union/主动推送语义。
⁵ Telegram：`getChat`（私聊时 chat_id 即 user id）取昵称；头像需 `ChatPhoto.big_file_id`
再走 `getFile` 换 `file_path`，因此 `AvatarProvider` 会发两次请求，且返回的 URL 里含 bot token。
⁶ Milky：`get_user_profile` 只有昵称等资料，**协议没有头像字段**，因此只实现
`UserInfoProvider`，不声明 `AvatarProvider`。

## 适配器级能力

| 可选接口 | onebot | milky | qq | satori | discord | telegram |
|---|---|---|---|---|---|---|
| `RecoverableAdapter` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `BotIdentity` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `HealthDetailer` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `APIProvider`（平台 API 门面） | ✅ → `*onebot.Sender` | ✅ → `*milky.Adapter` | ✅ → `openapi.OpenAPI` | ✅ → `*satori.Client` | ✅ → `*discordgo.Session` | ✅ → `*telegram.Client` |

## 消息能力声明（Capabilities）

| 能力 | onebot | milky | qq | satori | discord | telegram |
|---|---|---|---|---|---|---|
| Markdown | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ |
| Buttons | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ |
| MultiAttachment | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ |
| MessageEdit | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ |
| MessageDelete | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Embeds | ❌ | ❌ | ✅ | ❌ | ✅ | ❌ |
| FileUpload | ❌ | ✅ | ✅ | ✅ | ✅ | ✅ |
| GuildSupport | ❌ | ❌ | ✅ | ✅ | ✅ | ❌ |
| Reactions | ❌ | ✅ | ❌ | ✅ | ✅ | ❌ |
| ThreadReply | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| TypingIndicator | ❌ | ❌ | ❌ | ❌ | ✅ | ❌ |
| MentionAll | ✅ | ✅ | ❌ | ✅ | ✅ | ❌ |
| VoiceChannel | ❌ | ❌ | ❌ | ❌ | ✅ | ❌ |
| Caption | ✅ | ❌ | ✅ | ✅ | ✅ | ✅ |

> 注：Capabilities 为适配器声明，可能与实际运行环境（如 QQ 官方 bot 的权限）
> 有出入；`Has()` 检查与 `Get*` 接口检查应结合使用。
>
> Markdown 平台的"Markdown + 附件"同条消息由 sender 层负责落地：
> Telegram/Discord/终端将 Markdown 文本作为 caption/content 渲染；
> QQ 官方将图片以原生 markdown 图片语法内嵌（本地图片先经分片上传换取
> 公网 `raw_url`，markdown 权限缺失时回退 msg_type=7 富媒体图文混排）；
> Satori 在发送前通过 `upload.create` 上传二进制附件并回填 `<img>` 元素。
> QQ 的 Embeds 是有损映射：群聊/单聊映射为图文卡片（`msg_type=8`，只有 title/description/url/pic_url，
> 因此 Fields/Color/Footer/Timestamp 会丢弃，缺 URL 或图片时降级为 Markdown/文本），
> 频道映射为原生 `embed`（title + prompt + thumbnail + fields）。
