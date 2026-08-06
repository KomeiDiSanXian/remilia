package platform

// optional.go — 可选平台管理接口
//
// 包含各平台按能力可选实现的接口，以及统一的"不支持"哨兵错误。
//
// 接口列表：
//   - [GroupManager]       — 群成员管理（踢人、禁言、设置管理员）
//   - [InvitationHandler]  — 好友/群邀请处理
//   - [AutoModerator]      — 内容自动审核（删除他人消息、全员禁言）
//   - [GroupInfoProvider]  — 群组信息查询（只读）
//   - [AvatarProvider]     — 用户头像查询
//   - [SessionNotifier]    — 主动推送（无需事件上下文）
//
// 使用前均需用 Get* 辅助函数做类型断言检查，平台不支持时返回 [ErrNotSupported]。

import (
	stdctx "context"
	"errors"
	"time"
)

// ErrNotSupported 当平台不支持某个可选管理操作时，实现方应返回此错误。
//
// 示例（WeChat Sender 实现 GroupManager 接口但不支持 SetAdmin）：
//
//	func (s *wechatSender) SetAdmin(_ stdctx.Context, _, _ string, _ bool) error {
//	    return platform.ErrNotSupported
//	}
var ErrNotSupported = errors.New("operation not supported by this platform")

// ────────────────────────────────────────────────────────────────────────────
// GroupManager（可选接口）
// ────────────────────────────────────────────────────────────────────────────

// GroupManager 可选接口：支持群成员管理操作的平台适配器 Sender 实现此接口。
//
// 不同平台对群管理的支持程度不同：
//   - QQ：支持全部操作（通过 OneBot 协议）
//   - Discord：支持踢出/禁言（通过 Guild 管理 API）
//   - Telegram：支持踢出成员（ban/unban）
//   - WeChat：通常不支持（返回 ErrNotSupported）
//
// 使用前用 [GetGroupManager] 检查支持：
//
//	if gm, ok := platform.GetGroupManager(adapter); ok {
//	    _ = gm.KickMember(ctx, groupID, userID, false)
//	}
type GroupManager interface {
	// KickMember 将指定用户踢出群组。
	//
	// permanent=true 时拉黑（禁止重新加入），false 时仅踢出。
	// 平台不支持 permanent 时应忽略并返回 nil。
	KickMember(ctx stdctx.Context, groupID, userID string, permanent bool) error

	// BanMember 禁言/解禁指定用户。
	//
	// duration 为禁言时长；传入 0 表示解除禁言。
	BanMember(ctx stdctx.Context, groupID, userID string, duration time.Duration) error

	// SetAdmin 授予/撤销群内管理员身份。
	//
	// isAdmin=true 授予管理员；isAdmin=false 撤销。
	// 平台不支持时返回 [ErrNotSupported]。
	SetAdmin(ctx stdctx.Context, groupID, userID string, isAdmin bool) error
}

// GetGroupManager 安全获取适配器 Sender 的群成员管理接口。
//
// 若 Sender 未实现 [GroupManager]，返回 (nil, false)。
func GetGroupManager(a Adapter) (GroupManager, bool) {
	gm, ok := a.Sender().(GroupManager)
	return gm, ok
}

// ────────────────────────────────────────────────────────────────────────────
// InvitationHandler（可选接口）
// ────────────────────────────────────────────────────────────────────────────

// InvitationHandler 可选接口：支持处理好友/群邀请请求的平台适配器 Sender 实现此接口。
//
// 使用前用 [GetInvitationHandler] 检查支持：
//
//	if ih, ok := platform.GetInvitationHandler(adapter); ok {
//	    _ = ih.AcceptGroupInvite(ctx, inviteID)
//	}
type InvitationHandler interface {
	// AcceptGroupInvite 接受群组邀请（inviteID 来自群邀请事件）。
	AcceptGroupInvite(ctx stdctx.Context, inviteID string) error

	// RejectGroupInvite 拒绝群组邀请（reason 不支持时忽略）。
	RejectGroupInvite(ctx stdctx.Context, inviteID, reason string) error

	// AcceptFriendRequest 接受好友申请（requestID 来自好友申请事件）。
	AcceptFriendRequest(ctx stdctx.Context, requestID string) error

	// RejectFriendRequest 拒绝好友申请。
	RejectFriendRequest(ctx stdctx.Context, requestID, reason string) error
}

// GetInvitationHandler 安全获取适配器 Sender 的邀请处理接口。
//
// 若 Sender 未实现 [InvitationHandler]，返回 (nil, false)。
func GetInvitationHandler(a Adapter) (InvitationHandler, bool) {
	ih, ok := a.Sender().(InvitationHandler)
	return ih, ok
}

// ────────────────────────────────────────────────────────────────────────────
// AutoModerator（可选接口）
// ────────────────────────────────────────────────────────────────────────────

// AutoModerator 可选接口：支持自动化内容审核/撤回的平台适配器 Sender 实现此接口。
//
// 与 [MessageDeleter] 的区别：
//   - MessageDeleter：撤回**机器人自己**发送的消息
//   - AutoModerator：撤回/屏蔽**他人**发送的消息（需管理员权限）
//
// 使用前用 [GetAutoModerator] 检查支持：
//
//	if am, ok := platform.GetAutoModerator(adapter); ok {
//	    _ = am.DeleteMemberMessage(ctx, groupID, messageID)
//	}
type AutoModerator interface {
	// DeleteMemberMessage 删除群成员发送的消息（机器人需有管理员权限）。
	DeleteMemberMessage(ctx stdctx.Context, groupID, messageID string) error

	// MuteAll 开启/关闭全体禁言（mute=true 开启，false 解除）。
	// 平台不支持时返回 [ErrNotSupported]。
	MuteAll(ctx stdctx.Context, groupID string, mute bool) error
}

// GetAutoModerator 安全获取适配器 Sender 的自动审核接口。
//
// 若 Sender 未实现 [AutoModerator]，返回 (nil, false)。
func GetAutoModerator(a Adapter) (AutoModerator, bool) {
	am, ok := a.Sender().(AutoModerator)
	return am, ok
}

// ────────────────────────────────────────────────────────────────────────────
// GroupInfoProvider（可选接口）
// ────────────────────────────────────────────────────────────────────────────

// GroupInfo 群组基本信息。
//
// 由 [GroupInfoProvider.GetGroupInfo] 返回；各平台填充能力不同，
// 未知字段返回零值。
type GroupInfo struct {
	// ID 群组唯一标识
	ID string
	// Name 群组名称
	Name string
	// MemberCount 成员数量（平台不提供时为 0）
	MemberCount int
	// Description 群组简介（平台不提供时为空字符串）
	Description string
}

// GroupMemberInfo 群成员信息。
//
// 由 [GroupInfoProvider.GetGroupMemberList] / [GroupInfoProvider.GetGroupMember] 返回。
type GroupMemberInfo struct {
	// UserID 用户唯一标识
	UserID string
	// DisplayName 群内昵称（优先于用户昵称）；若平台不提供则为用户全局昵称
	DisplayName string
	// GroupRole 成员角色（普通/管理/群主）
	GroupRole GroupRole
	// JoinedAt 加入群组的时间（平台不提供时为零值）
	JoinedAt time.Time
	// AvatarURL 用户头像 URL（平台不提供时为空字符串）
	AvatarURL string
}

// GroupInfoProvider 可选接口：支持查询群组信息的平台适配器 Sender 实现此接口。
//
// 与 [GroupManager]（成员管理）独立——GroupManager 用于"写"操作（踢人/禁言），
// GroupInfoProvider 用于"读"操作（查询成员列表、群名等）。
//
// 使用前用 [GetGroupInfoProvider] 检查支持：
//
//	if gip, ok := platform.GetGroupInfoProvider(adapter); ok {
//	    members, err := gip.GetGroupMemberList(ctx, groupID)
//	}
type GroupInfoProvider interface {
	// GetGroupInfo 查询群组基本信息（名称、成员数等）。
	//
	// 平台不支持时返回 [ErrNotSupported]。
	GetGroupInfo(ctx stdctx.Context, groupID string) (GroupInfo, error)

	// GetGroupMemberList 获取群所有成员的基本信息列表。
	//
	// 大型群组下列表可能很长，各平台可能有分页限制。
	// 平台不支持时返回 [ErrNotSupported]。
	GetGroupMemberList(ctx stdctx.Context, groupID string) ([]GroupMemberInfo, error)

	// GetGroupMember 查询指定群成员的详细信息。
	//
	// 成员不存在或被踢出时返回错误；平台不支持时返回 [ErrNotSupported]。
	GetGroupMember(ctx stdctx.Context, groupID, userID string) (GroupMemberInfo, error)

	// GetJoinedGroups 返回机器人当前已加入的群组 ID 列表。
	//
	// 用于定时推送（整点报时、每日新闻）等需要枚举目标群的场景。
	// 平台不支持时返回 [ErrNotSupported]。
	GetJoinedGroups(ctx stdctx.Context) ([]GroupInfo, error)
}

// GetGroupInfoProvider 安全获取适配器 Sender 的群组信息查询接口。
//
// 若 Sender 未实现 [GroupInfoProvider]，返回 (nil, false)。
//
// 使用示例：
//
//	if gip, ok := platform.GetGroupInfoProvider(adapter); ok {
//	    members, err := gip.GetGroupMemberList(ctx, groupID)
//	    for _, m := range members {
//	        fmt.Println(m.DisplayName, m.UserID)
//	    }
//	}
func GetGroupInfoProvider(a Adapter) (GroupInfoProvider, bool) {
	gip, ok := a.Sender().(GroupInfoProvider)
	return gip, ok
}

// ────────────────────────────────────────────────────────────────────────────
// AvatarProvider（可选接口）
// ────────────────────────────────────────────────────────────────────────────

// AvatarProvider 可选接口：支持获取用户/群组头像 URL 的平台适配器 Sender 实现此接口。
//
// 适用于需要在消息中展示用户头像的场景（如 /头像 命令、个人资料卡片）。
//
// 使用前用 [GetAvatarProvider] 检查支持：
//
//	if ap, ok := platform.GetAvatarProvider(adapter); ok {
//	    url, err := ap.GetUserAvatarURL(ctx, userID)
//	}
type AvatarProvider interface {
	// GetUserAvatarURL 获取指定用户的头像 URL。
	// userID 为平台用户唯一 ID。
	// 平台不支持时返回 [ErrNotSupported]。
	GetUserAvatarURL(ctx stdctx.Context, userID string) (string, error)
}

// GetAvatarProvider 安全获取适配器 Sender 的头像查询接口。
//
// 若 Sender 未实现 [AvatarProvider]，返回 (nil, false)。
func GetAvatarProvider(a Adapter) (AvatarProvider, bool) {
	ap, ok := a.Sender().(AvatarProvider)
	return ap, ok
}

// ────────────────────────────────────────────────────────────────────────────
// SessionNotifier（可选接口）
// ────────────────────────────────────────────────────────────────────────────

// SessionNotifier 可选接口：支持主动向任意用户/群组推送消息的平台适配器 Sender 实现此接口。
//
// 与普通 [Sender] 的区别：
//   - Sender.Send 需要 ChatInfo（通常包含被动回复令牌），依赖事件上下文
//   - SessionNotifier 仅凭 userID/groupID 字符串即可主动发起推送，无需事件上下文
//
// 典型使用场景：
//   - 漂流瓶：捞起时跨群通知原投递者（无事件上下文）
//   - 提醒系统：定时向指定用户发送私信
//   - 跨群广播：向特定群主动推送信息
//
// 使用前用 [GetSessionNotifier] 检查支持：
//
//	if sn, ok := platform.GetSessionNotifier(adapter); ok {
//	    _ = sn.NotifyUser(ctx, userID, platform.TextMessage("你的漂流瓶被捞起了！"))
//	}
type SessionNotifier interface {
	// NotifyUser 向指定用户（私聊会话）主动推送一条消息。
	// userID 为平台用户唯一 ID（如 QQ 号字符串）。
	// 平台不支持主动私信时返回 [ErrNotSupported]。
	NotifyUser(ctx stdctx.Context, userID string, msg OutboundMessage) error

	// NotifyGroup 向指定群组主动推送一条消息。
	// groupID 为平台群组唯一 ID。
	// 机器人不在该群时应返回错误。
	NotifyGroup(ctx stdctx.Context, groupID string, msg OutboundMessage) error
}

// GetSessionNotifier 安全获取适配器 Sender 的主动推送接口。
//
// 若 Sender 未实现 [SessionNotifier]，返回 (nil, false)。
//
// 使用示例：
//
//	if sn, ok := platform.GetSessionNotifier(adapter); ok {
//	    _ = sn.NotifyUser(ctx, targetUserID, platform.TextMessage("消息来了！"))
//	}
func GetSessionNotifier(a Adapter) (SessionNotifier, bool) {
	sn, ok := a.Sender().(SessionNotifier)
	return sn, ok
}

// ────────────────────────────────────────────────────────────────────────────
// GroupSettings（可选接口）
// ────────────────────────────────────────────────────────────────────────────

// GroupSettings 可选接口：支持群资料管理的平台适配器 Sender 实现此接口。
//
// 覆盖群名称、群名片、专属头衔、退群等群资料操作，
// 与 [GroupManager]（成员管理）互补。
//
// 使用前用 [GetGroupSettings] 检查支持：
//
//	if gs, ok := platform.GetGroupSettings(adapter); ok {
//	    _ = gs.SetGroupName(ctx, groupID, "新群名")
//	}
type GroupSettings interface {
	// SetGroupName 修改群名称。
	// 平台不支持时返回 [ErrNotSupported]。
	SetGroupName(ctx stdctx.Context, groupID, name string) error

	// SetGroupCard 设置群成员名片（备注名）。
	// card 为空字符串时清除名片。
	// 平台不支持时返回 [ErrNotSupported]。
	SetGroupCard(ctx stdctx.Context, groupID, userID, card string) error

	// SetGroupSpecialTitle 设置群成员专属头衔。
	// title 为空字符串时清除头衔。
	// 平台不支持时返回 [ErrNotSupported]。
	SetGroupSpecialTitle(ctx stdctx.Context, groupID, userID, title string) error

	// LeaveGroup 退出群组；dismiss 为 true 时尝试解散群（仅群主）。
	// 平台不支持时返回 [ErrNotSupported]。
	LeaveGroup(ctx stdctx.Context, groupID string, dismiss bool) error
}

// GetGroupSettings 安全获取适配器 Sender 的群资料管理接口。
//
// 若 Sender 未实现 [GroupSettings]，返回 (nil, false)。
func GetGroupSettings(a Adapter) (GroupSettings, bool) {
	gs, ok := a.Sender().(GroupSettings)
	return gs, ok
}

// ────────────────────────────────────────────────────────────────────────────
// MessageHistoryProvider（可选接口）
// ────────────────────────────────────────────────────────────────────────────

// MessageHistoryProvider 可选接口：支持查询历史消息的平台适配器 Sender 实现此接口。
//
// 使用前用 [GetMessageHistoryProvider] 检查支持：
//
//	if hp, ok := platform.GetMessageHistoryProvider(adapter); ok {
//	    msgs, err := hp.GetGroupHistory(ctx, groupID, 20)
//	}
type MessageHistoryProvider interface {
	// GetGroupHistory 获取群历史消息。
	// chatID 为群会话 ID（取事件 Chat().ID 原样传入）；limit 为最大返回条数（0 使用平台默认值）。
	// 返回按时间从新到旧排列的消息快照列表。
	// 平台不支持时返回 [ErrNotSupported]。
	GetGroupHistory(ctx stdctx.Context, chatID string, limit int) ([]Message, error)

	// GetFriendHistory 获取好友（私聊）历史消息。
	// chatID 为用户会话 ID；limit 为最大返回条数（0 使用平台默认值）。
	// 平台不支持时返回 [ErrNotSupported]。
	GetFriendHistory(ctx stdctx.Context, chatID string, limit int) ([]Message, error)
}

// GetMessageHistoryProvider 安全获取适配器 Sender 的历史消息接口。
//
// 若 Sender 未实现 [MessageHistoryProvider]，返回 (nil, false)。
func GetMessageHistoryProvider(a Adapter) (MessageHistoryProvider, bool) {
	hp, ok := a.Sender().(MessageHistoryProvider)
	return hp, ok
}

// ────────────────────────────────────────────────────────────────────────────
// AnnouncementManager（可选接口）
// ────────────────────────────────────────────────────────────────────────────

// AnnouncementManager 可选接口：支持群公告的平台适配器 Sender 实现此接口。
//
// 使用前用 [GetAnnouncementManager] 检查支持：
//
//	if am, ok := platform.GetAnnouncementManager(adapter); ok {
//	    _ = am.SendAnnouncement(ctx, groupID, "公告内容")
//	}
type AnnouncementManager interface {
	// SendAnnouncement 发布群公告。
	// imageURL 为可选配图 URL，空字符串表示纯文字公告。
	// 平台不支持时返回 [ErrNotSupported]。
	SendAnnouncement(ctx stdctx.Context, groupID, content, imageURL string) error

	// GetAnnouncements 获取群公告列表。
	// 平台不支持时返回 [ErrNotSupported]。
	GetAnnouncements(ctx stdctx.Context, groupID string) ([]Announcement, error)
}

// Announcement 是群公告的跨平台摘要。
//
// 平台特有字段（如置顶、生效时间）通过 Extra 携带（key-value 形式）。
type Announcement struct {
	// ID 公告 ID（用于删除）。
	ID string
	// Content 公告内容。
	Content string
	// PublisherID 发布者用户 ID。
	PublisherID string
	// ImageURL 公告配图 URL（平台不提供时为空）。
	ImageURL string
	// Timestamp Unix 时间戳（秒）。
	Timestamp int64
	// Extra 平台特有扩展字段（key-value 形式）。
	Extra map[string]any
}

// GetAnnouncementManager 安全获取适配器 Sender 的群公告接口。
//
// 若 Sender 未实现 [AnnouncementManager]，返回 (nil, false)。
func GetAnnouncementManager(a Adapter) (AnnouncementManager, bool) {
	am, ok := a.Sender().(AnnouncementManager)
	return am, ok
}
