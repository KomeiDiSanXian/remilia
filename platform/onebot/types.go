// Package onebot 实现了 remilia 框架的 OneBot V11 协议适配器。
//
// OneBot V11 是 QQ 机器人开发的标准协议，被 go-cqhttp、NapCat 和 Lagrange 等实现所支持。
//
// 通信方式：
//   - ForwardWS（默认）：适配器主动连接 OneBot 实现的 WS 服务端
//   - ReverseWS：由 OneBot 实现反向连接到适配器的 WS 服务端
//   - HTTPPost：OneBot 实现通过 HTTP POST 向适配器上报事件
//
// 使用示例（ForwardWS）：
//
//	adapter := onebot.NewForwardWSAdapter(onebot.Config{
//	    URL:   "ws://127.0.0.1:6700",
//	    Token: "your-access-token",
//	})
//	bot, _ := remilia.NewBotBuilder().WithPlatformAdapter(adapter).Build()
package onebot

import "encoding/json"

// ────────────────────────────────────────────────────────────────────────────
// 上报类型常量
// ────────────────────────────────────────────────────────────────────────────

const (
	PostTypeMessage     = "message"
	PostTypeMessageSent = "message_sent"
	PostTypeNotice      = "notice"
	PostTypeRequest     = "request"
	PostTypeMetaEvent   = "meta_event"
)

// 消息类型常量
const (
	MessageTypePrivate = "private"
	MessageTypeGroup   = "group"
)

// 私聊消息子类型常量
const (
	PrivateSubTypeFriend = "friend"
	PrivateSubTypeGroup  = "group" // 临时群会话
	PrivateSubTypeOther  = "other"
)

// 群消息子类型常量
const (
	GroupSubTypeNormal    = "normal"
	GroupSubTypeAnonymous = "anonymous"
	GroupSubTypeNotice    = "notice"
)

// 通知类型常量
const (
	NoticeTypeGroupUpload   = "group_upload"
	NoticeTypeGroupAdmin    = "group_admin"
	NoticeTypeGroupDecrease = "group_decrease"
	NoticeTypeGroupIncrease = "group_increase"
	NoticeTypeGroupBan      = "group_ban"
	NoticeTypeFriendAdd     = "friend_add"
	NoticeTypeGroupRecall   = "group_recall"
	NoticeTypeFriendRecall  = "friend_recall"
	// NoticeTypeFriendRemove 为宽容解析预留：OneBot 11 标准（onebot.dev/11）、
	// NapCat、go-cqhttp、LLOneBot/LuckyLilliaBot、Lagrange.OneBot 均无
	// friend_remove 通知事件（好友删除只有 delete_friend 动作，无通知）。
	// 2026-08 对照上述实现源码核验后确认查无出处，纯宽容预留。
	NoticeTypeFriendRemove = "friend_remove"
	NoticeTypeNotify       = "notify"

	// ── LLOneBot / LuckyLilliaBot 扩展通知类型 ────────────────────────────
	// 来源：github.com/LLOneBot/LuckyLilliaBot src/onebot11/event/notice/ 目录
	// （2026-08 核验）。标准 OneBot 11 中不存在，LLB 系协议端会发送。
	NoticeTypeGroupCard         = "group_card"           // 群名片变更：group_id, user_id, card_new, card_old
	NoticeTypeGroupDismiss      = "group_dismiss"        // 群解散：group_id, user_id
	NoticeTypeEssence           = "essence"              // 精华消息变更：sub_type=add/delete, group_id, message_id, sender_id, operator_id
	NoticeTypeGroupMsgEmojiLike = "group_msg_emoji_like" // 群消息表情回应：group_id, user_id, message_id, likes[], is_add
	NoticeTypeFlashFile         = "flash_file"           // 闪照：sub_type=downloading/downloaded/uploading/uploaded
)

// 通知子类型常量（notice_type=notify）
const (
	NotifySubTypePoke      = "poke"
	NotifySubTypeLuckyKing = "lucky_king" // go-cqhttp 遗留类型
	NotifySubTypeHonor     = "honor"      // go-cqhttp 遗留类型

	// ── LLOneBot / LuckyLilliaBot 扩展 notify 子类型 ───────────────────────
	NotifySubTypePokeRecall  = "poke_recall"  // 戳一戳撤回：user_id, target_id, raw_info
	NotifySubTypeTitle       = "title"        // 群头衔变更：group_id, user_id, title
	NotifySubTypeProfileLike = "profile_like" // 名片赞：user_id, operator_id, operator_nick, times
)

// 群成员减少子类型常量
const (
	GroupDecreaseLeave  = "leave"
	GroupDecreaseKick   = "kick"
	GroupDecreaseKickMe = "kick_me"
)

// 群成员增加子类型常量
const (
	GroupIncreaseApprove = "approve"
	GroupIncreaseInvite  = "invite"
)

// 群管理员变动子类型常量
const (
	GroupAdminSet   = "set"
	GroupAdminUnset = "unset"
)

// 群禁言子类型常量
const (
	GroupBanBan     = "ban"
	GroupBanLiftBan = "lift_ban"
)

// 请求类型常量
const (
	RequestTypeFriend = "friend"
	RequestTypeGroup  = "group"
)

// 群请求子类型常量
const (
	GroupRequestAdd    = "add"
	GroupRequestInvite = "invite"
)

// 元事件类型常量
const (
	MetaEventTypeLifecycle = "lifecycle"
	MetaEventTypeHeartbeat = "heartbeat"
)

// 生命周期子类型常量
const (
	LifecycleEnable  = "enable"
	LifecycleDisable = "disable"
	LifecycleConnect = "connect"
)

// 荣誉类型常量
const (
	HonorTypeTalkative = "talkative"
	HonorTypePerformer = "performer"
	HonorTypeEmotion   = "emotion"
)

// ────────────────────────────────────────────────────────────────────────────
// 通用结构
// ────────────────────────────────────────────────────────────────────────────

// BaseEvent 包含每个 OneBot V11 事件中的公共字段。
type BaseEvent struct {
	Time     int64  `json:"time"`
	SelfID   int64  `json:"self_id"`
	PostType string `json:"post_type"`
}

// PrivateSender 是私聊消息事件中内嵌的发送者信息。
type PrivateSender struct {
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
	Sex      string `json:"sex"` // male, female, unknown
	Age      int32  `json:"age"`
}

// GroupSender 是群消息事件中内嵌的发送者信息。
type GroupSender struct {
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
	Card     string `json:"card"` // 群名片／备注
	Sex      string `json:"sex"`  // male, female, unknown
	Age      int32  `json:"age"`
	Area     string `json:"area"`
	Level    string `json:"level"`
	Role     string `json:"role"`  // owner, admin, member
	Title    string `json:"title"` // 专属头衔
}

// AnonymousInfo 包含匿名发送者的信息。
type AnonymousInfo struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Flag string `json:"flag"` // 用于禁言匿名用户
}

// FileInfo 包含 group_upload 事件中的文件元数据。
type FileInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Busid int64  `json:"busid"`
}

// ────────────────────────────────────────────────────────────────────────────
// 消息事件
// ────────────────────────────────────────────────────────────────────────────

// PrivateMessageEvent 表示收到的私聊消息事件。
type PrivateMessageEvent struct {
	BaseEvent
	MessageType string        `json:"message_type"`
	SubType     string        `json:"sub_type"` // friend, group, other
	MessageID   int32         `json:"message_id"`
	MessageSeq  int32         `json:"message_seq,omitempty"` // 消息序列号
	RealID      int32         `json:"real_id,omitempty"`     // 真实消息 ID
	TargetID    int64         `json:"target_id,omitempty"`   // 机器人自身发出的消息目标
	TempSource  int32         `json:"temp_source,omitempty"` // 临时会话来源
	UserID      int64         `json:"user_id"`
	Message     MessageChain  `json:"message"`
	RawMessage  string        `json:"raw_message"`
	Font        int32         `json:"font"`
	Sender      PrivateSender `json:"sender"`
}

// GroupMessageEvent 表示收到的群聊消息事件。
type GroupMessageEvent struct {
	BaseEvent
	MessageType string         `json:"message_type"`
	SubType     string         `json:"sub_type"` // normal, anonymous, notice
	MessageID   int32          `json:"message_id"`
	MessageSeq  int32          `json:"message_seq,omitempty"` // 消息序列号
	RealID      int32          `json:"real_id,omitempty"`     // 真实消息 ID
	TargetID    int64          `json:"target_id,omitempty"`   // 机器人自身发出的消息目标
	GroupID     int64          `json:"group_id"`
	UserID      int64          `json:"user_id"`
	Anonymous   *AnonymousInfo `json:"anonymous"` // 非匿名时为 nil
	Message     MessageChain   `json:"message"`
	RawMessage  string         `json:"raw_message"`
	Font        int32          `json:"font"`
	Sender      GroupSender    `json:"sender"`
}

// ────────────────────────────────────────────────────────────────────────────
// 通知事件
// ────────────────────────────────────────────────────────────────────────────

// NoticeEvent 是覆盖所有 notice_type 值的通用通知事件。
//
// 与当前 notice_type 无关的字段将为零值/空值。
type NoticeEvent struct {
	BaseEvent
	NoticeType string    `json:"notice_type"`
	SubType    string    `json:"sub_type,omitempty"`
	GroupID    int64     `json:"group_id,omitempty"`
	UserID     int64     `json:"user_id,omitempty"`
	OperatorID int64     `json:"operator_id,omitempty"`
	MessageID  int64     `json:"message_id,omitempty"`
	Duration   int64     `json:"duration,omitempty"` // 秒，用于 group_ban
	TargetID   int64     `json:"target_id,omitempty"`
	HonorType  string    `json:"honor_type,omitempty"`
	File       *FileInfo `json:"file,omitempty"` // group_upload

	// ── LLOneBot / LuckyLilliaBot 扩展字段 ────────────────────────────────
	CardNew      string          `json:"card_new,omitempty"`      // group_card 新名片
	CardOld      string          `json:"card_old,omitempty"`      // group_card 旧名片
	SenderID     int64           `json:"sender_id,omitempty"`     // essence 操作对象
	OperatorNick string          `json:"operator_nick,omitempty"` // profile_like 操作者昵称
	Times        int             `json:"times,omitempty"`         // profile_like 点赞次数
	Title        string          `json:"title,omitempty"`         // notify/title 新头衔
	Likes        json.RawMessage `json:"likes,omitempty"`         // group_msg_emoji_like 回应列表
	IsAdd        bool            `json:"is_add,omitempty"`        // group_msg_emoji_like 是否添加
}

// ────────────────────────────────────────────────────────────────────────────
// 请求事件
// ────────────────────────────────────────────────────────────────────────────

// RequestEvent 同时覆盖好友请求和加群请求事件。
type RequestEvent struct {
	BaseEvent
	RequestType string `json:"request_type"` // friend, group
	SubType     string `json:"sub_type,omitempty"`
	UserID      int64  `json:"user_id"`
	GroupID     int64  `json:"group_id,omitempty"`
	Comment     string `json:"comment"`
	Flag        string `json:"flag"` // 用于 set_friend_add_request / set_group_add_request
}

// ────────────────────────────────────────────────────────────────────────────
// 元事件
// ────────────────────────────────────────────────────────────────────────────

// MetaEvent 覆盖生命周期和心跳元事件。
type MetaEvent struct {
	BaseEvent
	MetaEventType string          `json:"meta_event_type"` // lifecycle, heartbeat
	SubType       string          `json:"sub_type,omitempty"`
	Status        json.RawMessage `json:"status,omitempty"`   // 心跳状态（与 get_status 相同）
	Interval      int64           `json:"interval,omitempty"` // 心跳间隔，单位毫秒
}

// ────────────────────────────────────────────────────────────────────────────
// API 相关类型
// ────────────────────────────────────────────────────────────────────────────

// APIRequest 是发送给 OneBot 实现的类 JSON-RPC API 调用。
type APIRequest struct {
	Action string `json:"action"`
	Params any    `json:"params"`
	Echo   string `json:"echo,omitempty"` // 可选的关联 ID
}

// APIResponse 是 OneBot 实现返回的响应。
type APIResponse struct {
	Status  string          `json:"status"`  // ok, async, failed
	Retcode int             `json:"retcode"` // 0=成功, 1=异步, 其他=错误
	Data    json.RawMessage `json:"data"`
	Echo    string          `json:"echo,omitempty"`
}

// IsOK 当 API 调用成功时（retcode == 0）返回 true。
func (r *APIResponse) IsOK() bool { return r.Status == "ok" && r.Retcode == 0 }

// IsAsync 当请求被异步处理时返回 true。
func (r *APIResponse) IsAsync() bool { return r.Status == "async" && r.Retcode == 1 }

// ────────────────────────────────────────────────────────────────────────────
// API 参数类型
// ────────────────────────────────────────────────────────────────────────────

// SendPrivateMsgParams 是 send_private_msg 的请求参数。
type SendPrivateMsgParams struct {
	UserID     int64 `json:"user_id"`
	Message    any   `json:"message"` // string 或 MessageChain
	AutoEscape bool  `json:"auto_escape,omitempty"`
}

// SendGroupMsgParams 是 send_group_msg 的请求参数。
type SendGroupMsgParams struct {
	GroupID    int64 `json:"group_id"`
	Message    any   `json:"message"` // string 或 MessageChain
	AutoEscape bool  `json:"auto_escape,omitempty"`
}

// SendMsgParams 是 send_msg 的请求参数（自动判断私聊/群聊）。
type SendMsgParams struct {
	MessageType string `json:"message_type,omitempty"`
	UserID      int64  `json:"user_id,omitempty"`
	GroupID     int64  `json:"group_id,omitempty"`
	Message     any    `json:"message"`
	AutoEscape  bool   `json:"auto_escape,omitempty"`
}

// SendMsgResult 是 send_private_msg / send_group_msg / send_msg 的响应数据。
type SendMsgResult struct {
	MessageID int64 `json:"message_id"`
}

// MessageIDParams 是 delete_msg 的请求参数。
type MessageIDParams struct {
	MessageID int64 `json:"message_id"`
}

// MessageIDParams 是 get_msg 的请求参数。

// GetMsgResult 是 get_msg 的响应数据。
type GetMsgResult struct {
	Time        int64         `json:"time"`
	MessageType string        `json:"message_type"` // private, group
	MessageID   int32         `json:"message_id"`
	RealID      int32         `json:"real_id"`
	Sender      PrivateSender `json:"sender"`
	Message     MessageChain  `json:"message"`
	RawMessage  string        `json:"raw_message"`
}

// GetForwardMsgParams 是 get_forward_msg 的请求参数。
type GetForwardMsgParams struct {
	ID string `json:"id"`
}

// ForwardNode 是合并转发消息中的单个节点。
type ForwardNode struct {
	Content MessageChain `json:"content"`
	Sender  struct {
		Nickname string `json:"nickname"`
		UserID   int64  `json:"user_id"`
	} `json:"sender"`
	Time int64 `json:"time"`
}

// GetForwardMsgResult 是 get_forward_msg 的响应数据。
type GetForwardMsgResult struct {
	Message []ForwardNode `json:"message"`
}

// SendLikeParams 是 send_like 的请求参数。
type SendLikeParams struct {
	UserID int64 `json:"user_id"`
	Times  int   `json:"times,omitempty"` // 点赞次数，默认 1，上限 10
}

// SetGroupKickParams 是 set_group_kick 的请求参数。
type SetGroupKickParams struct {
	GroupID          int64 `json:"group_id"`
	UserID           int64 `json:"user_id"`
	RejectAddRequest bool  `json:"reject_add_request,omitempty"`
}

// SetGroupBanParams 是 set_group_ban 的请求参数。
type SetGroupBanParams struct {
	GroupID  int64 `json:"group_id"`
	UserID   int64 `json:"user_id"`
	Duration int64 `json:"duration"` // 秒；0 表示解禁
}

// SetGroupAnonymousBanParams 是 set_group_anonymous_ban 的请求参数。
type SetGroupAnonymousBanParams struct {
	GroupID       int64          `json:"group_id"`
	Anonymous     *AnonymousInfo `json:"anonymous,omitempty"`
	AnonymousFlag string         `json:"anonymous_flag,omitempty"` // 别名：flag
	Duration      int64          `json:"duration"`
}

// GroupToggleParams 是 set_group_whole_ban 的请求参数。
type GroupToggleParams struct {
	GroupID int64 `json:"group_id"`
	Enable  bool  `json:"enable"`
}

// SetGroupAdminParams 是 set_group_admin 的请求参数。
type SetGroupAdminParams struct {
	GroupID int64 `json:"group_id"`
	UserID  int64 `json:"user_id"`
	Enable  bool  `json:"enable"`
}

// GroupToggleParams 是 set_group_anonymous 的请求参数。

// SetGroupCardParams 是 set_group_card 的请求参数。
type SetGroupCardParams struct {
	GroupID int64  `json:"group_id"`
	UserID  int64  `json:"user_id"`
	Card    string `json:"card,omitempty"`
}

// SetGroupNameParams 是 set_group_name 的请求参数。
type SetGroupNameParams struct {
	GroupID   int64  `json:"group_id"`
	GroupName string `json:"group_name"`
}

// SetGroupLeaveParams 是 set_group_leave 的请求参数。
type SetGroupLeaveParams struct {
	GroupID   int64 `json:"group_id"`
	IsDismiss bool  `json:"is_dismiss,omitempty"`
}

// SetGroupSpecialTitleParams 是 set_group_special_title 的请求参数。
type SetGroupSpecialTitleParams struct {
	GroupID      int64  `json:"group_id"`
	UserID       int64  `json:"user_id"`
	SpecialTitle string `json:"special_title,omitempty"`
	Duration     int64  `json:"duration,omitempty"` // -1 表示永久
}

// SetFriendAddRequestParams 是 set_friend_add_request 的请求参数。
type SetFriendAddRequestParams struct {
	Flag    string `json:"flag"`
	Approve bool   `json:"approve"`
	Remark  string `json:"remark,omitempty"`
}

// SetGroupAddRequestParams 是 set_group_add_request 的请求参数。
type SetGroupAddRequestParams struct {
	Flag    string `json:"flag"`
	SubType string `json:"sub_type"` // add 或 invite
	Approve bool   `json:"approve"`
	Reason  string `json:"reason,omitempty"`
}

// GetLoginInfoResult 是 get_login_info 的响应数据。
type GetLoginInfoResult struct {
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
}

// GetStrangerInfoParams 是 get_stranger_info 的请求参数。
type GetStrangerInfoParams struct {
	UserID  int64 `json:"user_id"`
	NoCache bool  `json:"no_cache,omitempty"`
}

// StrangerInfo 是 get_stranger_info 的响应数据。
type StrangerInfo struct {
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
	Sex      string `json:"sex"` // male, female, unknown
	Age      int32  `json:"age"`
}

// FriendInfo 是好友列表中的单条记录。
type FriendInfo struct {
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
	Remark   string `json:"remark"`
}

// GetGroupInfoParams 是 get_group_info 的请求参数。
type GetGroupInfoParams struct {
	GroupID int64 `json:"group_id"`
	NoCache bool  `json:"no_cache,omitempty"`
}

// GroupInfo 是 get_group_info / get_group_list 的响应数据。
type GroupInfo struct {
	GroupID        int64  `json:"group_id"`
	GroupName      string `json:"group_name"`
	MemberCount    int32  `json:"member_count"`
	MaxMemberCount int32  `json:"max_member_count"`
}

// GetGroupMemberInfoParams 是 get_group_member_info 的请求参数。
type GetGroupMemberInfoParams struct {
	GroupID int64 `json:"group_id"`
	UserID  int64 `json:"user_id"`
	NoCache bool  `json:"no_cache,omitempty"`
}

// GroupIDParams 是 get_group_member_list 的请求参数。
type GroupIDParams struct {
	GroupID int64 `json:"group_id"`
}

// GroupMemberInfo 是 get_group_member_info / get_group_member_list 的响应数据。
type GroupMemberInfo struct {
	Nickname        string `json:"nickname"`
	Card            string `json:"card"`
	Sex             string `json:"sex"`
	Area            string `json:"area"`
	Level           string `json:"level"`
	Role            string `json:"role"` // owner, admin, member
	Title           string `json:"title"`
	GroupID         int64  `json:"group_id"`
	UserID          int64  `json:"user_id"`
	JoinTime        int64  `json:"join_time"`
	LastSentTime    int64  `json:"last_sent_time"`
	TitleExpireTime int64  `json:"title_expire_time"`
	Age             int32  `json:"age"`
	Unfriendly      bool   `json:"unfriendly"`
	CardChangeable  bool   `json:"card_changeable"`
}

// GetGroupHonorInfoParams 是 get_group_honor_info 的请求参数。
// Type 可为 "talkative"（龙王）、"performer"（群聊之火）、"legend"（群聊炽焰）、
// "strong_newbie"（冒尖小春笋）、"emotion"（快乐之源）或 "all"（查询所有）。
type GetGroupHonorInfoParams struct {
	GroupID int64  `json:"group_id"`
	Type    string `json:"type"`
}

// HonorMember 是荣誉成员信息。
type HonorMember struct {
	UserID      int64  `json:"user_id"`
	Nickname    string `json:"nickname"`
	Avatar      string `json:"avatar"`
	Description string `json:"description"`
}

// GroupHonorInfo 是 get_group_honor_info 的响应数据。
type GroupHonorInfo struct {
	GroupID          int64         `json:"group_id"`
	CurrentTalkative *HonorMember  `json:"current_talkative,omitempty"`
	TalkativeList    []HonorMember `json:"talkative_list,omitempty"`
	PerformerList    []HonorMember `json:"performer_list,omitempty"`
	LegendList       []HonorMember `json:"legend_list,omitempty"`
	StrongNewbieList []HonorMember `json:"strong_newbie_list,omitempty"`
	EmotionList      []HonorMember `json:"emotion_list,omitempty"`
}

// GetCookiesParams 是 get_cookies 的请求参数。
type GetCookiesParams struct {
	Domain string `json:"domain,omitempty"`
}

// GetCookiesResult 是 get_cookies 的响应数据。
type GetCookiesResult struct {
	Cookies string `json:"cookies"`
}

// GetCSRFTokenResult 是 get_csrf_token 的响应数据。
type GetCSRFTokenResult struct {
	Token int32 `json:"token"`
}

// GetCredentialsParams 是 get_credentials 的请求参数。
type GetCredentialsParams struct {
	Domain string `json:"domain,omitempty"`
}

// GetCredentialsResult 是 get_credentials 的响应数据。
type GetCredentialsResult struct {
	Cookies   string `json:"cookies"`
	CSRFToken int32  `json:"csrf_token"`
}

// GetRecordParams 是 get_record 的请求参数。
type GetRecordParams struct {
	File      string `json:"file"`
	OutFormat string `json:"out_format"` // mp3, amr, wma, m4a, spx, ogg, wav, flac
}

// GetRecordResult 是 get_record 的响应数据。
type GetRecordResult struct {
	File string `json:"file"`
}

// GetImageParams 是 get_image 的请求参数。
type GetImageParams struct {
	File string `json:"file"`
}

// GetImageResult 是 get_image 的响应数据。
type GetImageResult struct {
	File string `json:"file"`
}

// CanSendResult 是 can_send_image / can_send_record 的响应数据。
type CanSendResult struct {
	Yes bool `json:"yes"`
}

// GetStatusResult 是 get_status 的响应数据。
type GetStatusResult struct {
	Online bool `json:"online"`
	Good   bool `json:"good"`
}

// GetVersionInfoResult 是 get_version_info 的响应数据。
type GetVersionInfoResult struct {
	AppName         string `json:"app_name"`
	AppVersion      string `json:"app_version"`
	ProtocolVersion string `json:"protocol_version"`
}

// SetRestartParams 是 set_restart 的请求参数。
type SetRestartParams struct {
	Delay int `json:"delay,omitempty"` // 延迟毫秒数，默认 0
}

// GroupIDParams 是 get_group_at_all_remain 的请求参数。

// GetGroupAtAllRemainResult 是 get_group_at_all_remain 的响应数据。
type GetGroupAtAllRemainResult struct {
	CanAtAll                 bool `json:"can_at_all"`
	RemainAtAllCountForGroup int  `json:"remain_at_all_count_for_group"`
	RemainAtAllCountForUin   int  `json:"remain_at_all_count_for_uin"`
}

// ────────────────────────────────────────────────────────────────────────────
// 扩展动作：请求/响应类型（2026-08 对照 OneBot 11 标准、
// LLOneBot/LuckyLilliaBot、NapCat、Lagrange.OneBot v1 源码补齐）
// ────────────────────────────────────────────────────────────────────────────

// SendForwardMsgParams 是 send_forward_msg（智能路由合并转发）的请求参数。
type SendForwardMsgParams struct {
	MessageType string       `json:"message_type,omitempty"` // group / private
	UserID      int64        `json:"user_id,omitempty"`
	GroupID     int64        `json:"group_id,omitempty"`
	Messages    MessageChain `json:"messages"` // node 消息段数组
}

// SendGroupForwardMsgParams 是 send_group_forward_msg 的请求参数。
type SendGroupForwardMsgParams struct {
	GroupID  int64        `json:"group_id"`
	Messages MessageChain `json:"messages"` // node 消息段数组
}

// SendPrivateForwardMsgParams 是 send_private_forward_msg 的请求参数。
type SendPrivateForwardMsgParams struct {
	UserID   int64        `json:"user_id"`
	Messages MessageChain `json:"messages"` // node 消息段数组
}

// GetGroupMsgHistoryParams 是 get_group_msg_history 的请求参数（go-cqhttp 定义）。
type GetGroupMsgHistoryParams struct {
	GroupID    int64 `json:"group_id"`
	MessageSeq int64 `json:"message_seq,omitempty"` // 起始消息序号，0 从最新开始
	Count      int   `json:"count,omitempty"`       // 数量，默认 20
}

// GetFriendMsgHistoryParams 是 get_friend_msg_history 的请求参数（go-cqhttp 定义）。
type GetFriendMsgHistoryParams struct {
	UserID     int64 `json:"user_id"`
	MessageSeq int64 `json:"message_seq,omitempty"`
	Count      int   `json:"count,omitempty"`
}

// HistoryMsg 是历史消息列表中的单条消息。
type HistoryMsg struct {
	Time       int64        `json:"time"`
	MessageID  int32        `json:"message_id"`
	RealID     int32        `json:"real_id,omitempty"`
	MessageSeq int64        `json:"message_seq,omitempty"`
	GroupID    int64        `json:"group_id,omitempty"`
	UserID     int64        `json:"user_id,omitempty"`
	Message    MessageChain `json:"message"`
	RawMessage string       `json:"raw_message,omitempty"`
	Sender     GroupSender  `json:"sender"`
}

// GetMsgHistoryResult 是 get_group_msg_history / get_friend_msg_history 的响应数据。
type GetMsgHistoryResult struct {
	Messages []HistoryMsg `json:"messages"`
}

// EssenceMsg 是群精华消息条目。
type EssenceMsg struct {
	SenderID     int64        `json:"sender_id"`
	OperatorID   int64        `json:"operator_id"`
	MessageID    int32        `json:"message_id"`
	GroupID      int64        `json:"group_id"`
	Message      MessageChain `json:"message"`
	SenderName   string       `json:"sender_nick,omitempty"`
	OperatorName string       `json:"operator_nick,omitempty"`
	Time         int64        `json:"time"`
}

// MessageIDParams 是 set_essence_msg / delete_essence_msg 的请求参数。

// MessageIDParams 是 mark_msg_as_read 的请求参数（go-cqhttp 定义）。

// UploadGroupFileParams 是 upload_group_file 的请求参数（OneBot 11 标准）。
type UploadGroupFileParams struct {
	GroupID int64  `json:"group_id"`
	File    string `json:"file"`             // 本地路径或 URL
	Name    string `json:"name,omitempty"`   // 文件名（file 为 URL 时必填）
	Folder  string `json:"folder,omitempty"` // 文件夹 ID，默认根目录
}

// UploadPrivateFileParams 是 upload_private_file 的请求参数（OneBot 11 标准）。
type UploadPrivateFileParams struct {
	UserID int64  `json:"user_id"`
	File   string `json:"file"`
	Name   string `json:"name,omitempty"`
}

// DownloadFileParams 是 download_file 的请求参数（OneBot 11 标准）。
type DownloadFileParams struct {
	URL         string            `json:"url"`
	ThreadCount int               `json:"thread_count,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Timeout     int               `json:"timeout,omitempty"` // 毫秒
}

// DownloadFileResult 是 download_file 的响应数据。
type DownloadFileResult struct {
	File string `json:"file"` // 下载文件的绝对路径
}

// GetFileParams 是 get_file 的请求参数（go-cqhttp/LLB 扩展）。
type GetFileParams struct {
	FileID string `json:"file_id"`
}

// GetFileResult 是 get_file 的响应数据。
type GetFileResult struct {
	File     string `json:"file"`
	URL      string `json:"url,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
}

// OCRImageParams 是 ocr_image 的请求参数（OneBot 11 标准）。
type OCRImageParams struct {
	Image string `json:"image"`
}

// OCRText 是 OCR 识别出的一段文本。
type OCRText struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
}

// OCRImageResult 是 ocr_image 的响应数据。
type OCRImageResult struct {
	Texts []OCRText `json:"texts"`
}

// UserIDParams 是 delete_friend 的请求参数（LLB/NapCat 扩展）。
type UserIDParams struct {
	UserID int64 `json:"user_id"`
}

// SendGroupNoticeParams 是 send_group_notice 的请求参数（LLB/NapCat 扩展）。
type SendGroupNoticeParams struct {
	GroupID int64  `json:"group_id"`
	Content string `json:"content"`
	Image   string `json:"image,omitempty"` // 公告配图 URL
}

// GroupNotice 是群公告条目。
type GroupNotice struct {
	NoticeID string `json:"notice_id"`
	Title    string `json:"title,omitempty"`
	Content  string `json:"content"`
	UserID   int64  `json:"user_id,omitempty"`
	Time     int64  `json:"time,omitempty"`
}

// GroupIDParams 是 get_group_notice 的请求参数（LLB 扩展）。

// GetGroupNoticeResult 是 get_group_notice 的响应数据。
type GetGroupNoticeResult struct {
	Notices []GroupNotice `json:"notices"`
}

// SetGroupPortraitParams 是 set_group_portrait 的请求参数（OneBot 11 标准扩展）。
type SetGroupPortraitParams struct {
	GroupID int64  `json:"group_id"`
	File    string `json:"file"` // 本地路径或 URL
}

// GroupSystemMsg 是群系统消息（go-cqhttp/LLB 扩展）。
type GroupSystemMsg struct {
	InvitedRequests []GroupInvitedRequest `json:"invited_requests,omitempty"`
	JoinRequests    []GroupJoinRequest    `json:"join_requests,omitempty"`
}

// GroupInvitedRequest 是邀请入群请求条目。
type GroupInvitedRequest struct {
	RequestID   int64  `json:"request_id"`
	InvitorUin  int64  `json:"invitor_uin"`
	InvitorNick string `json:"invitor_nick"`
	GroupID     int64  `json:"group_id"`
	GroupName   string `json:"group_name"`
	Checked     bool   `json:"checked"`
	Actor       int64  `json:"actor,omitempty"`
}

// GroupJoinRequest 是加群请求条目。
type GroupJoinRequest struct {
	RequestID int64  `json:"request_id"`
	UserID    int64  `json:"user_id"`
	Nickname  string `json:"nickname"`
	Avatar    string `json:"avatar,omitempty"`
	GroupID   int64  `json:"group_id"`
	GroupName string `json:"group_name"`
	Checked   bool   `json:"checked"`
	Actor     int64  `json:"actor,omitempty"`
}

// GuildInfo 是频道信息（go-cqhttp/LLB 扩展 get_guild_list）。
type GuildInfo struct {
	GuildID   string `json:"guild_id"`
	GuildName string `json:"guild_name"`
}

// GroupIDParams 是 send_group_sign 的请求参数（LLB 扩展）。

// SendPokeParams 是 send_poke 的请求参数（OneBot 11 标准扩展）。
type SendPokeParams struct {
	UserID   int64 `json:"user_id,omitempty"`
	GroupID  int64 `json:"group_id,omitempty"`
	TargetID int64 `json:"target_id"` // 接收戳一戳的 QQ 号
}

// UserIDParams 是 friend_poke 的请求参数（LLB 扩展）。

// GroupPokeParams 是 group_poke 的请求参数（LLB 扩展）。
type GroupPokeParams struct {
	GroupID int64 `json:"group_id"`
	UserID  int64 `json:"user_id"`
}

// SetMsgEmojiLikeParams 是 set_msg_emoji_like / unset_msg_emoji_like 的请求参数（LLB 扩展）。
type SetMsgEmojiLikeParams struct {
	MessageID int64  `json:"message_id"`
	EmojiID   string `json:"emoji_id"`
}

// MessageIDParams 是 fetch_emoji_like 的请求参数（LLB 扩展）。

// EmojiLike 是消息上的表情回应统计。
type EmojiLike struct {
	EmojiID    string  `json:"emoji_id"`
	EmojiCount int     `json:"emoji_count"`
	UserIDs    []int64 `json:"user_ids,omitempty"`
}

// FetchEmojiLikeResult 是 fetch_emoji_like 的响应数据。
type FetchEmojiLikeResult struct {
	Likes []EmojiLike `json:"likes"`
}

// SetGroupReactionParams 是 set_group_reaction 的请求参数（Lagrange.OneBot v1 扩展）。
type SetGroupReactionParams struct {
	Code      string `json:"code"` // 表情 ID
	GroupID   int64  `json:"group_id"`
	MessageID int64  `json:"message_id"`
	IsAdd     bool   `json:"is_add,omitempty"`
}

// FilePathParams 是 set_qq_avatar 的请求参数（LLB 扩展）。
type FilePathParams struct {
	File string `json:"file"`
}

// UserIDParams 是 get_qq_avatar 的请求参数（LLB 扩展）。

// URLResult 是 get_qq_avatar 的响应数据。
type URLResult struct {
	URL string `json:"url"`
}

// SetQQProfileParams 是 set_qq_profile 的请求参数（LLB 扩展，字段全可选）。
type SetQQProfileParams struct {
	Nickname     string `json:"nickname,omitempty"`
	PersonalSign string `json:"personal_sign,omitempty"`
	Sex          string `json:"sex,omitempty"`
	City         string `json:"city,omitempty"`
	Birthday     string `json:"birthday,omitempty"`
}

// SetFriendRemarkParams 是 set_friend_remark 的请求参数（LLB 扩展）。
type SetFriendRemarkParams struct {
	UserID int64  `json:"user_id"`
	Remark string `json:"remark"`
}

// SetInputStatusParams 是 set_input_status 的请求参数（LLB 扩展）。
type SetInputStatusParams struct {
	EventType int `json:"event_type"` // 0=输入中，1=离开
	Times     int `json:"times,omitempty"`
}

// GroupShutMember 是群禁言列表中的成员。
type GroupShutMember struct {
	UserID     int64 `json:"user_id"`
	ShutUpTime int64 `json:"shut_up_time"`
}

// GetGroupShutListResult 是 get_group_shut_list 的响应数据（LLB 扩展）。
type GetGroupShutListResult struct {
	Members []GroupShutMember `json:"members"`
}

// SetGroupMsgMaskParams 是 set_group_msg_mask 的请求参数（LLB 扩展）。
// Mask：1=接收不提醒，2=收进列表不提醒，3=完全不接收。
type SetGroupMsgMaskParams struct {
	GroupID int64 `json:"group_id"`
	Mask    int   `json:"mask"`
}

// BatchDeleteGroupMemberParams 是 batch_delete_group_member 的请求参数（LLB 扩展）。
type BatchDeleteGroupMemberParams struct {
	GroupID          int64   `json:"group_id"`
	UserIDs          []int64 `json:"user_ids"`
	RejectAddRequest bool    `json:"reject_add_request,omitempty"`
}

// GetAICharactersParams 是 get_ai_characters 的请求参数（LLB 扩展）。
type GetAICharactersParams struct {
	GroupID  int64 `json:"group_id"`
	ChatType int   `json:"chat_type,omitempty"`
}

// AICharacter 是 AI 角色条目。
type AICharacter struct {
	CharacterID   int64  `json:"character_id"`
	CharacterName string `json:"character_name"`
	Example       string `json:"example,omitempty"`
}

// SendGroupAIRecordParams 是 send_group_ai_record 的请求参数（LLB 扩展）。
type SendGroupAIRecordParams struct {
	GroupID     int64 `json:"group_id"`
	CharacterID int64 `json:"character_id"`
}

// SendMsgResult 是 send_group_ai_record 的响应数据。

// GetAIRecordParams 是 get_ai_record 的请求参数（LLB 扩展）。
type GetAIRecordParams struct {
	CharacterID int64 `json:"character_id"`
}

// AIRecord 是 AI 角色消息记录。
type AIRecord struct {
	Time        int64        `json:"time"`
	MessageID   int32        `json:"message_id"`
	CharacterID int64        `json:"character_id"`
	Message     MessageChain `json:"message"`
	SenderID    int64        `json:"sender_id,omitempty"`
}

// GroupIDParams 是 get_group_file_system_info 的请求参数（LLB 扩展）。

// GetGroupFileSystemInfoResult 是 get_group_file_system_info 的响应数据。
type GetGroupFileSystemInfoResult struct {
	FileCount  int64 `json:"file_count"`
	LimitCount int64 `json:"limit_count"`
	UsedSpace  int64 `json:"used_space"`
}

// GroupFileItem 是群文件条目。
type GroupFileItem struct {
	FileID        string `json:"file_id"`
	FileName      string `json:"file_name"`
	FileSize      int64  `json:"file_size"`
	UploadTime    int64  `json:"upload_time"`
	DeadTime      int64  `json:"dead_time,omitempty"`
	ModifyTime    int64  `json:"modify_time,omitempty"`
	DownloadTimes int64  `json:"download_times,omitempty"`
	Uploader      int64  `json:"uploader,omitempty"`
	UploaderName  string `json:"uploader_name,omitempty"`
}

// GroupFolderItem 是群文件夹条目。
type GroupFolderItem struct {
	FolderID    string `json:"folder_id"`
	FolderName  string `json:"folder_name"`
	CreateTime  int64  `json:"create_time,omitempty"`
	Creator     int64  `json:"creator,omitempty"`
	CreatorName string `json:"creator_name,omitempty"`
}

// GroupFileListResult 是 get_group_root_files / get_group_files_by_folder 的响应数据。
type GroupFileListResult struct {
	Files   []GroupFileItem   `json:"files"`
	Folders []GroupFolderItem `json:"folders"`
}

// GetGroupFilesByFolderParams 是 get_group_files_by_folder 的请求参数（LLB 扩展）。
type GetGroupFilesByFolderParams struct {
	GroupID  int64  `json:"group_id"`
	FolderID string `json:"folder_id"`
}

// GetGroupFileURLParams 是 get_group_file_url 的请求参数（LLB 扩展）。
type GetGroupFileURLParams struct {
	GroupID int64  `json:"group_id"`
	FileID  string `json:"file_id"`
}

// GetPrivateFileURLParams 是 get_private_file_url 的请求参数（LLB 扩展）。
type GetPrivateFileURLParams struct {
	UserID int64  `json:"user_id"`
	FileID string `json:"file_id"`
}

// URLResult 是 get_group_file_url / get_private_file_url 的响应数据。

// MoveGroupFileParams 是 move_group_file 的请求参数（LLB 扩展）。
type MoveGroupFileParams struct {
	GroupID  int64  `json:"group_id"`
	FileID   string `json:"file_id"`
	ParentID string `json:"parent_id"`
	TargetID string `json:"target_id"`
}

// RenameGroupFileParams 是 rename_group_file 的请求参数（LLB 扩展）。
type RenameGroupFileParams struct {
	GroupID int64  `json:"group_id"`
	FileID  string `json:"file_id"`
	NewName string `json:"new_name"`
}

// DeleteGroupFileParams 是 delete_group_file 的请求参数（LLB 扩展）。
type DeleteGroupFileParams struct {
	GroupID int64  `json:"group_id"`
	FileID  string `json:"file_id"`
}

// CreateGroupFileFolderParams 是 create_group_file_folder 的请求参数（LLB 扩展）。
type CreateGroupFileFolderParams struct {
	GroupID int64  `json:"group_id"`
	Name    string `json:"name"`
}

// CreateGroupFileFolderResult 是 create_group_file_folder 的响应数据。
type CreateGroupFileFolderResult struct {
	FolderID string `json:"folder_id"`
}

// DeleteGroupFolderParams 是 delete_group_folder 的请求参数（LLB 扩展）。
type DeleteGroupFolderParams struct {
	GroupID  int64  `json:"group_id"`
	FolderID string `json:"folder_id"`
}

// RenameGroupFileFolderParams 是 rename_group_file_folder 的请求参数（LLB 扩展）。
type RenameGroupFileFolderParams struct {
	GroupID  int64  `json:"group_id"`
	FolderID string `json:"folder_id"`
	NewName  string `json:"new_name"`
}

// SetGroupFileForeverParams 是 set_group_file_forever 的请求参数（LLB 扩展）。
type SetGroupFileForeverParams struct {
	GroupID int64  `json:"group_id"`
	FileID  string `json:"file_id"`
}

// ────────────────────────────────────────────────────────────────────────────
// LLOneBot/LuckyLilliaBot 专有扩展：请求/响应类型
// （2026-08 对照 github.com/LLOneBot/LuckyLilliaBot src/onebot11/action/
//  llbot/ 目录源码补齐）
// ────────────────────────────────────────────────────────────────────────────

// LLOneBotConfig 是 get_config / set_config 的配置对象。
// 字段与 LLB 的 Config 一致，均为可选（仅修改传入的字段）。
type LLOneBotConfig struct {
	Milky               any    `json:"milky,omitempty"`
	Satori              any    `json:"satori,omitempty"`
	OB11                any    `json:"ob11,omitempty"`
	WebUI               any    `json:"webui,omitempty"`
	OnlyLocalhost       bool   `json:"onlyLocalhost,omitempty"`
	EnableLocalFile2URL bool   `json:"enableLocalFile2Url,omitempty"`
	Log                 bool   `json:"log,omitempty"`
	AutoDeleteFile      bool   `json:"autoDeleteFile,omitempty"`
	AutoDeleteFileSec   int    `json:"autoDeleteFileSecond,omitempty"`
	FFmpeg              string `json:"ffmpeg,omitempty"`
	MusicSignURL        string `json:"musicSignUrl,omitempty"`
	MsgCacheExpire      int    `json:"msgCacheExpire,omitempty"`
}

// LLOneBotDebugParams 是 llonebot_debug 的请求参数（调用 LLB 内部 API）。
type LLOneBotDebugParams struct {
	APIClass string `json:"apiClass"`
	Method   string `json:"method"`
	Args     []any  `json:"args,omitempty"`
}

// GetEventParams 是 get_event 的请求参数（获取 HTTP 事件池中的事件）。
type GetEventParams struct {
	Key     string `json:"key,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

// GetRobotUinRangeResult 是 get_robot_uin_range 的响应条目。
type GetRobotUinRangeResult struct {
	MinUin string `json:"min_uin"`
	MaxUin string `json:"max_uin"`
}

// SendPBParams 是 send_pb 的请求参数（发送底层 PB 协议包）。
type SendPBParams struct {
	Cmd string `json:"cmd"`
	Hex string `json:"hex"`
}

// SendPBResult 是 send_pb 的响应数据。
type SendPBResult struct {
	Cmd  string `json:"cmd"`
	Hex  string `json:"hex"`
	Echo string `json:"echo,omitempty"`
}

// FilePathParams 是 scan_qrcode 的请求参数（扫描本地二维码图片）。

// ScanQRCodeResult 是 scan_qrcode 的响应条目。
type ScanQRCodeResult struct {
	Text string `json:"text"`
}

// GetRkeyResult 是 get_rkey 的响应数据（QQ 文件下载 RKey）。
type GetRkeyResult struct {
	PrivateKey  string `json:"private_key"`
	GroupKey    string `json:"group_key"`
	ExpiredTime int    `json:"expired_time"`
	UpdatedTime string `json:"updated_time"`
}

// ForwardSingleMsgParams 是 forward_friend_single_msg / forward_group_single_msg
// 的请求参数（转发单条消息）。
type ForwardSingleMsgParams struct {
	MessageID int64 `json:"message_id"`
	GroupID   int64 `json:"group_id,omitempty"`
	UserID    int64 `json:"user_id,omitempty"`
}

// MessageIDParams 是 voice_msg_to_text 的请求参数（语音转文字）。

// VoiceMsg2TextResult 是 voice_msg_to_text 的响应数据。
type VoiceMsg2TextResult struct {
	Text string `json:"text"`
}

// GetRecommendFaceParams 是 get_recommend_face 的请求参数（推荐表情搜索）。
type GetRecommendFaceParams struct {
	Word string `json:"word"`
}

// GetRecommendFaceResult 是 get_recommend_face 的响应数据。
type GetRecommendFaceResult struct {
	URL []string `json:"url"`
}

// SetOnlineStatusParams 是 set_online_status 的请求参数。
type SetOnlineStatusParams struct {
	Status        int `json:"status"`
	ExtStatus     int `json:"ext_status"`
	BatteryStatus int `json:"battery_status"`
}

// ProfileLikeUser 是名片赞列表中的用户条目。
type ProfileLikeUser struct {
	UID      string `json:"uid,omitempty"`
	UIN      int64  `json:"uin,omitempty"`
	IsFriend bool   `json:"is_friend,omitempty"`
}

// GetProfileLikeParams 是 get_profile_like / get_profile_like_me 的请求参数。
// Start 从 0 开始，-1 表示获取全部；Count 每页最多 30。
type GetProfileLikeParams struct {
	Start int `json:"start,omitempty"`
	Count int `json:"count,omitempty"`
}

// GetProfileLikeResult 是 get_profile_like / get_profile_like_me 的响应数据。
type GetProfileLikeResult struct {
	Users     []ProfileLikeUser `json:"users"`
	NextStart int               `json:"next_start"`
}

// UserIDParams 是 get_profile_like_count 的请求参数。

// GetProfileLikeCountResult 是 get_profile_like_count 的响应数据。
type GetProfileLikeCountResult struct {
	Count int `json:"count"`
}

// FriendCategoryWithList 是带好友列表的分组。
type FriendCategoryWithList struct {
	CategoryID    int64        `json:"category_id"`
	CategoryName  string       `json:"category_name"`
	CategoryCount int          `json:"category_mb_count,omitempty"`
	OnlineCount   int          `json:"online_count,omitempty"`
	BuddyList     []FriendInfo `json:"buddy_list"`
}

// SetFriendCategoryParams 是 set_friend_category 的请求参数。
type SetFriendCategoryParams struct {
	UserID     int64 `json:"user_id"`
	CategoryID int64 `json:"category_id"`
}

// DoubtFriendRequest 是风险（疑似诈骗）好友请求条目。
type DoubtFriendRequest struct {
	Flag      string `json:"flag"`
	UIN       string `json:"uin"`
	Nick      string `json:"nick"`
	Source    string `json:"source"`
	Reason    string `json:"reason"`
	Msg       string `json:"msg"`
	GroupCode string `json:"group_code"`
	Time      string `json:"time"`
	Type      string `json:"type"`
}

// CountParams 是 get_doubt_friends_add_request 的请求参数。
type CountParams struct {
	Count int `json:"count,omitempty"`
}

// SetDoubtFriendsAddRequestParams 是 set_doubt_friends_add_request 的请求参数。
type SetDoubtFriendsAddRequestParams struct {
	Flag string `json:"flag"`
}

// SetGroupRemarkParams 是 set_group_remark 的请求参数（群备注）。
type SetGroupRemarkParams struct {
	GroupID int64  `json:"group_id"`
	Remark  string `json:"remark,omitempty"`
}

// GroupSignedMember 是群签到列表条目。
type GroupSignedMember struct {
	UserID int64  `json:"user_id"`
	Nick   string `json:"nick"`
	Time   int64  `json:"time"`
	Rank   int    `json:"rank"`
}

// DeleteGroupNoticeParams 是 _delete_group_notice 的请求参数（隐藏动作）。
type DeleteGroupNoticeParams struct {
	GroupID  int64  `json:"group_id"`
	NoticeID string `json:"notice_id"`
}

// UploadFlashFileParams 是 upload_flash_file 的请求参数（闪传上传）。
type UploadFlashFileParams struct {
	Title string   `json:"title,omitempty"`
	Paths []string `json:"paths"`
}

// FlashFileDownload 是闪传文件的下载入口。
type FlashFileDownload struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	URL    string `json:"url"`
	Expire int64  `json:"expire"`
}

// FlashFileSetResult 是 upload_flash_file 的响应数据。
type FlashFileSetResult struct {
	FileSetID  string              `json:"file_set_id"`
	ShareLink  string              `json:"share_link"`
	ExpireTime int64               `json:"expire_time"`
	Downloads  []FlashFileDownload `json:"downloads,omitempty"`
}

// UnmarshalJSON 兼容两套字段名：LLB 的 file_set_id 与 NapCat 的 fileset_id。
//
// 合并了 upload_flash_file / create_flash_task / reshare_flash_file 的响应，
// 而各协议端返回的键名不同（NapCat 用 fileset_id、LLB 用 file_set_id），
// 这里对两者都接受，避免同一字段因协议端差异解码失败。
func (r *FlashFileSetResult) UnmarshalJSON(b []byte) error {
	type alias FlashFileSetResult
	var raw struct {
		alias
		FileSetIDNap string              `json:"fileset_id"`
		DownloadsNap []FlashFileDownload `json:"downloads"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*r = FlashFileSetResult(raw.alias)
	if r.FileSetID == "" {
		r.FileSetID = raw.FileSetIDNap
	}
	if len(r.Downloads) == 0 {
		r.Downloads = raw.DownloadsNap
	}
	return nil
}

// FlashFileParams 是闪传相关动作的请求参数（share_link 与 file_set_id 二选一）。
// GetFilesetIdParams 是 get_fileset_id 的请求参数（NapCat 由分享链接取 ID）。
type GetFilesetIdParams struct {
	ShareLink string `json:"share_link"`
}

// GetFlashFileListParams 是 get_flash_file_list 的请求参数（NapCat 字段名 fileset_id）。
type GetFlashFileListParams struct {
	FileSetID string `json:"fileset_id"`
}

// GetShareLinkParams 是 get_share_link 的请求参数（NapCat 字段名 fileset_id）。
type GetShareLinkParams struct {
	FileSetID string `json:"fileset_id"`
}
type FlashFileParams struct {
	ShareLink string `json:"share_link,omitempty"`
	FileSetID string `json:"file_set_id,omitempty"`
}

// FlashFileInfo 是闪传文件信息条目。
type FlashFileInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// GetFlashFileInfoResult 是 get_flash_file_info 的响应数据。
type GetFlashFileInfoResult struct {
	FileSetID     string          `json:"file_set_id"`
	Title         string          `json:"title"`
	ShareLink     string          `json:"share_link"`
	TotalFileSize int64           `json:"total_file_size"`
	Files         []FlashFileInfo `json:"files"`
}

// GetFlashFileDownloadUrlsResult 是 get_flash_file_download_urls 的响应数据。
type GetFlashFileDownloadUrlsResult struct {
	FileSetID string              `json:"file_set_id"`
	ShareLink string              `json:"share_link"`
	Files     []FlashFileDownload `json:"files"`
}

// FlashFileSetResult 是 reshare_flash_file 的响应数据。

// GroupAlbum 是群相册信息（简化字段，完整字段在 Raw 中）。
type GroupAlbum struct {
	AlbumID        string `json:"album_id"`
	Owner          string `json:"owner,omitempty"`
	Name           string `json:"name,omitempty"`
	Desc           string `json:"desc,omitempty"`
	CreateTime     string `json:"create_time,omitempty"`
	ModifyTime     string `json:"modify_time,omitempty"`
	LastUploadTime string `json:"last_upload_time,omitempty"`
	UploadNumber   string `json:"upload_number,omitempty"`
	CoverURL       string `json:"cover_url,omitempty"`
}

// CreateGroupAlbumParams 是 create_group_album 的请求参数。
type CreateGroupAlbumParams struct {
	GroupID int64  `json:"group_id"`
	Name    string `json:"name"`
	Desc    string `json:"desc,omitempty"`
}

// CreateGroupAlbumResult 是 create_group_album 的响应数据。
type CreateGroupAlbumResult struct {
	AlbumID string `json:"album_id"`
	Owner   string `json:"owner,omitempty"`
	Name    string `json:"name,omitempty"`
	Desc    string `json:"desc,omitempty"`
}

// DeleteGroupAlbumParams 是 delete_group_album 的请求参数。
type DeleteGroupAlbumParams struct {
	GroupID int64  `json:"group_id"`
	AlbumID string `json:"album_id"`
}

// UploadGroupAlbumParams 是 upload_group_album 的请求参数。
type UploadGroupAlbumParams struct {
	GroupID int64    `json:"group_id"`
	AlbumID string   `json:"album_id"`
	Files   []string `json:"files"`
}

// GetGroupAlbumMediaListParams 是 get_group_album_media_list 的请求参数。
type GetGroupAlbumMediaListParams struct {
	GroupID    int64  `json:"group_id"`
	AlbumID    string `json:"album_id"`
	AttachInfo string `json:"attach_info,omitempty"`
}

// GroupAlbumMedia 是群相册媒体条目（简化字段）。
type GroupAlbumMedia struct {
	Type       int    `json:"type,omitempty"`
	Desc       string `json:"desc,omitempty"`
	UploadTime string `json:"upload_time,omitempty"`
	Uploader   string `json:"uploader,omitempty"`
	ImageURL   string `json:"image_url,omitempty"`
	VideoURL   string `json:"video_url,omitempty"`
}

// GetGroupAlbumMediaListResult 是 get_group_album_media_list 的响应数据。
type GetGroupAlbumMediaListResult struct {
	Album          GroupAlbum        `json:"album"`
	MediaList      []GroupAlbumMedia `json:"media_list"`
	NextAttachInfo string            `json:"next_attach_info,omitempty"`
	NextHasMore    bool              `json:"next_has_more"`
}

// ────────────────────────────────────────────────────────────────────────────
// NapCat 与 Lagrange.OneBot v1 补充动作：请求/响应类型
// （2026-08 对照 github.com/NapNeko/NapCatQQ packages/napcat-onebot/action/
//  与 Lagrange.Core v1 分支源码补齐）
// ────────────────────────────────────────────────────────────────────────────

// SetGroupKickMembersParams 是 set_group_kick_members 的请求参数（NapCat 批量踢人）。
type SetGroupKickMembersParams struct {
	GroupID          int64   `json:"group_id"`
	UserIDs          []int64 `json:"user_id"`
	RejectAddRequest bool    `json:"reject_add_request,omitempty"`
}

// MessageIDParams 是 fetch_ptt_text 的请求参数（NapCat 语音转文字）。

// GetEmojiLikesParams 是 get_emoji_likes 的请求参数（NapCat 表情回应列表）。
type GetEmojiLikesParams struct {
	GroupID   int64  `json:"group_id,omitempty"`
	MessageID string `json:"message_id"`
	EmojiID   string `json:"emoji_id"`
	EmojiType string `json:"emoji_type,omitempty"`
	Count     int    `json:"count,omitempty"` // 0=全部
}

// EmojiLikeUser 是表情回应中的用户。
type EmojiLikeUser struct {
	Uin      int64  `json:"uin"`
	NickName string `json:"nick_name,omitempty"`
}

// GetEmojiLikesResult 是 get_emoji_likes 的响应条目。
type GetEmojiLikesResult struct {
	EmojiID  string          `json:"emoji_id"`
	EmojiURL string          `json:"emoji_url,omitempty"`
	Users    []EmojiLikeUser `json:"users"`
}

// GetQunAlbumListParams 是 get_qun_album_list 的请求参数（NapCat 群相册列表）。
type GetQunAlbumListParams struct {
	GroupID    int64  `json:"group_id"`
	AttachInfo string `json:"attach_info,omitempty"` // 分页游标
}

// GroupInfoEx 是 get_group_info_ex 的扩展群信息。
type GroupInfoEx struct {
	GroupID     int64  `json:"group_id"`
	GroupName   string `json:"group_name,omitempty"`
	MemberCount int    `json:"member_count,omitempty"`
	// 其余字段与协议端返回一致（透传）。
}

// ClickInlineKeyboardButtonParams 是 click_inline_keyboard_button 的请求参数（NapCat 扩展）。
type ClickInlineKeyboardButtonParams struct {
	GroupID      int64  `json:"group_id"`
	BotAppID     string `json:"bot_appid"`
	ButtonID     string `json:"button_id,omitempty"`
	CallbackData string `json:"callback_data,omitempty"`
	MsgSeq       string `json:"msg_seq,omitempty"`
}

// SetGroupMemberInvitePolicyParams 是 set_group_member_invite_policy 的请求参数（NapCat 扩展）。
// Policy：0=需要验证，1=允许任何人加入，2=不允许任何人加入（以协议端实现为准）。
type SetGroupMemberInvitePolicyParams struct {
	GroupID int64 `json:"group_id"`
	Policy  int   `json:"policy"`
}

// SetGroupMemberPermissionsParams 是 set_group_member_permissions 的请求参数（NapCat 扩展）。
type SetGroupMemberPermissionsParams struct {
	GroupID                     int64 `json:"group_id"`
	AllowMemberUploadAlbum      *bool `json:"allow_member_upload_album,omitempty"`
	AllowMemberTemporarySession *bool `json:"allow_member_temporary_session,omitempty"`
	AllowMemberCreateGroup      *bool `json:"allow_member_create_group,omitempty"`
}

// SetGroupNewMemberHistoryVisibilityParams 是 set_group_new_member_history_visibility 的请求参数。
type SetGroupNewMemberHistoryVisibilityParams struct {
	GroupID int64 `json:"group_id"`
	Visible bool  `json:"visible"`
}

// SetDiyOnlineStatusParams 是 set_diy_online_status 的请求参数（NapCat 扩展）。
type SetDiyOnlineStatusParams struct {
	FaceID   int    `json:"face_id"`
	FaceType int    `json:"face_type,omitempty"`
	Wording  string `json:"wording,omitempty"`
}

// SetGroupSearchParams 是 set_group_search 的请求参数（NapCat 扩展）。
type SetGroupSearchParams struct {
	GroupID          int64 `json:"group_id"`
	NoCodeFingerOpen int   `json:"no_code_finger_open,omitempty"`
	NoFingerOpen     int   `json:"no_finger_open,omitempty"`
}

// TranslateEn2ZhParams 是 translate_en2zh 的请求参数（NapCat 英译中）。
type TranslateEn2ZhParams struct {
	Words []string `json:"words"`
}

// TranslateEn2ZhResult 是 translate_en2zh 的响应条目。
type TranslateEn2ZhResult struct {
	Word      string `json:"word"`
	Translate string `json:"translate"`
}

// URLParams 是 check_url_safely 的请求参数。
type URLParams struct {
	URL string `json:"url"`
}

// CheckUrlSafelyResult 是 check_url_safely 的响应数据。
// Level：1=安全，2=未知，3=危险。
type CheckUrlSafelyResult struct {
	Level int `json:"level"`
}

// CountParams 是 fetch_custom_face_detail 的请求参数。

// AddCustomFaceParams 是 add_custom_face 的请求参数。
type AddCustomFaceParams struct {
	File       string `json:"file"`
	EmojiID    string `json:"emoji_id,omitempty"`
	PackageID  string `json:"package_id,omitempty"`
	FileName   string `json:"file_name,omitempty"`
	FileSize   int64  `json:"file_size,omitempty"`
	MD5        string `json:"md5,omitempty"`
	IsMarkFace bool   `json:"is_mark_face,omitempty"`
	IsOrigin   bool   `json:"is_origin,omitempty"`
}

// DeleteCustomFaceParams 是 delete_custom_face 的请求参数。
// 支持 res_id / id / ids / md5 任一形式（以协议端实现为准）。
type DeleteCustomFaceParams struct {
	ResID any      `json:"res_id,omitempty"` // string 或 []string
	ID    any      `json:"id,omitempty"`
	IDs   []string `json:"ids,omitempty"`
	MD5   any      `json:"md5,omitempty"`
}

// SetCustomFaceDescParams 是 set_custom_face_desc 的请求参数。
type SetCustomFaceDescParams struct {
	EmojiID string `json:"emoji_id"`
	ResID   string `json:"res_id"`
	MD5     string `json:"md5"`
	Desc    string `json:"desc"`
}

// SetGroupAddOptionParams 是 set_group_add_option 的请求参数（NapCat 加群设置）。
type SetGroupAddOptionParams struct {
	GroupID       int64  `json:"group_id"`
	AddType       int    `json:"add_type"`
	GroupQuestion string `json:"group_question,omitempty"`
	GroupAnswer   string `json:"group_answer,omitempty"`
}

// SetGroupRobotAddOptionParams 是 set_group_robot_add_option 的请求参数（NapCat 扩展）。
type SetGroupRobotAddOptionParams struct {
	GroupID            int64 `json:"group_id"`
	RobotMemberSwitch  int   `json:"robot_member_switch,omitempty"`
	RobotMemberExamine int   `json:"robot_member_examine,omitempty"`
}

// MarkMsgAsReadExParams 是 mark_group_msg_as_read / mark_private_msg_as_read 的请求参数。
type MarkMsgAsReadExParams struct {
	UserID    int64 `json:"user_id,omitempty"`
	GroupID   int64 `json:"group_id,omitempty"`
	MessageID int64 `json:"message_id,omitempty"`
}

// GroupIgnoredNotify 是被过滤的加群通知条目。
type GroupIgnoredNotify struct {
	GroupID int64  `json:"group_id"`
	UserID  int64  `json:"user_id"`
	Flag    string `json:"flag"`
}

// CreateFlashTaskParams 是 create_flash_task 的请求参数（NapCat 闪传创建任务）。
type CreateFlashTaskParams struct {
	Files     []string `json:"files"`
	Name      string   `json:"name,omitempty"`
	ThumbPath string   `json:"thumb_path,omitempty"`
}

// FlashFileSetResult 是 create_flash_task 的响应数据。

// FilesetInfo 是闪传文件集信息。
type FilesetInfo struct {
	FileSetID string `json:"fileset_id,omitempty"`
	Title     string `json:"title,omitempty"`
	ShareLink string `json:"share_link,omitempty"`
	FileName  string `json:"file_name,omitempty"`
	FileSize  int64  `json:"file_size,omitempty"`
	FileCount int    `json:"file_count,omitempty"`
	// 其余字段透传（napcat 返回较深结构，用 Raw 保留）。
	Raw json.RawMessage `json:"-"`
}

// FlashFileParams 是 get_fileset_id 的请求参数（NapCat 由分享链接取 ID）。

// GetFilesetIdResult 是 get_fileset_id 的响应数据。
type GetFilesetIdResult struct {
	FileSetID string `json:"fileset_id"`
}

// FlashFileParams 是 get_flash_file_list 的请求参数。

// FlashFileEntry 是闪传文件条目。
type FlashFileEntry struct {
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"`
	FileID   string `json:"file_id,omitempty"`
	URL      string `json:"url,omitempty"`
}

// GetFlashFileUrlParams 是 get_flash_file_url 的请求参数。
type GetFlashFileUrlParams struct {
	FileSetID string `json:"fileset_id"`
	FileName  string `json:"file_name,omitempty"`
	FileIndex int    `json:"file_index,omitempty"`
}

// GetFlashFileUrlResult 是 get_flash_file_url 的响应数据。
type GetFlashFileUrlResult struct {
	URL    string `json:"url"`
	Expire int64  `json:"expire,omitempty"`
}

// FlashFileParams 是 get_share_link 的请求参数。

// GetShareLinkResult 是 get_share_link 的响应数据。
type GetShareLinkResult struct {
	ShareLink string `json:"share_link"`
}

// SendFlashMsgParams 是 send_flash_msg 的请求参数。
type SendFlashMsgParams struct {
	FileSetID string `json:"fileset_id"`
	UserID    int64  `json:"user_id,omitempty"`
	GroupID   int64  `json:"group_id,omitempty"`
}

// FetchMFaceKeyParams 是 fetch_mface_key 的请求参数（Lagrange v1 扩展）。
type FetchMFaceKeyParams struct {
	EmojiIDs []string `json:"emoji_ids"`
}

// FetchMFaceKeyResult 是 fetch_mface_key 的响应条目。
type FetchMFaceKeyResult struct {
	EmojiID string `json:"emoji_id"`
	Key     string `json:"key"`
}

// GroupMemo 是群备忘录条目。
type GroupMemo struct {
	MemoID  string `json:"memo_id,omitempty"`
	Content string `json:"content"`
	UserID  int64  `json:"user_id,omitempty"`
	Time    int64  `json:"time,omitempty"`
}

// SetGroupMemoParams 是 set_group_memo 的请求参数（Lagrange v1 扩展）。
type SetGroupMemoParams struct {
	GroupID int64  `json:"group_id"`
	Content string `json:"content"`
}

// DeleteGroupMemoParams 是 delete_group_memo 的请求参数（Lagrange v1 扩展）。
type DeleteGroupMemoParams struct {
	GroupID int64  `json:"group_id"`
	MemoID  string `json:"memo_id,omitempty"`
}

// SetGroupBotStatusParams 是 set_group_bot_status 的请求参数（Lagrange v1 扩展）。
type SetGroupBotStatusParams struct {
	GroupID int64 `json:"group_id"`
	Online  bool  `json:"online"`
}

// FilePathParams 是 upload_image 的请求参数（Lagrange v1 扩展）。

// URLResult 是 upload_image 的响应数据。
