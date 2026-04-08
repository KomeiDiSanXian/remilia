package milky

import (
	stdctx "context"
	"strconv"
	"time"

	"github.com/KomeiDiSanXian/remilia/errutil"
)

// ────────────────────────────────────────────────────────────────────────────
// 公开数据类型（API 响应结构）
// ────────────────────────────────────────────────────────────────────────────

// LoginInfo 登录信息。
type LoginInfo struct {
	Uin      int64
	Nickname string
}

// ImplInfo 协议端信息。
type ImplInfo struct {
	ImplName          string
	ImplVersion       string
	QQProtocolVersion string
	QQProtocolType    string
	MilkyVersion      string
}

// UserProfile 用户个人信息。
type UserProfile struct {
	Nickname string
	QID      string
	Age      int
	Sex      string
	Remark   string
	Bio      string
	Level    int
	Country  string
	City     string
	School   string
}

// FriendCategory 好友分组。
type FriendCategory struct {
	CategoryID   int
	CategoryName string
}

// FriendInfo 好友信息。
type FriendInfo struct {
	UserID   int64
	Nickname string
	Sex      string
	QID      string
	Remark   string
	Category FriendCategory
}

// GroupInfo 群信息。
type GroupInfo struct {
	GroupID        int64
	GroupName      string
	MemberCount    int
	MaxMemberCount int
	Remark         string
	CreatedTime    int64
	Description    string
	Question       string
	Announcement   string
}

// GroupMemberInfo 群成员信息。
type GroupMemberInfo struct {
	UserID        int64
	GroupID       int64
	Nickname      string
	Card          string
	Sex           string
	Title         string
	Level         int
	Role          string
	JoinTime      int64
	LastSentTime  int64
	ShutUpEndTime *int64
}

// Announcement 群公告。
type Announcement struct {
	GroupID        int64
	AnnouncementID string
	UserID         int64
	Time           int64
	Content        string
	ImageURL       *string
}

// MessageSegmentData 消息段数据载荷。
type MessageSegmentData struct {
	// 文本
	Text string
	// 提及
	UserID int64
	Name   string
	// 表情
	FaceID  string
	IsLarge bool
	// 引用回复
	MessageSeq int64
	SenderID   int64
	SenderName string
	ReplyTime  int64 // 被引用消息的 Unix 时间戳（秒）
	// 图片/语音/视频
	ResourceID string
	TempURL    string
	SubType    string
	Width      int
	Height     int
	Duration   int
	// 文件
	FileID   string
	FileName string
	FileSize int64
	FileHash string // TriSHA1 哈希值，仅私聊文件消息段存在
	// 合并转发
	ForwardID string
	Title     string
	Preview   []string
	Summary   string
	// 市场表情（market_face）
	EmojiPackageID int
	EmojiID        string
	EmojiKey       string
	EmojiURL       string
	// 小程序（light_app）
	AppName     string
	JSONPayload string
	// XML 消息
	ServiceID  int
	XMLPayload string
}

// MessageSegment 消息段。
type MessageSegment struct {
	Type string
	Data MessageSegmentData
}

// Message 消息（由 GetMessage / GetHistoryMessages 返回）。
type Message struct {
	Scene      string // "friend"、"group" 或 "temp"
	PeerID     int64
	MessageSeq int64
	SenderID   int64
	Time       int64
	Segments   []MessageSegment
}

// ForwardedMessage 合并转发中的单条消息。
type ForwardedMessage struct {
	MessageSeq int64
	SenderName string
	AvatarURL  string
	Time       int64
	Segments   []MessageSegment
}

// EssenceMessage 群精华消息。
type EssenceMessage struct {
	GroupID       int64
	MessageSeq    int64
	MessageTime   int64
	SenderID      int64
	SenderName    string
	OperatorID    int64
	OperatorName  string
	OperationTime int64
	Segments      []MessageSegment
}

// GroupNotification 群通知（入群申请/邀请他人入群等）。
type GroupNotification struct {
	Type            string
	GroupID         int64
	NotificationSeq int64
	IsFiltered      bool
	InitiatorID     int64
	TargetUserID    int64
	State           string
	OperatorID      *int64
	Comment         string
	IsSet           bool
}

// FriendRequest 好友请求。
type FriendRequest struct {
	Time          int64
	InitiatorID   int64
	InitiatorUID  string
	TargetUserID  int64
	TargetUserUID string
	State         string
	Comment       string
	Via           string
	IsFiltered    bool
}

// GroupFile 群文件。
type GroupFile struct {
	GroupID         int64
	FileID          string
	FileName        string
	ParentFolderID  string
	FileSize        int64
	UploadedTime    int64
	ExpireTime      *int64
	UploaderID      int64
	DownloadedTimes int
}

// GroupFolder 群文件夹。
type GroupFolder struct {
	GroupID          int64
	FolderID         string
	ParentFolderID   string
	FolderName       string
	CreatedTime      int64
	LastModifiedTime int64
	CreatorID        int64
	FileCount        int
}

// ────────────────────────────────────────────────────────────────────────────
// 内部转换辅助函数
// ────────────────────────────────────────────────────────────────────────────

func toMessageSegment(s incomingSegment) MessageSegment {
	return MessageSegment{
		Type: s.Type,
		Data: MessageSegmentData{
			Text:           s.Data.Text,
			UserID:         s.Data.UserID,
			Name:           s.Data.Name,
			FaceID:         s.Data.FaceID,
			IsLarge:        s.Data.IsLarge,
			MessageSeq:     s.Data.MessageSeq,
			SenderID:       s.Data.SenderID,
			SenderName:     s.Data.SenderName,
			ReplyTime:      s.Data.ReplyTime,
			ResourceID:     s.Data.ResourceID,
			TempURL:        s.Data.TempURL,
			SubType:        s.Data.SubType,
			Width:          s.Data.Width,
			Height:         s.Data.Height,
			Duration:       s.Data.Duration,
			FileID:         s.Data.FileID,
			FileName:       s.Data.FileName,
			FileSize:       s.Data.FileSize,
			FileHash:       s.Data.FileHash,
			ForwardID:      s.Data.ForwardID,
			Title:          s.Data.Title,
			Preview:        s.Data.Preview,
			Summary:        s.Data.Summary,
			EmojiPackageID: s.Data.EmojiPackageID,
			EmojiID:        s.Data.EmojiID,
			EmojiKey:       s.Data.EmojiKey,
			EmojiURL:       s.Data.EmojiURL,
			AppName:        s.Data.AppName,
			JSONPayload:    s.Data.JSONPayload,
			ServiceID:      s.Data.ServiceID,
			XMLPayload:     s.Data.XMLPayload,
		},
	}
}

func toMessageSegments(segs []incomingSegment) []MessageSegment {
	out := make([]MessageSegment, len(segs))
	for i, s := range segs {
		out[i] = toMessageSegment(s)
	}
	return out
}

func toMessage(m incomingMessage) Message {
	return Message{
		Scene:      m.MessageScene,
		PeerID:     m.PeerID,
		MessageSeq: m.MessageSeq,
		SenderID:   m.SenderID,
		Time:       m.Time,
		Segments:   toMessageSegments(m.Segments),
	}
}

func toFriendInfo(f friendInfoJSON) FriendInfo {
	return FriendInfo{
		UserID:   f.UserID,
		Nickname: f.Nickname,
		Sex:      f.Sex,
		QID:      f.QID,
		Remark:   f.Remark,
		Category: FriendCategory{
			CategoryID:   f.Category.CategoryID,
			CategoryName: f.Category.CategoryName,
		},
	}
}

func toGroupInfo(g groupInfoJSON) GroupInfo {
	return GroupInfo{
		GroupID:        g.GroupID,
		GroupName:      g.GroupName,
		MemberCount:    g.MemberCount,
		MaxMemberCount: g.MaxMemberCount,
		Remark:         g.Remark,
		CreatedTime:    g.CreatedTime,
		Description:    g.Description,
		Question:       g.Question,
		Announcement:   g.Announcement,
	}
}

func toGroupMemberInfo(m groupMemberInfoJSON) GroupMemberInfo {
	return GroupMemberInfo{
		UserID:        m.UserID,
		GroupID:       m.GroupID,
		Nickname:      m.Nickname,
		Card:          m.Card,
		Sex:           m.Sex,
		Title:         m.Title,
		Level:         m.Level,
		Role:          m.Role,
		JoinTime:      m.JoinTime,
		LastSentTime:  m.LastSentTime,
		ShutUpEndTime: m.ShutUpEndTime,
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 系统 API
// ────────────────────────────────────────────────────────────────────────────

// GetImplInfo 获取协议端实现信息（版本号、协议类型等）。
func (a *Adapter) GetImplInfo(ctx stdctx.Context) (ImplInfo, error) {
	var out getImplInfoOutput
	if err := a.client.call(ctx, "get_impl_info", struct{}{}, &out); err != nil {
		return ImplInfo{}, err
	}
	return ImplInfo{
		ImplName:          out.ImplName,
		ImplVersion:       out.ImplVersion,
		QQProtocolVersion: out.QQProtocolVersion,
		QQProtocolType:    out.QQProtocolType,
		MilkyVersion:      out.MilkyVersion,
	}, nil
}

// GetLoginInfo 获取机器人账号的登录信息（QQ 号及昵称）。
func (a *Adapter) GetLoginInfo(ctx stdctx.Context) (LoginInfo, error) {
	var out getLoginInfoOutput
	if err := a.client.call(ctx, "get_login_info", struct{}{}, &out); err != nil {
		return LoginInfo{}, err
	}
	return LoginInfo{Uin: out.Uin, Nickname: out.Nickname}, nil
}

// GetUserProfile 获取指定用户的个人信息。
func (a *Adapter) GetUserProfile(ctx stdctx.Context, userID int64) (UserProfile, error) {
	var out getUserProfileOutput
	if err := a.client.call(ctx, "get_user_profile", &getUserProfileInput{UserID: userID}, &out); err != nil {
		return UserProfile{}, err
	}
	return UserProfile{
		Nickname: out.Nickname,
		QID:      out.QID,
		Age:      out.Age,
		Sex:      out.Sex,
		Remark:   out.Remark,
		Bio:      out.Bio,
		Level:    out.Level,
		Country:  out.Country,
		City:     out.City,
		School:   out.School,
	}, nil
}

// GetFriendList 获取好友列表。noCache=true 时绕过缓存强制刷新。
func (a *Adapter) GetFriendList(ctx stdctx.Context, noCache bool) ([]FriendInfo, error) {
	var out getFriendListOutput
	if err := a.client.call(ctx, "get_friend_list", &getFriendListInput{NoCache: noCache}, &out); err != nil {
		return nil, err
	}
	result := make([]FriendInfo, len(out.Friends))
	for i, f := range out.Friends {
		result[i] = toFriendInfo(f)
	}
	return result, nil
}

// GetFriendInfo 获取指定好友的信息。
func (a *Adapter) GetFriendInfo(ctx stdctx.Context, userID int64, noCache bool) (FriendInfo, error) {
	var out getFriendInfoOutput
	if err := a.client.call(ctx, "get_friend_info", &getFriendInfoInput{UserID: userID, NoCache: noCache}, &out); err != nil {
		return FriendInfo{}, err
	}
	return toFriendInfo(out.Friend), nil
}

// GetGroupList 获取群列表。
func (a *Adapter) GetGroupList(ctx stdctx.Context, noCache bool) ([]GroupInfo, error) {
	var out getGroupListOutput
	if err := a.client.call(ctx, "get_group_list", &getGroupListInput{NoCache: noCache}, &out); err != nil {
		return nil, err
	}
	result := make([]GroupInfo, len(out.Groups))
	for i, g := range out.Groups {
		result[i] = toGroupInfo(g)
	}
	return result, nil
}

// GetGroupInfo 获取指定群的信息。
func (a *Adapter) GetGroupInfo(ctx stdctx.Context, groupID int64, noCache bool) (GroupInfo, error) {
	var out getGroupInfoOutput
	if err := a.client.call(ctx, "get_group_info", &getGroupInfoInput{GroupID: groupID, NoCache: noCache}, &out); err != nil {
		return GroupInfo{}, err
	}
	return toGroupInfo(out.Group), nil
}

// GetGroupMemberList 获取指定群的成员列表。
func (a *Adapter) GetGroupMemberList(ctx stdctx.Context, groupID int64, noCache bool) ([]GroupMemberInfo, error) {
	var out getGroupMemberListOutput
	if err := a.client.call(ctx, "get_group_member_list", &getGroupMemberListInput{GroupID: groupID, NoCache: noCache}, &out); err != nil {
		return nil, err
	}
	result := make([]GroupMemberInfo, len(out.Members))
	for i, m := range out.Members {
		result[i] = toGroupMemberInfo(m)
	}
	return result, nil
}

// GetGroupMemberInfo 获取指定群成员的信息。
func (a *Adapter) GetGroupMemberInfo(ctx stdctx.Context, groupID, userID int64, noCache bool) (GroupMemberInfo, error) {
	var out getGroupMemberInfoOutput
	if err := a.client.call(ctx, "get_group_member_info", &getGroupMemberInfoInput{GroupID: groupID, UserID: userID, NoCache: noCache}, &out); err != nil {
		return GroupMemberInfo{}, err
	}
	return toGroupMemberInfo(out.Member), nil
}

// GetPeerPins 获取已置顶的好友和群列表。
func (a *Adapter) GetPeerPins(ctx stdctx.Context) (friends []FriendInfo, groups []GroupInfo, err error) {
	var out getPeerPinsOutput
	if err = a.client.call(ctx, "get_peer_pins", struct{}{}, &out); err != nil {
		return
	}
	friends = make([]FriendInfo, len(out.Friends))
	for i, f := range out.Friends {
		friends[i] = toFriendInfo(f)
	}
	groups = make([]GroupInfo, len(out.Groups))
	for i, g := range out.Groups {
		groups[i] = toGroupInfo(g)
	}
	return
}

// SetPeerPin 设置好友或群的置顶状态。
// scene 为消息场景（"friend" 或 "group"）。
func (a *Adapter) SetPeerPin(ctx stdctx.Context, scene string, peerID int64, isPinned bool) error {
	return a.client.call(ctx, "set_peer_pin", &setPeerPinInput{
		MessageScene: scene,
		PeerID:       peerID,
		IsPinned:     isPinned,
	}, nil)
}

// SetAvatar 设置机器人 QQ 账号头像。uri 支持 http(s):// 或 base64:// 前缀。
func (a *Adapter) SetAvatar(ctx stdctx.Context, uri string) error {
	return a.client.call(ctx, "set_avatar", &setAvatarInput{URI: uri}, nil)
}

// SetNickname 设置机器人 QQ 账号昵称。
func (a *Adapter) SetNickname(ctx stdctx.Context, nickname string) error {
	return a.client.call(ctx, "set_nickname", &setNicknameInput{NewNickname: nickname}, nil)
}

// SetBio 设置机器人 QQ 账号个性签名。
func (a *Adapter) SetBio(ctx stdctx.Context, bio string) error {
	return a.client.call(ctx, "set_bio", &setBioInput{NewBio: bio}, nil)
}

// GetCustomFaceURLList 获取账号的自定义表情 URL 列表。
func (a *Adapter) GetCustomFaceURLList(ctx stdctx.Context) ([]string, error) {
	var out getCustomFaceURLListOutput
	if err := a.client.call(ctx, "get_custom_face_url_list", struct{}{}, &out); err != nil {
		return nil, err
	}
	return out.URLs, nil
}

// GetCookies 获取指定域名的 Cookies。
func (a *Adapter) GetCookies(ctx stdctx.Context, domain string) (string, error) {
	var out getCookiesOutput
	if err := a.client.call(ctx, "get_cookies", &getCookiesInput{Domain: domain}, &out); err != nil {
		return "", err
	}
	return out.Cookies, nil
}

// GetCSRFToken 获取 CSRF Token。
func (a *Adapter) GetCSRFToken(ctx stdctx.Context) (string, error) {
	var out getCSRFTokenOutput
	if err := a.client.call(ctx, "get_csrf_token", struct{}{}, &out); err != nil {
		return "", err
	}
	return out.CSRFToken, nil
}

// ────────────────────────────────────────────────────────────────────────────
// 消息 API
// ────────────────────────────────────────────────────────────────────────────

// GetMessage 获取单条消息。
// scene 为消息场景（"friend"、"group" 或 "temp"）。
func (a *Adapter) GetMessage(ctx stdctx.Context, scene string, peerID, messageSeq int64) (Message, error) {
	var out getMessageOutput
	if err := a.client.call(ctx, "get_message", &getMessageInput{
		MessageScene: scene,
		PeerID:       peerID,
		MessageSeq:   messageSeq,
	}, &out); err != nil {
		return Message{}, err
	}
	return toMessage(out.Message), nil
}

// GetHistoryMessages 获取历史消息列表。
//
// startSeq 为起始消息序列号（nil 表示从最新开始）；limit 为最大返回条数（0 使用服务端默认值）。
// 返回消息列表和下一页起始序列号（nil 表示已到末尾）。
func (a *Adapter) GetHistoryMessages(ctx stdctx.Context, scene string, peerID int64, startSeq *int64, limit int) ([]Message, *int64, error) {
	var out getHistoryMessagesOutput
	if err := a.client.call(ctx, "get_history_messages", &getHistoryMessagesInput{
		MessageScene:    scene,
		PeerID:          peerID,
		StartMessageSeq: startSeq,
		Limit:           limit,
	}, &out); err != nil {
		return nil, nil, err
	}
	msgs := make([]Message, len(out.Messages))
	for i, m := range out.Messages {
		msgs[i] = toMessage(m)
	}
	return msgs, out.NextMessageSeq, nil
}

// GetResourceTempURL 获取消息资源（图片/语音/视频）的临时下载链接。
func (a *Adapter) GetResourceTempURL(ctx stdctx.Context, resourceID string) (string, error) {
	var out getResourceTempURLOutput
	if err := a.client.call(ctx, "get_resource_temp_url", &getResourceTempURLInput{ResourceID: resourceID}, &out); err != nil {
		return "", err
	}
	return out.URL, nil
}

// GetForwardedMessages 获取合并转发消息的内容。
func (a *Adapter) GetForwardedMessages(ctx stdctx.Context, forwardID string) ([]ForwardedMessage, error) {
	var out getForwardedMessagesOutput
	if err := a.client.call(ctx, "get_forwarded_messages", &getForwardedMessagesInput{ForwardID: forwardID}, &out); err != nil {
		return nil, err
	}
	result := make([]ForwardedMessage, len(out.Messages))
	for i, m := range out.Messages {
		result[i] = ForwardedMessage{
			MessageSeq: m.MessageSeq,
			SenderName: m.SenderName,
			AvatarURL:  m.AvatarURL,
			Time:       m.Time,
			Segments:   toMessageSegments(m.Segments),
		}
	}
	return result, nil
}

// MarkMessageAsRead 将指定消息标记为已读。
func (a *Adapter) MarkMessageAsRead(ctx stdctx.Context, scene string, peerID, messageSeq int64) error {
	return a.client.call(ctx, "mark_message_as_read", &markMessageAsReadInput{
		MessageScene: scene,
		PeerID:       peerID,
		MessageSeq:   messageSeq,
	}, nil)
}

// ────────────────────────────────────────────────────────────────────────────
// 好友 API
// ────────────────────────────────────────────────────────────────────────────

// SendFriendNudge 向好友发送戳一戳。isSelf=true 时戳自己（显示在对方会话中）。
func (a *Adapter) SendFriendNudge(ctx stdctx.Context, userID int64, isSelf bool) error {
	return a.client.call(ctx, "send_friend_nudge", &sendFriendNudgeInput{
		UserID: userID,
		IsSelf: isSelf,
	}, nil)
}

// SendProfileLike 给指定用户点赞（名片赞）。count 为点赞次数（通常 1～20）。
func (a *Adapter) SendProfileLike(ctx stdctx.Context, userID int64, count int) error {
	return a.client.call(ctx, "send_profile_like", &sendProfileLikeInput{
		UserID: userID,
		Count:  count,
	}, nil)
}

// DeleteFriend 删除好友。
func (a *Adapter) DeleteFriend(ctx stdctx.Context, userID int64) error {
	return a.client.call(ctx, "delete_friend", &deleteFriendInput{UserID: userID}, nil)
}

// GetFriendRequests 获取好友请求列表。
// limit 为最大返回条数；isFiltered=true 时仅返回被过滤（风险账户）的请求。
func (a *Adapter) GetFriendRequests(ctx stdctx.Context, limit int, isFiltered bool) ([]FriendRequest, error) {
	var out getFriendRequestsOutput
	if err := a.client.call(ctx, "get_friend_requests", &getFriendRequestsInput{
		Limit:      limit,
		IsFiltered: isFiltered,
	}, &out); err != nil {
		return nil, err
	}
	result := make([]FriendRequest, len(out.Requests))
	for i, r := range out.Requests {
		result[i] = FriendRequest{
			Time:          r.Time,
			InitiatorID:   r.InitiatorID,
			InitiatorUID:  r.InitiatorUID,
			TargetUserID:  r.TargetUserID,
			TargetUserUID: r.TargetUserUID,
			State:         r.State,
			Comment:       r.Comment,
			Via:           r.Via,
			IsFiltered:    r.IsFiltered,
		}
	}
	return result, nil
}

// ────────────────────────────────────────────────────────────────────────────
// 群聊 API
// ────────────────────────────────────────────────────────────────────────────

// SetGroupName 修改群名称。
func (a *Adapter) SetGroupName(ctx stdctx.Context, groupID int64, name string) error {
	return a.client.call(ctx, "set_group_name", &setGroupNameInput{
		GroupID:      groupID,
		NewGroupName: name,
	}, nil)
}

// SetGroupAvatar 修改群头像。imageURI 支持 http(s):// 或 base64:// 前缀。
func (a *Adapter) SetGroupAvatar(ctx stdctx.Context, groupID int64, imageURI string) error {
	return a.client.call(ctx, "set_group_avatar", &setGroupAvatarInput{
		GroupID:  groupID,
		ImageURI: imageURI,
	}, nil)
}

// SetGroupMemberCard 修改群成员名片。card 为空字符串时清除名片。
func (a *Adapter) SetGroupMemberCard(ctx stdctx.Context, groupID, userID int64, card string) error {
	return a.client.call(ctx, "set_group_member_card", &setGroupMemberCardInput{
		GroupID: groupID,
		UserID:  userID,
		Card:    card,
	}, nil)
}

// SetGroupMemberSpecialTitle 设置群成员专属头衔。title 为空字符串时清除头衔。
func (a *Adapter) SetGroupMemberSpecialTitle(ctx stdctx.Context, groupID, userID int64, title string) error {
	return a.client.call(ctx, "set_group_member_special_title", &setGroupMemberSpecialTitleInput{
		GroupID:      groupID,
		UserID:       userID,
		SpecialTitle: title,
	}, nil)
}

// GetGroupAnnouncements 获取群公告列表。
func (a *Adapter) GetGroupAnnouncements(ctx stdctx.Context, groupID int64) ([]Announcement, error) {
	var out getGroupAnnouncementsOutput
	if err := a.client.call(ctx, "get_group_announcements", &getGroupAnnouncementsInput{GroupID: groupID}, &out); err != nil {
		return nil, err
	}
	result := make([]Announcement, len(out.Announcements))
	for i, ann := range out.Announcements {
		result[i] = Announcement{
			GroupID:        ann.GroupID,
			AnnouncementID: ann.AnnouncementID,
			UserID:         ann.UserID,
			Time:           ann.Time,
			Content:        ann.Content,
			ImageURL:       ann.ImageURL,
		}
	}
	return result, nil
}

// SendGroupAnnouncement 发布群公告。imageURI 为可选配图，留空表示纯文字公告。
func (a *Adapter) SendGroupAnnouncement(ctx stdctx.Context, groupID int64, content string, imageURI string) error {
	input := &sendGroupAnnouncementInput{
		GroupID: groupID,
		Content: content,
	}
	if imageURI != "" {
		input.ImageURI = &imageURI
	}
	return a.client.call(ctx, "send_group_announcement", input, nil)
}

// DeleteGroupAnnouncement 删除群公告。
func (a *Adapter) DeleteGroupAnnouncement(ctx stdctx.Context, groupID int64, announcementID string) error {
	return a.client.call(ctx, "delete_group_announcement", &deleteGroupAnnouncementInput{
		GroupID:        groupID,
		AnnouncementID: announcementID,
	}, nil)
}

// GetGroupEssenceMessages 获取群精华消息列表（分页）。
// 返回消息列表和是否已到末尾的标志。
func (a *Adapter) GetGroupEssenceMessages(ctx stdctx.Context, groupID int64, pageIndex, pageSize int) ([]EssenceMessage, bool, error) {
	var out getGroupEssenceMessagesOutput
	if err := a.client.call(ctx, "get_group_essence_messages", &getGroupEssenceMessagesInput{
		GroupID:   groupID,
		PageIndex: pageIndex,
		PageSize:  pageSize,
	}, &out); err != nil {
		return nil, false, err
	}
	result := make([]EssenceMessage, len(out.Messages))
	for i, m := range out.Messages {
		result[i] = EssenceMessage{
			GroupID:       m.GroupID,
			MessageSeq:    m.MessageSeq,
			MessageTime:   m.MessageTime,
			SenderID:      m.SenderID,
			SenderName:    m.SenderName,
			OperatorID:    m.OperatorID,
			OperatorName:  m.OperatorName,
			OperationTime: m.OperationTime,
			Segments:      toMessageSegments(m.Segments),
		}
	}
	return result, out.IsEnd, nil
}

// SetGroupEssenceMessage 设置或取消群精华消息。isSet=true 设置，false 取消。
func (a *Adapter) SetGroupEssenceMessage(ctx stdctx.Context, groupID, messageSeq int64, isSet bool) error {
	return a.client.call(ctx, "set_group_essence_message", &setGroupEssenceMessageInput{
		GroupID:    groupID,
		MessageSeq: messageSeq,
		IsSet:      isSet,
	}, nil)
}

// QuitGroup 退出群。
func (a *Adapter) QuitGroup(ctx stdctx.Context, groupID int64) error {
	return a.client.call(ctx, "quit_group", &quitGroupInput{GroupID: groupID}, nil)
}

// SendGroupNudge 在群内发送戳一戳。
func (a *Adapter) SendGroupNudge(ctx stdctx.Context, groupID, userID int64) error {
	return a.client.call(ctx, "send_group_nudge", &sendGroupNudgeInput{
		GroupID: groupID,
		UserID:  userID,
	}, nil)
}

// GetGroupNotifications 获取群通知列表（入群申请、邀请他人入群等）。
//
// startSeq 为起始通知序列号（nil 表示从最新开始）；
// isFiltered=true 时仅返回来自风险账户的通知；limit 为最大返回条数。
// 返回通知列表和下一页起始序列号（nil 表示已到末尾）。
func (a *Adapter) GetGroupNotifications(ctx stdctx.Context, startSeq *int64, isFiltered bool, limit int) ([]GroupNotification, *int64, error) {
	var out getGroupNotificationsOutput
	if err := a.client.call(ctx, "get_group_notifications", &getGroupNotificationsInput{
		StartNotificationSeq: startSeq,
		IsFiltered:           isFiltered,
		Limit:                limit,
	}, &out); err != nil {
		return nil, nil, err
	}
	result := make([]GroupNotification, len(out.Notifications))
	for i, n := range out.Notifications {
		result[i] = GroupNotification{
			Type:            n.Type,
			GroupID:         n.GroupID,
			NotificationSeq: n.NotificationSeq,
			IsFiltered:      n.IsFiltered,
			InitiatorID:     n.InitiatorID,
			TargetUserID:    n.TargetUserID,
			State:           n.State,
			OperatorID:      n.OperatorID,
			Comment:         n.Comment,
			IsSet:           n.IsSet,
		}
	}
	return result, out.NextNotificationSeq, nil
}

// AcceptGroupRequest 同意入群请求或邀请他人入群请求。
//
// notificationType 为 "join_request"（主动申请）或 "invited_join_request"（被邀请）。
// isFiltered 须与 GetGroupNotifications 返回值中对应通知的 IsFiltered 字段一致。
func (a *Adapter) AcceptGroupRequest(ctx stdctx.Context, groupID, notificationSeq int64, notificationType string, isFiltered bool) error {
	return a.client.call(ctx, "accept_group_request", &acceptGroupRequestInput{
		NotificationSeq:  notificationSeq,
		NotificationType: notificationType,
		GroupID:          groupID,
		IsFiltered:       isFiltered,
	}, nil)
}

// RejectGroupRequest 拒绝入群请求或邀请他人入群请求。
//
// reason 为拒绝原因（可为空字符串）。
func (a *Adapter) RejectGroupRequest(ctx stdctx.Context, groupID, notificationSeq int64, notificationType string, isFiltered bool, reason string) error {
	return a.client.call(ctx, "reject_group_request", &rejectGroupRequestInput{
		NotificationSeq:  notificationSeq,
		NotificationType: notificationType,
		GroupID:          groupID,
		IsFiltered:       isFiltered,
		Reason:           reason,
	}, nil)
}

// ────────────────────────────────────────────────────────────────────────────
// 文件 API
// ────────────────────────────────────────────────────────────────────────────

// UploadPrivateFile 向好友发送文件。
// fileURI 支持 http(s):// 或 base64:// 前缀；fileName 为发送时显示的文件名。
// 返回 fileID。
func (a *Adapter) UploadPrivateFile(ctx stdctx.Context, userID int64, fileURI, fileName string) (string, error) {
	var out uploadFileOutput
	if err := a.client.call(ctx, "upload_private_file", &uploadPrivateFileInput{
		UserID:   userID,
		FileURI:  fileURI,
		FileName: fileName,
	}, &out); err != nil {
		return "", err
	}
	return out.FileID, nil
}

// UploadGroupFile 向群上传文件。
// parentFolderID 为目标文件夹 ID（根目录留空字符串）。
// 返回 fileID。
func (a *Adapter) UploadGroupFile(ctx stdctx.Context, groupID int64, parentFolderID, fileURI, fileName string) (string, error) {
	var out uploadFileOutput
	if err := a.client.call(ctx, "upload_group_file", &uploadGroupFileInput{
		GroupID:        groupID,
		ParentFolderID: parentFolderID,
		FileURI:        fileURI,
		FileName:       fileName,
	}, &out); err != nil {
		return "", err
	}
	return out.FileID, nil
}

// GetPrivateFileDownloadURL 获取私聊文件的下载链接。
// fileHash 为文件的哈希值（由文件消息段的 Extra 携带）。
func (a *Adapter) GetPrivateFileDownloadURL(ctx stdctx.Context, userID int64, fileID, fileHash string) (string, error) {
	var out fileDownloadURLOutput
	if err := a.client.call(ctx, "get_private_file_download_url", &getPrivateFileDownloadURLInput{
		UserID:   userID,
		FileID:   fileID,
		FileHash: fileHash,
	}, &out); err != nil {
		return "", err
	}
	return out.DownloadURL, nil
}

// GetGroupFileDownloadURL 获取群文件的下载链接。
func (a *Adapter) GetGroupFileDownloadURL(ctx stdctx.Context, groupID int64, fileID string) (string, error) {
	var out fileDownloadURLOutput
	if err := a.client.call(ctx, "get_group_file_download_url", &getGroupFileDownloadURLInput{
		GroupID: groupID,
		FileID:  fileID,
	}, &out); err != nil {
		return "", err
	}
	return out.DownloadURL, nil
}

// GetGroupFiles 获取群文件列表。parentFolderID 为文件夹 ID（根目录留空字符串）。
func (a *Adapter) GetGroupFiles(ctx stdctx.Context, groupID int64, parentFolderID string) ([]GroupFile, []GroupFolder, error) {
	var out getGroupFilesOutput
	if err := a.client.call(ctx, "get_group_files", &getGroupFilesInput{
		GroupID:        groupID,
		ParentFolderID: parentFolderID,
	}, &out); err != nil {
		return nil, nil, err
	}
	files := make([]GroupFile, len(out.Files))
	for i, f := range out.Files {
		files[i] = GroupFile{
			GroupID:         f.GroupID,
			FileID:          f.FileID,
			FileName:        f.FileName,
			ParentFolderID:  f.ParentFolderID,
			FileSize:        f.FileSize,
			UploadedTime:    f.UploadedTime,
			ExpireTime:      f.ExpireTime,
			UploaderID:      f.UploaderID,
			DownloadedTimes: f.DownloadedTimes,
		}
	}
	folders := make([]GroupFolder, len(out.Folders))
	for i, f := range out.Folders {
		folders[i] = GroupFolder{
			GroupID:          f.GroupID,
			FolderID:         f.FolderID,
			ParentFolderID:   f.ParentFolderID,
			FolderName:       f.FolderName,
			CreatedTime:      f.CreatedTime,
			LastModifiedTime: f.LastModifiedTime,
			CreatorID:        f.CreatorID,
			FileCount:        f.FileCount,
		}
	}
	return files, folders, nil
}

// MoveGroupFile 移动群文件到另一个文件夹。
func (a *Adapter) MoveGroupFile(ctx stdctx.Context, groupID int64, fileID, parentFolderID, targetFolderID string) error {
	return a.client.call(ctx, "move_group_file", &moveGroupFileInput{
		GroupID:        groupID,
		FileID:         fileID,
		ParentFolderID: parentFolderID,
		TargetFolderID: targetFolderID,
	}, nil)
}

// RenameGroupFile 重命名群文件。
func (a *Adapter) RenameGroupFile(ctx stdctx.Context, groupID int64, fileID, parentFolderID, newFileName string) error {
	return a.client.call(ctx, "rename_group_file", &renameGroupFileInput{
		GroupID:        groupID,
		FileID:         fileID,
		ParentFolderID: parentFolderID,
		NewFileName:    newFileName,
	}, nil)
}

// DeleteGroupFile 删除群文件。
func (a *Adapter) DeleteGroupFile(ctx stdctx.Context, groupID int64, fileID string) error {
	return a.client.call(ctx, "delete_group_file", &deleteGroupFileInput{
		GroupID: groupID,
		FileID:  fileID,
	}, nil)
}

// CreateGroupFolder 在群文件根目录下创建文件夹，返回新建文件夹的 ID。
func (a *Adapter) CreateGroupFolder(ctx stdctx.Context, groupID int64, folderName string) (string, error) {
	var out createGroupFolderOutput
	if err := a.client.call(ctx, "create_group_folder", &createGroupFolderInput{
		GroupID:    groupID,
		FolderName: folderName,
	}, &out); err != nil {
		return "", err
	}
	return out.FolderID, nil
}

// RenameGroupFolder 重命名群文件夹。
func (a *Adapter) RenameGroupFolder(ctx stdctx.Context, groupID int64, folderID, newFolderName string) error {
	return a.client.call(ctx, "rename_group_folder", &renameGroupFolderInput{
		GroupID:       groupID,
		FolderID:      folderID,
		NewFolderName: newFolderName,
	}, nil)
}

// DeleteGroupFolder 删除群文件夹。
func (a *Adapter) DeleteGroupFolder(ctx stdctx.Context, groupID int64, folderID string) error {
	return a.client.call(ctx, "delete_group_folder", &deleteGroupFolderInput{
		GroupID:  groupID,
		FolderID: folderID,
	}, nil)
}

// ────────────────────────────────────────────────────────────────────────────
// 便捷辅助：将字符串 UIN 解析为 int64（供外部调用方使用）
// ────────────────────────────────────────────────────────────────────────────

// ParseUIN 将 QQ 号字符串解析为 int64。
// 支持纯数字字符串（如 "123456789"）以及 encodeChatID 产生的 "scene:peerID" 格式。
func ParseUIN(s string) (int64, error) {
	return parseUin(s, "uin")
}

// FormatUIN 将 int64 QQ 号格式化为字符串。
func FormatUIN(uin int64) string {
	return strconv.FormatInt(uin, 10)
}

// ────────────────────────────────────────────────────────────────────────────
// 内部辅助：将 OutgoingSegment 切片转换为线上格式
// ────────────────────────────────────────────────────────────────────────────

// buildWireSegments 将 []OutgoingSegment 转换为 Milky 内部线上消息段切片。
func buildWireSegments(segs []OutgoingSegment) []outgoingSegment {
	result := make([]outgoingSegment, 0, len(segs))
	for _, s := range segs {
		if wire := convertOutgoingSegment(s); wire != nil {
			result = append(result, *wire)
		}
	}
	return result
}

// ────────────────────────────────────────────────────────────────────────────
// 消息发送/撤回 API（直接调用 Milky 协议，不经过 platform.Sender）
// ────────────────────────────────────────────────────────────────────────────

// SendPrivateMessage 发送私聊消息，返回消息序列号和服务端时间戳。
//
// 使用 [TextSegment]、[ImageSegment] 等类型构造 segments。
func (a *Adapter) SendPrivateMessage(ctx stdctx.Context, userID int64, segments []OutgoingSegment) (messageSeq int64, sentAt time.Time, err error) {
	wires := buildWireSegments(segments)
	if len(wires) == 0 {
		err = errutil.ErrEmptyMessage
		return
	}
	var out sendMessageOutput
	if err = a.client.call(ctx, "send_private_message", &sendPrivateMessageInput{
		UserID:  userID,
		Message: wires,
	}, &out); err != nil {
		return
	}
	return out.MessageSeq, time.Unix(out.Time, 0), nil
}

// SendGroupMessage 发送群聊消息，返回消息序列号和服务端时间戳。
//
// 使用 [TextSegment]、[ImageSegment] 等类型构造 segments。
func (a *Adapter) SendGroupMessage(ctx stdctx.Context, groupID int64, segments []OutgoingSegment) (messageSeq int64, sentAt time.Time, err error) {
	wires := buildWireSegments(segments)
	if len(wires) == 0 {
		err = errutil.ErrEmptyMessage
		return
	}
	var out sendMessageOutput
	if err = a.client.call(ctx, "send_group_message", &sendGroupMessageInput{
		GroupID: groupID,
		Message: wires,
	}, &out); err != nil {
		return
	}
	return out.MessageSeq, time.Unix(out.Time, 0), nil
}

// RecallPrivateMessage 撤回私聊消息。
func (a *Adapter) RecallPrivateMessage(ctx stdctx.Context, userID, messageSeq int64) error {
	return a.client.call(ctx, "recall_private_message", &recallPrivateMessageInput{
		UserID:     userID,
		MessageSeq: messageSeq,
	}, nil)
}

// RecallGroupMessage 撤回群聊消息。
func (a *Adapter) RecallGroupMessage(ctx stdctx.Context, groupID, messageSeq int64) error {
	return a.client.call(ctx, "recall_group_message", &recallGroupMessageInput{
		GroupID:    groupID,
		MessageSeq: messageSeq,
	}, nil)
}

// ────────────────────────────────────────────────────────────────────────────
// 群成员管理 API（直接调用，不经过 platform.GroupManager）
// ────────────────────────────────────────────────────────────────────────────

// KickGroupMember 将用户从群中移除。
// rejectAddRequest=true 时同时拒绝该用户今后的入群申请。
func (a *Adapter) KickGroupMember(ctx stdctx.Context, groupID, userID int64, rejectAddRequest bool) error {
	return a.client.call(ctx, "kick_group_member", &kickGroupMemberInput{
		GroupID:          groupID,
		UserID:           userID,
		RejectAddRequest: rejectAddRequest,
	}, nil)
}

// SetGroupMemberMute 禁言或解禁群成员。
// duration 为禁言时长（秒），0 表示解禁。
func (a *Adapter) SetGroupMemberMute(ctx stdctx.Context, groupID, userID int64, duration int64) error {
	return a.client.call(ctx, "set_group_member_mute", &setGroupMemberMuteInput{
		GroupID:  groupID,
		UserID:   userID,
		Duration: duration,
	}, nil)
}

// SetGroupWholeMute 开启或关闭全群禁言。
func (a *Adapter) SetGroupWholeMute(ctx stdctx.Context, groupID int64, isMute bool) error {
	return a.client.call(ctx, "set_group_whole_mute", &setGroupWholeMuteInput{
		GroupID: groupID,
		IsMute:  isMute,
	}, nil)
}

// SetGroupMemberAdmin 授予或撤销群成员的管理员权限。
// isSet=true 授予，false 撤销。
func (a *Adapter) SetGroupMemberAdmin(ctx stdctx.Context, groupID, userID int64, isSet bool) error {
	return a.client.call(ctx, "set_group_member_admin", &setGroupMemberAdminInput{
		GroupID: groupID,
		UserID:  userID,
		IsSet:   isSet,
	}, nil)
}

// SendGroupMessageReaction 为群消息添加或取消表情回应。
//
// reaction 为表情 ID（face 类型）或 Unicode 字符（emoji 类型）。
// reactionType 为 "face"（QQ 系统表情）或 "emoji"（Unicode 表情）。
// isAdd=true 添加回应，false 取消回应。
func (a *Adapter) SendGroupMessageReaction(ctx stdctx.Context, groupID, messageSeq int64, reaction, reactionType string, isAdd bool) error {
	return a.client.call(ctx, "send_group_message_reaction", &sendGroupMessageReactionInput{
		GroupID:      groupID,
		MessageSeq:   messageSeq,
		Reaction:     reaction,
		ReactionType: reactionType,
		IsAdd:        isAdd,
	}, nil)
}

// ────────────────────────────────────────────────────────────────────────────
// 好友/群请求处理 API（直接调用，不经过 platform.InvitationHandler）
// ────────────────────────────────────────────────────────────────────────────

// AcceptFriendRequest 同意好友请求。
//
// initiatorUID 为请求发起者 UID（由 FriendRequest.InitiatorUID 或好友请求事件携带）。
// isFiltered 须与对应请求的 IsFiltered 字段一致。
func (a *Adapter) AcceptFriendRequest(ctx stdctx.Context, initiatorUID string, isFiltered bool) error {
	return a.client.call(ctx, "accept_friend_request", &acceptFriendRequestInput{
		InitiatorUID: initiatorUID,
		IsFiltered:   isFiltered,
	}, nil)
}

// RejectFriendRequest 拒绝好友请求。
//
// reason 为拒绝原因（可为空字符串）。
// isFiltered 须与对应请求的 IsFiltered 字段一致。
func (a *Adapter) RejectFriendRequest(ctx stdctx.Context, initiatorUID, reason string, isFiltered bool) error {
	return a.client.call(ctx, "reject_friend_request", &rejectFriendRequestInput{
		InitiatorUID: initiatorUID,
		IsFiltered:   isFiltered,
		Reason:       reason,
	}, nil)
}

// AcceptGroupInvitation 接受邀请机器人加入某群的邀请。
//
// invitationSeq 由 group_invitation 事件携带。
func (a *Adapter) AcceptGroupInvitation(ctx stdctx.Context, groupID, invitationSeq int64) error {
	return a.client.call(ctx, "accept_group_invitation", &acceptGroupInvitationInput{
		GroupID:       groupID,
		InvitationSeq: invitationSeq,
	}, nil)
}

// RejectGroupInvitation 拒绝邀请机器人加入某群的邀请。
//
// invitationSeq 由 group_invitation 事件携带。
func (a *Adapter) RejectGroupInvitation(ctx stdctx.Context, groupID, invitationSeq int64) error {
	return a.client.call(ctx, "reject_group_invitation", &rejectGroupInvitationInput{
		GroupID:       groupID,
		InvitationSeq: invitationSeq,
	}, nil)
}
