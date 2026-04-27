# QQ Bot API v2 适配器覆盖情况报告

> 文档生成时间：2026-03-21  
> 参考文档：https://bot.q.qq.com/wiki/develop/api-v2/

---

## 一、消息相关（server-inter/message）

### 1.1 消息收发

| API | 方法 | 端点 | 状态 | 说明 |
|-----|------|------|------|------|
| 单聊发消息 | POST | `/v2/users/{openid}/messages` | ✅ 已实现 | `SingleChat()` |
| 群聊发消息 | POST | `/v2/groups/{group_openid}/messages` | ✅ 已实现 | `GroupChat()` |
| 文字子频道发消息 | POST | `/channels/{channel_id}/messages` | ✅ 已实现 | `ChannelChat()` |
| 频道私信发消息 | POST | `/dms/{guild_id}/messages` | ✅ 已实现（本次新增）| `DMChat()` |
| 单聊富媒体上传 | POST | `/v2/users/{openid}/files` | ✅ 已实现 | `SingleRichMedia()` |
| 群聊富媒体上传 | POST | `/v2/groups/{group_openid}/files` | ✅ 已实现 | `GroupRichMedia()` |
| 单聊撤回消息 | DELETE | `/v2/users/{openid}/messages/{msg_id}` | ✅ 已实现 | `SingleReset()` |
| 群聊撤回消息 | DELETE | `/v2/groups/{group_openid}/messages/{msg_id}` | ✅ 已实现 | `GroupReset()` |
| 文字子频道撤回消息 | DELETE | `/channels/{channel_id}/messages/{msg_id}` | ✅ 已实现 | `ChannelReset()`，仅私域 |
| 频道私信撤回消息 | DELETE | `/dms/{guild_id}/messages/{msg_id}` | ✅ 已实现 | `DMReset()`，仅私域 |

### 1.2 消息事件

| 事件类型 | 触发场景 | 状态 | platform.EventKind |
|---------|---------|------|-------------------|
| `C2C_MESSAGE_CREATE` | 用户在单聊发送消息 | ✅ 已实现 | `EventKindPrivateMessage` |
| `GROUP_AT_MESSAGE_CREATE` | 用户在群内 @ 机器人 | ✅ 已实现 | `EventKindGroupMessage` |
| `AT_MESSAGE_CREATE` | 用户在文字子频道 @ 机器人 | ✅ 已实现 | `EventKindGuildMessage` |
| `MESSAGE_CREATE` | 文字子频道全量消息（私域） | ✅ 已实现 | `EventKindGuildMessage` |
| `DIRECT_MESSAGE_CREATE` | 用户在频道私信发消息 | ✅ 已实现（本次修复）| `EventKindGuildMessage`，`IsDM=true` |
| `MESSAGE_DELETE` | 频道消息撤回 | ✅ 已实现（本次新增）| `EventKindMessageDelete` |

> **本次修复**：`DIRECT_MESSAGE_CREATE` 事件此前路由到 `/channels/{id}/messages`（错误），现已修复为使用 `DMChat()` → `/dms/{guild_id}/messages`，并在 `ChatInfo.IsDM=true` 时触发该路径。

### 1.3 消息交互

| API | 方法 | 端点 | 状态 | 说明 |
|-----|------|------|------|------|
| 回应互动事件 | PUT | `/interactions/{interaction_id}` | ✅ 已实现 | `RespondInteraction()` |
| `INTERACTION_CREATE` 事件 | — | — | ✅ 已实现 | `EventKindInteraction` |

---

## 二、用户模块（server-inter/user）

### 2.1 用户管理事件

| 事件类型 | 触发场景 | 状态 | platform.EventKind |
|---------|---------|------|-------------------|
| `FRIEND_ADD` | 用户添加机器人为好友 | ✅ 已实现 | `EventKindMemberJoin` |
| `FRIEND_DEL` | 用户删除机器人好友 | ✅ 已实现 | `EventKindMemberLeave` |
| `C2C_MSG_REJECT` | 用户拒绝机器人主动消息 | ✅ 已实现 | `EventKindNotice` |
| `C2C_MSG_RECEIVE` | 用户允许机器人主动消息 | ✅ 已实现 | `EventKindNotice` |

> **机器人链接**（`share_url`）为前端能力，无对应服务端接口，不需要实现。

---

## 三、群聊模块（server-inter/group）

### 3.1 群管理事件

| 事件类型 | 触发场景 | 状态 | platform.EventKind |
|---------|---------|------|-------------------|
| `GROUP_ADD_ROBOT` | 机器人加入群聊 | ✅ 已实现 | `EventKindMemberJoin` |
| `GROUP_DEL_ROBOT` | 机器人退出群聊 | ✅ 已实现 | `EventKindMemberLeave` |
| `GROUP_MSG_REJECT` | 群拒绝机器人主动消息 | ✅ 已实现 | `EventKindNotice` |
| `GROUP_MSG_RECEIVE` | 群允许机器人主动消息 | ✅ 已实现 | `EventKindNotice` |

---

## 四、频道模块（server-inter/channel）

### 4.1 频道管理（频道/子频道 CRUD）

| API | 方法 | 端点 | 状态 | 说明 |
|-----|------|------|------|------|
| 获取机器人详情 | GET | `/users/@me` | ✅ 已实现（本次新增）| `GetMe()` |
| 获取机器人频道列表 | GET | `/users/@me/guilds` | ✅ 已实现（本次新增）| `GetMyGuilds()` |
| 获取频道详情 | GET | `/guilds/{guild_id}` | ✅ 已实现（本次新增）| `GetGuild()` |
| 获取子频道列表 | GET | `/guilds/{guild_id}/channels` | ✅ 已实现（本次新增）| `GetGuildChannels()` |
| 获取子频道详情 | GET | `/channels/{channel_id}` | ✅ 已实现（本次新增）| `GetChannel()` |
| 创建子频道 | POST | `/guilds/{guild_id}/channels` | ✅ 已实现（本次新增）| `CreateGuildChannel()`，仅私域 |
| 修改子频道 | PATCH | `/channels/{channel_id}` | ✅ 已实现（本次新增）| `UpdateGuildChannel()`，仅私域 |
| 删除子频道 | DELETE | `/channels/{channel_id}` | ✅ 已实现（本次新增）| `DeleteGuildChannel()`，仅私域 |
| 创建频道私信会话 | POST | `/users/@me/dms` | ✅ 已实现（本次新增）| `CreateDirectMessageSession()` |

### 4.2 频道管理事件

| 事件类型 | 触发场景 | 状态 | platform.EventKind |
|---------|---------|------|-------------------|
| `GUILD_CREATE` | 机器人加入频道 | ✅ 已实现（本次新增）| `EventKindMemberJoin` |
| `GUILD_UPDATE` | 频道信息变更 | ✅ 已实现（本次新增）| `EventKindNotice` |
| `GUILD_DELETE` | 机器人退出频道 | ✅ 已实现（本次新增）| `EventKindMemberLeave` |
| `GUILD_MEMBER_ADD` | 频道成员加入 | ✅ 已实现（本次新增）| `EventKindMemberJoin` |
| `GUILD_MEMBER_UPDATE` | 频道成员信息更新 | ✅ 已实现（本次新增）| `EventKindNotice` |
| `GUILD_MEMBER_REMOVE` | 频道成员移除 | ✅ 已实现（本次新增）| `EventKindMemberLeave` |
| `CHANNEL_CREATE` | 子频道创建 | ✅ 已实现（本次新增）| `EventKindNotice` |
| `CHANNEL_UPDATE` | 子频道更新 | ✅ 已实现（本次新增）| `EventKindNotice` |
| `CHANNEL_DELETE` | 子频道删除 | ✅ 已实现（本次新增）| `EventKindNotice` |

### 4.3 频道成员（仅私域机器人）

| API | 方法 | 端点 | 状态 | 说明 |
|-----|------|------|------|------|
| 获取在线成员数 | GET | `/channels/{channel_id}/online_nums` | ✅ 已实现 | `GetChannelOnlineNums()` |
| 获取成员列表 | GET | `/guilds/{guild_id}/members` | ✅ 已实现 | `GetGuildMembers()` |
| 获取身份组成员列表 | GET | `/guilds/{guild_id}/roles/{role_id}/members` | ✅ 已实现 | `GetGuildRoleMembers()` |
| 获取成员详情 | GET | `/guilds/{guild_id}/members/{user_id}` | ✅ 已实现 | `GetGuildMember()` |
| 删除成员 | DELETE | `/guilds/{guild_id}/members/{user_id}` | ✅ 已实现 | `DeleteGuildMember()` |

### 4.4 频道身份组与权限管理（仅私域机器人）

| API | 方法 | 端点 | 状态 | 说明 |
|-----|------|------|------|------|
| 获取身份组列表 | GET | `/guilds/{guild_id}/roles` | ✅ 已实现 | `GetGuildRoles()` |
| 创建身份组 | POST | `/guilds/{guild_id}/roles` | ✅ 已实现 | `CreateGuildRole()` |
| 修改身份组 | PATCH | `/guilds/{guild_id}/roles/{role_id}` | ✅ 已实现 | `UpdateGuildRole()` |
| 删除身份组 | DELETE | `/guilds/{guild_id}/roles/{role_id}` | ✅ 已实现 | `DeleteGuildRole()` |
| 添加成员到身份组 | PUT | `/guilds/{guild_id}/members/{user_id}/roles/{role_id}` | ✅ 已实现 | `AddGuildMemberRole()` |
| 从身份组移除成员 | DELETE | `/guilds/{guild_id}/members/{user_id}/roles/{role_id}` | ✅ 已实现 | `DeleteGuildMemberRole()` |
| 获取子频道成员权限 | GET | `/channels/{channel_id}/members/{user_id}/permissions` | ✅ 已实现 | `GetChannelMemberPermissions()` |
| 修改子频道成员权限 | PUT | `/channels/{channel_id}/members/{user_id}/permissions` | ✅ 已实现 | `UpdateChannelMemberPermissions()` |
| 获取子频道身份组权限 | GET | `/channels/{channel_id}/roles/{role_id}/permissions` | ✅ 已实现 | `GetChannelRolePermissions()` |
| 修改子频道身份组权限 | PUT | `/channels/{channel_id}/roles/{role_id}/permissions` | ✅ 已实现 | `UpdateChannelRolePermissions()` |

### 4.5 接口授权管理（仅私域机器人）

| API | 方法 | 端点 | 状态 | 说明 |
|-----|------|------|------|------|
| 获取已授权接口列表 | GET | `/guilds/{guild_id}/api_permission` | ✅ 已实现 | `GetGuildAPIPermissions()` |
| 发送授权链接 | POST | `/guilds/{guild_id}/api_permission/demand` | ✅ 已实现 | `RequestGuildAPIPermission()` |

### 4.6 发言管理（仅私域机器人）

| API | 方法 | 端点 | 状态 | 说明 |
|-----|------|------|------|------|
| 获取频道消息频率设置 | GET | `/guilds/{guild_id}/message/setting` | ✅ 已实现 | `GetGuildMessageSetting()` |
| 频道全员禁言/解禁 | PATCH | `/guilds/{guild_id}/mute` | ✅ 已实现 | `MuteGuild()` |
| 禁言指定成员 | PATCH | `/guilds/{guild_id}/members/{user_id}/mute` | ✅ 已实现 | `MuteGuildMember()` |
| 批量禁言成员 | PATCH | `/guilds/{guild_id}/mute`（带 user_ids）| ✅ 已实现 | `MuteGuildMultiMembers()` |

### 4.7 内容管理（仅私域机器人）

| API | 方法 | 端点 | 状态 | 说明 |
|-----|------|------|------|------|
| 创建频道公告 | POST | `/guilds/{guild_id}/announces` | ✅ 已实现 | `CreateGuildAnnounce()` |
| 删除频道公告 | DELETE | `/guilds/{guild_id}/announces/{message_id}` | ✅ 已实现 | `DeleteGuildAnnounce()` |
| 添加精华消息 | PUT | `/channels/{channel_id}/pins/{message_id}` | ✅ 已实现 | `PinMessage()` |
| 删除精华消息 | DELETE | `/channels/{channel_id}/pins/{message_id}` | ✅ 已实现 | `UnpinMessage()` |
| 获取精华消息列表 | GET | `/channels/{channel_id}/pins` | ✅ 已实现 | `GetPinnedMessages()` |
| 获取日程列表 | GET | `/channels/{channel_id}/schedules` | ✅ 已实现 | `GetChannelSchedules()` |
| 获取日程详情 | GET | `/channels/{channel_id}/schedules/{schedule_id}` | ✅ 已实现 | `GetChannelSchedule()` |
| 创建日程 | POST | `/channels/{channel_id}/schedules` | ✅ 已实现 | `CreateChannelSchedule()` |
| 修改日程 | PATCH | `/channels/{channel_id}/schedules/{schedule_id}` | ✅ 已实现 | `UpdateChannelSchedule()` |
| 删除日程 | DELETE | `/channels/{channel_id}/schedules/{schedule_id}` | ✅ 已实现 | `DeleteChannelSchedule()` |
| 音频控制 | POST | `/channels/{channel_id}/audio` | ✅ 已实现 | `AudioControl()` |
| 机器人上麦 | PUT | `/channels/{channel_id}/mic` | ✅ 已实现 | `BotOnMic()` |
| 机器人下麦 | DELETE | `/channels/{channel_id}/mic` | ✅ 已实现 | `BotOffMic()` |
| 获取帖子列表 | GET | `/channels/{channel_id}/threads` | ✅ 已实现 | `GetThreadList()` |
| 获取帖子详情 | GET | `/channels/{channel_id}/threads/{thread_id}` | ✅ 已实现 | `GetThread()` |
| 发表帖子 | PUT | `/channels/{channel_id}/threads` | ✅ 已实现 | `CreateThread()` |
| 删除帖子 | DELETE | `/channels/{channel_id}/threads/{thread_id}` | ✅ 已实现 | `DeleteThread()` |

---

## 五、本次变更汇总

### 新增

| 文件 | 变更内容 |
|------|---------|
| `platform/event.go` | `ChatInfo` 新增 `IsDM bool` 字段，用于区分频道私信与普通子频道消息 |
| `platform/qq/openapi/constant/constant.go` | 新增 `UsersMeURL`、`UsersMeGuildsURL`、`UsersMeDMsURL`、`GuildURL`、`GuildChannelsURL`、`ChannelURL`、`DMChatURL` 共 7 个常量 |
| `platform/qq/openapi/dto/guild.go` | 新增 `ChannelRequest`（子频道创建/修改请求体）、`DirectMessageSessionRequest`（创建私信会话请求体）|
| `platform/qq/openapi/iface.go` | 新增 `DMChat`、`GetMe`、`GetMyGuilds`、`GetGuild`、`GetGuildChannels`、`GetChannel`、`CreateGuildChannel`、`UpdateGuildChannel`、`DeleteGuildChannel`、`CreateDirectMessageSession` 共 10 个接口方法 |
| `platform/qq/openapi/openapi.go` | 实现上述 10 个新接口方法 |
| `platform/qq/openapi/dto/event.go` | 取消注释并完善 `GuildEvent`、`GuildMemberEvent`、`ChannelEvent` 及其子类型；新增 `MessageDeleteEventData` |
| `platform/qq/event.go` | 新增 `GuildCreate/Update/Delete`、`GuildMemberAdd/Update/Remove`、`ChannelCreate/Update/Delete`、`MessageDeleteEvent` 共 10 种事件的处理；新增 `populateGuildEvent`、`populateGuildMemberEvent`、`populateChannelEvent`、`populateMessageDelete` 函数 |
| `testbot/testbot.go` | `MockAPI` 补全所有新增接口方法 |

### 修复

| 文件 | Bug 描述 | 修复方式 |
|------|---------|---------|
| `platform/qq/sender.go` | `DIRECT_MESSAGE_CREATE` 事件的回复被错误路由到 `/channels/{channel_id}/messages`，应使用 `/dms/{guild_id}/messages` | `sendGuildChannelMessage` 检查 `chat.IsDM`，若为 `true` 则调用 `DMChat(chat.ID, ...)` |
| `platform/qq/event.go` | `DIRECT_MESSAGE_CREATE` 未设置 `IsDM=true`，导致发送路由无法区分 DM 和普通频道 | `populateGuildMessage` 接受 `evType` 参数，当 `evType == DirectMessageCreate` 时设置 `chat.IsDM = true`，且 `chat.ID = guild_id`（私信会话 ID）|

