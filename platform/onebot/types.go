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
	NoticeTypeFriendRemove  = "friend_remove"
	NoticeTypeNotify        = "notify"
)

// 通知子类型常量（notice_type=notify）
const (
	NotifySubTypePoke      = "poke"
	NotifySubTypeLuckyKing = "lucky_king"
	NotifySubTypeHonor     = "honor"
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
	MessageID int32 `json:"message_id"`
}

// DeleteMsgParams 是 delete_msg 的请求参数。
type DeleteMsgParams struct {
	MessageID int32 `json:"message_id"`
}

// GetMsgParams 是 get_msg 的请求参数。
type GetMsgParams struct {
	MessageID int32 `json:"message_id"`
}

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

// SetGroupWholeBanParams 是 set_group_whole_ban 的请求参数。
type SetGroupWholeBanParams struct {
	GroupID int64 `json:"group_id"`
	Enable  bool  `json:"enable"`
}

// SetGroupAdminParams 是 set_group_admin 的请求参数。
type SetGroupAdminParams struct {
	GroupID int64 `json:"group_id"`
	UserID  int64 `json:"user_id"`
	Enable  bool  `json:"enable"`
}

// SetGroupAnonymousParams 是 set_group_anonymous 的请求参数。
type SetGroupAnonymousParams struct {
	GroupID int64 `json:"group_id"`
	Enable  bool  `json:"enable"`
}

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

// GetGroupMemberListParams 是 get_group_member_list 的请求参数。
type GetGroupMemberListParams struct {
	GroupID int64 `json:"group_id"`
}

// GroupMemberInfo 是 get_group_member_info / get_group_member_list 的响应数据。
type GroupMemberInfo struct {
	GroupID         int64  `json:"group_id"`
	UserID          int64  `json:"user_id"`
	Nickname        string `json:"nickname"`
	Card            string `json:"card"`
	Sex             string `json:"sex"`
	Age             int32  `json:"age"`
	Area            string `json:"area"`
	JoinTime        int64  `json:"join_time"`
	LastSentTime    int64  `json:"last_sent_time"`
	Level           string `json:"level"`
	Role            string `json:"role"` // owner, admin, member
	Unfriendly      bool   `json:"unfriendly"`
	Title           string `json:"title"`
	TitleExpireTime int64  `json:"title_expire_time"`
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

// GetGroupAtAllRemainParams 是 get_group_at_all_remain 的请求参数。
type GetGroupAtAllRemainParams struct {
	GroupID int64 `json:"group_id"`
}

// GetGroupAtAllRemainResult 是 get_group_at_all_remain 的响应数据。
type GetGroupAtAllRemainResult struct {
	CanAtAll                 bool `json:"can_at_all"`
	RemainAtAllCountForGroup int  `json:"remain_at_all_count_for_group"`
	RemainAtAllCountForUin   int  `json:"remain_at_all_count_for_uin"`
}
