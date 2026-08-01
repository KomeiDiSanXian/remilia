package milky

import "encoding/json"

// ────────────────────────────────────────────────────────────────────────────
// API 请求/响应类型（内部使用，不对外暴露）
// ────────────────────────────────────────────────────────────────────────────

// ── 系统 ─────────────────────────────────────────────────────────────────────

type getLoginInfoOutput struct {
	Uin      int64  `json:"uin"`
	Nickname string `json:"nickname"`
}

type getImplInfoOutput struct {
	ImplName          string `json:"impl_name"`
	ImplVersion       string `json:"impl_version"`
	QQProtocolVersion string `json:"qq_protocol_version"`
	QQProtocolType    string `json:"qq_protocol_type"`
	MilkyVersion      string `json:"milky_version"`
}

type getUserProfileInput struct {
	UserID int64 `json:"user_id"`
}

type getUserProfileOutput struct {
	Nickname string `json:"nickname"`
	QID      string `json:"qid"`
	Age      int    `json:"age"`
	Sex      string `json:"sex"`
	Remark   string `json:"remark"`
	Bio      string `json:"bio"`
	Level    int    `json:"level"`
	Country  string `json:"country"`
	City     string `json:"city"`
	School   string `json:"school"`
}

type friendCategoryJSON struct {
	CategoryID   int    `json:"category_id"`
	CategoryName string `json:"category_name"`
}

type friendInfoJSON struct {
	UserID   int64              `json:"user_id"`
	Nickname string             `json:"nickname"`
	Sex      string             `json:"sex"`
	QID      string             `json:"qid"`
	Remark   string             `json:"remark"`
	Category friendCategoryJSON `json:"category"`
}

type getFriendListInput struct {
	NoCache bool `json:"no_cache"`
}

type getFriendListOutput struct {
	Friends []friendInfoJSON `json:"friends"`
}

type getFriendInfoInput struct {
	UserID  int64 `json:"user_id"`
	NoCache bool  `json:"no_cache"`
}

type getFriendInfoOutput struct {
	Friend friendInfoJSON `json:"friend"`
}

type groupInfoJSON struct {
	GroupID        int64  `json:"group_id"`
	GroupName      string `json:"group_name"`
	MemberCount    int    `json:"member_count"`
	MaxMemberCount int    `json:"max_member_count"`
	Remark         string `json:"remark"`
	CreatedTime    int64  `json:"created_time"`
	Description    string `json:"description"`
	Question       string `json:"question"`
	Announcement   string `json:"announcement"`
}
type getGroupListInput struct {
	NoCache bool `json:"no_cache"`
}

type getGroupListOutput struct {
	Groups []groupInfoJSON `json:"groups"`
}

type getGroupInfoInput struct {
	GroupID int64 `json:"group_id"`
	NoCache bool  `json:"no_cache"`
}

type getGroupInfoOutput struct {
	Group groupInfoJSON `json:"group"`
}

type groupMemberInfoJSON struct {
	UserID        int64  `json:"user_id"`
	GroupID       int64  `json:"group_id"`
	Nickname      string `json:"nickname"`
	Card          string `json:"card"`
	Sex           string `json:"sex"`
	Title         string `json:"title"`
	Level         int    `json:"level"`
	Role          string `json:"role"`
	JoinTime      int64  `json:"join_time"`
	LastSentTime  int64  `json:"last_sent_time"`
	ShutUpEndTime *int64 `json:"shut_up_end_time,omitempty"`
}
type getGroupMemberListInput struct {
	GroupID int64 `json:"group_id"`
	NoCache bool  `json:"no_cache"`
}

type getGroupMemberListOutput struct {
	Members []groupMemberInfoJSON `json:"members"`
}

type getGroupMemberInfoInput struct {
	GroupID int64 `json:"group_id"`
	UserID  int64 `json:"user_id"`
	NoCache bool  `json:"no_cache"`
}

type getGroupMemberInfoOutput struct {
	Member groupMemberInfoJSON `json:"member"`
}

type getPeerPinsOutput struct {
	Friends []friendInfoJSON `json:"friends"`
	Groups  []groupInfoJSON  `json:"groups"`
}
type setPeerPinInput struct {
	MessageScene string `json:"message_scene"`
	PeerID       int64  `json:"peer_id"`
	IsPinned     bool   `json:"is_pinned"`
}

type setAvatarInput struct {
	URI string `json:"uri"`
}

type setNicknameInput struct {
	NewNickname string `json:"new_nickname"`
}

type setBioInput struct {
	NewBio string `json:"new_bio"`
}

type getCustomFaceURLListOutput struct {
	URLs []string `json:"urls"`
}

type getCookiesInput struct {
	Domain string `json:"domain"`
}

type getCookiesOutput struct {
	Cookies string `json:"cookies"`
}

type getCSRFTokenOutput struct {
	CSRFToken string `json:"csrf_token"`
}

// ── 消息 ─────────────────────────────────────────────────────────────────────

type sendPrivateMessageInput struct {
	UserID  int64             `json:"user_id"`
	Message []outgoingSegment `json:"message"`
}

type sendGroupMessageInput struct {
	GroupID int64             `json:"group_id"`
	Message []outgoingSegment `json:"message"`
}

type sendMessageOutput struct {
	MessageSeq int64 `json:"message_seq"`
	Time       int64 `json:"time"`
}

type recallPrivateMessageInput struct {
	UserID     int64 `json:"user_id"`
	MessageSeq int64 `json:"message_seq"`
}

type recallGroupMessageInput struct {
	GroupID    int64 `json:"group_id"`
	MessageSeq int64 `json:"message_seq"`
}

type getMessageInput struct {
	MessageScene string `json:"message_scene"`
	PeerID       int64  `json:"peer_id"`
	MessageSeq   int64  `json:"message_seq"`
}

type getMessageOutput struct {
	Message incomingMessage `json:"message"`
}

type getHistoryMessagesInput struct {
	MessageScene    string `json:"message_scene"`
	PeerID          int64  `json:"peer_id"`
	StartMessageSeq *int64 `json:"start_message_seq,omitempty"`
	Limit           int    `json:"limit,omitempty"`
}

type getHistoryMessagesOutput struct {
	Messages       []incomingMessage `json:"messages"`
	NextMessageSeq *int64            `json:"next_message_seq,omitempty"`
}

type getResourceTempURLInput struct {
	ResourceID string `json:"resource_id"`
}

type getResourceTempURLOutput struct {
	URL string `json:"url"`
}

type getForwardedMessagesInput struct {
	ForwardID string `json:"forward_id"`
}

type forwardedMessageJSON struct {
	MessageSeq int64             `json:"message_seq"`
	SenderName string            `json:"sender_name"`
	AvatarURL  string            `json:"avatar_url"`
	Time       int64             `json:"time"`
	Segments   []incomingSegment `json:"segments"`
}

type getForwardedMessagesOutput struct {
	Messages []forwardedMessageJSON `json:"messages"`
}

type markMessageAsReadInput struct {
	MessageScene string `json:"message_scene"`
	PeerID       int64  `json:"peer_id"`
	MessageSeq   int64  `json:"message_seq"`
}

// ── 好友 ─────────────────────────────────────────────────────────────────────

type sendFriendNudgeInput struct {
	UserID int64 `json:"user_id"`
	IsSelf bool  `json:"is_self"`
}

type sendProfileLikeInput struct {
	UserID int64 `json:"user_id"`
	Count  int   `json:"count"`
}

type deleteFriendInput struct {
	UserID int64 `json:"user_id"`
}

type getFriendRequestsInput struct {
	Limit      int  `json:"limit"`
	IsFiltered bool `json:"is_filtered"`
}

type friendRequestJSON struct {
	Time          int64  `json:"time"`
	InitiatorID   int64  `json:"initiator_id"`
	InitiatorUID  string `json:"initiator_uid"`
	TargetUserID  int64  `json:"target_user_id"`
	TargetUserUID string `json:"target_user_uid"`
	State         string `json:"state"`
	Comment       string `json:"comment"`
	Via           string `json:"via"`
	IsFiltered    bool   `json:"is_filtered"`
}

type getFriendRequestsOutput struct {
	Requests []friendRequestJSON `json:"requests"`
}

type acceptFriendRequestInput struct {
	InitiatorUID string `json:"initiator_uid"`
	IsFiltered   bool   `json:"is_filtered"`
}

type rejectFriendRequestInput struct {
	InitiatorUID string `json:"initiator_uid"`
	IsFiltered   bool   `json:"is_filtered"`
	Reason       string `json:"reason,omitempty"`
}

// ── 群组管理 ─────────────────────────────────────────────────────────────────

type kickGroupMemberInput struct {
	GroupID          int64 `json:"group_id"`
	UserID           int64 `json:"user_id"`
	RejectAddRequest bool  `json:"reject_add_request"`
}

type setGroupMemberMuteInput struct {
	GroupID  int64 `json:"group_id"`
	UserID   int64 `json:"user_id"`
	Duration int64 `json:"duration"` // 秒；0 表示解禁
}

type setGroupWholeMuteInput struct {
	GroupID int64 `json:"group_id"`
	IsMute  bool  `json:"is_mute"`
}

type setGroupMemberAdminInput struct {
	GroupID int64 `json:"group_id"`
	UserID  int64 `json:"user_id"`
	IsSet   bool  `json:"is_set"`
}

type sendGroupMessageReactionInput struct {
	GroupID      int64  `json:"group_id"`
	MessageSeq   int64  `json:"message_seq"`
	Reaction     string `json:"reaction"`
	ReactionType string `json:"reaction_type"` // "face" 或 "emoji"
	IsAdd        bool   `json:"is_add"`
}

type setGroupNameInput struct {
	GroupID      int64  `json:"group_id"`
	NewGroupName string `json:"new_group_name"`
}

type setGroupAvatarInput struct {
	GroupID  int64  `json:"group_id"`
	ImageURI string `json:"image_uri"`
}

type setGroupMemberCardInput struct {
	GroupID int64  `json:"group_id"`
	UserID  int64  `json:"user_id"`
	Card    string `json:"card"`
}

type setGroupMemberSpecialTitleInput struct {
	GroupID      int64  `json:"group_id"`
	UserID       int64  `json:"user_id"`
	SpecialTitle string `json:"special_title"`
}

type getGroupAnnouncementsInput struct {
	GroupID int64 `json:"group_id"`
}

type announcementJSON struct {
	GroupID        int64   `json:"group_id"`
	AnnouncementID string  `json:"announcement_id"`
	UserID         int64   `json:"user_id"`
	Time           int64   `json:"time"`
	Content        string  `json:"content"`
	ImageURL       *string `json:"image_url,omitempty"`
}

type getGroupAnnouncementsOutput struct {
	Announcements []announcementJSON `json:"announcements"`
}

type sendGroupAnnouncementInput struct {
	GroupID  int64   `json:"group_id"`
	Content  string  `json:"content"`
	ImageURI *string `json:"image_uri,omitempty"`
}

type deleteGroupAnnouncementInput struct {
	GroupID        int64  `json:"group_id"`
	AnnouncementID string `json:"announcement_id"`
}

type getGroupEssenceMessagesInput struct {
	GroupID   int64 `json:"group_id"`
	PageIndex int   `json:"page_index"`
	PageSize  int   `json:"page_size"`
}

type essenceMessageJSON struct {
	GroupID       int64             `json:"group_id"`
	MessageSeq    int64             `json:"message_seq"`
	MessageTime   int64             `json:"message_time"`
	SenderID      int64             `json:"sender_id"`
	SenderName    string            `json:"sender_name"`
	OperatorID    int64             `json:"operator_id"`
	OperatorName  string            `json:"operator_name"`
	OperationTime int64             `json:"operation_time"`
	Segments      []incomingSegment `json:"segments"`
}

type getGroupEssenceMessagesOutput struct {
	Messages []essenceMessageJSON `json:"messages"`
	IsEnd    bool                 `json:"is_end"`
}

type setGroupEssenceMessageInput struct {
	GroupID    int64 `json:"group_id"`
	MessageSeq int64 `json:"message_seq"`
	IsSet      bool  `json:"is_set"`
}

type quitGroupInput struct {
	GroupID int64 `json:"group_id"`
}

type sendGroupNudgeInput struct {
	GroupID int64 `json:"group_id"`
	UserID  int64 `json:"user_id"`
}

type getGroupNotificationsInput struct {
	StartNotificationSeq *int64 `json:"start_notification_seq,omitempty"`
	IsFiltered           bool   `json:"is_filtered"`
	Limit                int    `json:"limit"`
}

type groupNotificationJSON struct {
	OperatorID      *int64 `json:"operator_id,omitempty"`
	Type            string `json:"type"`
	State           string `json:"state"`
	Comment         string `json:"comment"`
	GroupID         int64  `json:"group_id"`
	NotificationSeq int64  `json:"notification_seq"`
	InitiatorID     int64  `json:"initiator_id"`
	TargetUserID    int64  `json:"target_user_id"`
	IsFiltered      bool   `json:"is_filtered"`
	IsSet           bool   `json:"is_set"`
}

type getGroupNotificationsOutput struct {
	Notifications       []groupNotificationJSON `json:"notifications"`
	NextNotificationSeq *int64                  `json:"next_notification_seq,omitempty"`
}

// ── 群邀请/申请 ───────────────────────────────────────────────────────────────

type acceptGroupInvitationInput struct {
	GroupID       int64 `json:"group_id"`
	InvitationSeq int64 `json:"invitation_seq"`
}

type rejectGroupInvitationInput struct {
	GroupID       int64 `json:"group_id"`
	InvitationSeq int64 `json:"invitation_seq"`
}

type acceptGroupRequestInput struct {
	NotificationSeq  int64  `json:"notification_seq"`
	NotificationType string `json:"notification_type"` // "join_request" 或 "invited_join_request"
	GroupID          int64  `json:"group_id"`
	IsFiltered       bool   `json:"is_filtered"`
}

type rejectGroupRequestInput struct {
	NotificationSeq  int64  `json:"notification_seq"`
	NotificationType string `json:"notification_type"`
	GroupID          int64  `json:"group_id"`
	IsFiltered       bool   `json:"is_filtered"`
	Reason           string `json:"reason,omitempty"`
}

// ── 文件 ─────────────────────────────────────────────────────────────────────

type uploadPrivateFileInput struct {
	UserID   int64  `json:"user_id"`
	FileURI  string `json:"file_uri"`
	FileName string `json:"file_name"`
}

type uploadGroupFileInput struct {
	GroupID        int64  `json:"group_id"`
	ParentFolderID string `json:"parent_folder_id"`
	FileURI        string `json:"file_uri"`
	FileName       string `json:"file_name"`
}

type uploadFileOutput struct {
	FileID string `json:"file_id"`
}

type getPrivateFileDownloadURLInput struct {
	UserID   int64  `json:"user_id"`
	FileID   string `json:"file_id"`
	FileHash string `json:"file_hash"`
}

type getGroupFileDownloadURLInput struct {
	GroupID int64  `json:"group_id"`
	FileID  string `json:"file_id"`
}

type fileDownloadURLOutput struct {
	DownloadURL string `json:"download_url"`
}

type getGroupFilesInput struct {
	GroupID        int64  `json:"group_id"`
	ParentFolderID string `json:"parent_folder_id"`
}

type groupFileJSON struct {
	GroupID         int64  `json:"group_id"`
	FileID          string `json:"file_id"`
	FileName        string `json:"file_name"`
	ParentFolderID  string `json:"parent_folder_id"`
	FileSize        int64  `json:"file_size"`
	UploadedTime    int64  `json:"uploaded_time"`
	ExpireTime      *int64 `json:"expire_time,omitempty"`
	UploaderID      int64  `json:"uploader_id"`
	DownloadedTimes int    `json:"downloaded_times"`
}

type groupFolderJSON struct {
	GroupID          int64  `json:"group_id"`
	FolderID         string `json:"folder_id"`
	ParentFolderID   string `json:"parent_folder_id"`
	FolderName       string `json:"folder_name"`
	CreatedTime      int64  `json:"created_time"`
	LastModifiedTime int64  `json:"last_modified_time"`
	CreatorID        int64  `json:"creator_id"`
	FileCount        int    `json:"file_count"`
}

type getGroupFilesOutput struct {
	Files   []groupFileJSON   `json:"files"`
	Folders []groupFolderJSON `json:"folders"`
}
type moveGroupFileInput struct {
	GroupID        int64  `json:"group_id"`
	FileID         string `json:"file_id"`
	ParentFolderID string `json:"parent_folder_id"`
	TargetFolderID string `json:"target_folder_id"`
}

type renameGroupFileInput struct {
	GroupID        int64  `json:"group_id"`
	FileID         string `json:"file_id"`
	ParentFolderID string `json:"parent_folder_id"`
	NewFileName    string `json:"new_file_name"`
}

type deleteGroupFileInput struct {
	GroupID int64  `json:"group_id"`
	FileID  string `json:"file_id"`
}

type createGroupFolderInput struct {
	GroupID    int64  `json:"group_id"`
	FolderName string `json:"folder_name"`
}

type createGroupFolderOutput struct {
	FolderID string `json:"folder_id"`
}

type renameGroupFolderInput struct {
	GroupID       int64  `json:"group_id"`
	FolderID      string `json:"folder_id"`
	NewFolderName string `json:"new_folder_name"`
}

type deleteGroupFolderInput struct {
	GroupID  int64  `json:"group_id"`
	FolderID string `json:"folder_id"`
}

// ────────────────────────────────────────────────────────────────────────────
// 消息段类型（发送）
// ────────────────────────────────────────────────────────────────────────────

// outgoingSegment 是 Milky 发送消息段（可辨别联合体）。
type outgoingSegment struct {
	Type string          `json:"type"`
	Data outgoingSegData `json:"data"`
}

// outgoingSegData 保存每种消息段类型的灵活载荷。
// 仅序列化相关字段（使用 omitempty）。
type outgoingSegData struct {
	// 文本
	Text string `json:"text,omitempty"`
	// 提及
	UserID int64 `json:"user_id,omitempty"`
	// 表情
	FaceID  string `json:"face_id,omitempty"`
	IsLarge bool   `json:"is_large,omitempty"`
	// 引用回复
	MessageSeq int64 `json:"message_seq,omitempty"`
	// 图片
	URI     string `json:"uri,omitempty"`
	SubType string `json:"sub_type,omitempty"`
	Summary string `json:"summary,omitempty"`
	// 语音/视频
	ThumbURI string `json:"thumb_uri,omitempty"`
	// 合并转发（forward）
	ForwardMessages []outgoingForwardedMessage `json:"messages,omitempty"`
	Title           string                     `json:"title,omitempty"`
	Preview         []string                   `json:"preview,omitempty"`
	Prompt          string                     `json:"prompt,omitempty"`
	// 小程序（light_app）
	JSONPayload string `json:"json_payload,omitempty"`
}

// outgoingForwardedMessage 是合并转发消息中的单条消息。
type outgoingForwardedMessage struct {
	UserID     int64             `json:"user_id"`
	SenderName string            `json:"sender_name"`
	Segments   []outgoingSegment `json:"segments"`
}

// ────────────────────────────────────────────────────────────────────────────
// 事件类型（接收）
// ────────────────────────────────────────────────────────────────────────────

// rawEvent 是通过 WebSocket 接收的 Milky 事件最外层信封。
type rawEvent struct {
	EventType string          `json:"event_type"`
	Time      int64           `json:"time"`
	SelfID    int64           `json:"self_id"`
	Data      json.RawMessage `json:"data"` // 延迟解析
}

// incomingMessage 涵盖三种消息场景（好友/群/临时会话）。
type incomingMessage struct {
	MessageScene string            `json:"message_scene"` // "friend"、"group"、"temp"
	PeerID       int64             `json:"peer_id"`
	MessageSeq   int64             `json:"message_seq"`
	SenderID     int64             `json:"sender_id"`
	Time         int64             `json:"time"`
	Segments     []incomingSegment `json:"segments"`
	// 好友场景
	Friend *friendEntity `json:"friend,omitempty"`
	// 群场景
	Group       *groupEntity       `json:"group,omitempty"`
	GroupMember *groupMemberEntity `json:"group_member,omitempty"`
}

// incomingSegment 是 Milky 接收消息段。
type incomingSegment struct {
	Type string              `json:"type"`
	Data incomingSegmentData `json:"data"`
}

// incomingSegmentData 保存所有消息段类型的全部可能字段。
type incomingSegmentData struct {
	// 文本
	Text string `json:"text,omitempty"`
	// 提及
	UserID int64  `json:"user_id,omitempty"`
	Name   string `json:"name,omitempty"`
	// 表情
	FaceID  string `json:"face_id,omitempty"`
	IsLarge bool   `json:"is_large,omitempty"`
	// 引用回复
	MessageSeq int64  `json:"message_seq,omitempty"`
	SenderID   int64  `json:"sender_id,omitempty"`
	SenderName string `json:"sender_name,omitempty"`
	ReplyTime  int64  `json:"time,omitempty"` // 被引用消息的 Unix 时间戳
	// 图片/语音/视频
	ResourceID string `json:"resource_id,omitempty"`
	TempURL    string `json:"temp_url,omitempty"`
	SubType    string `json:"sub_type,omitempty"` // 图片子类型，例如 "normal"、"sticker"
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	Duration   int    `json:"duration,omitempty"`
	// 文件
	FileID   string `json:"file_id,omitempty"`
	FileName string `json:"file_name,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
	FileHash string `json:"file_hash,omitempty"` // TriSHA1 哈希，仅私聊文件存在
	// 合并转发
	ForwardID string   `json:"forward_id,omitempty"`
	Title     string   `json:"title,omitempty"`
	Preview   []string `json:"preview,omitempty"`
	Summary   string   `json:"summary,omitempty"`
	// 市场表情（market_face）
	EmojiPackageID int    `json:"emoji_package_id,omitempty"`
	EmojiID        string `json:"emoji_id,omitempty"`
	EmojiKey       string `json:"key,omitempty"`
	EmojiURL       string `json:"url,omitempty"`
	// 小程序（light_app）
	AppName     string `json:"app_name,omitempty"`
	JSONPayload string `json:"json_payload,omitempty"`
	// XML 消息
	ServiceID  int    `json:"service_id,omitempty"`
	XMLPayload string `json:"xml_payload,omitempty"`
}

// ── 通用实体类型（消息事件内嵌） ─────────────────────────────────────────────

type friendEntity struct {
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
	Remark   string `json:"remark"`
}

type groupEntity struct {
	GroupID   int64  `json:"group_id"`
	GroupName string `json:"group_name"`
}

type groupMemberEntity struct {
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
	Card     string `json:"card"`
	Role     string `json:"role"` // "owner"、"admin"、"member"
}

// ── 非消息事件载荷 ────────────────────────────────────────────────────────────

type groupMemberIncreaseData struct {
	GroupID    int64  `json:"group_id"`
	UserID     int64  `json:"user_id"`
	OperatorID *int64 `json:"operator_id,omitempty"`
	InvitorID  *int64 `json:"invitor_id,omitempty"`
}

type groupMemberDecreaseData struct {
	GroupID    int64  `json:"group_id"`
	UserID     int64  `json:"user_id"`
	OperatorID *int64 `json:"operator_id,omitempty"`
}

type groupAdminChangeData struct {
	GroupID    int64 `json:"group_id"`
	UserID     int64 `json:"user_id"`
	OperatorID int64 `json:"operator_id"`
	IsSet      bool  `json:"is_set"`
}

type groupMuteData struct {
	GroupID    int64 `json:"group_id"`
	UserID     int64 `json:"user_id"`
	OperatorID int64 `json:"operator_id"`
	Duration   int   `json:"duration"` // 0 表示已解禁
}

type groupWholeMuteData struct {
	GroupID    int64 `json:"group_id"`
	OperatorID int64 `json:"operator_id"`
	IsMute     bool  `json:"is_mute"`
}

type groupMessageReactionData struct {
	GroupID      int64  `json:"group_id"`
	UserID       int64  `json:"user_id"`
	MessageSeq   int64  `json:"message_seq"`
	FaceID       string `json:"face_id"`
	ReactionType string `json:"reaction_type"`
	IsAdd        bool   `json:"is_add"`
}

type messageRecallData struct {
	MessageScene  string `json:"message_scene"`
	PeerID        int64  `json:"peer_id"`
	MessageSeq    int64  `json:"message_seq"`
	SenderID      int64  `json:"sender_id"`
	OperatorID    int64  `json:"operator_id"`
	DisplaySuffix string `json:"display_suffix"`
}

type friendRequestData struct {
	InitiatorID  int64  `json:"initiator_id"`
	InitiatorUID string `json:"initiator_uid"`
	Comment      string `json:"comment"`
	Via          string `json:"via"`
}

type groupJoinRequestData struct {
	GroupID         int64  `json:"group_id"`
	NotificationSeq int64  `json:"notification_seq"`
	IsFiltered      bool   `json:"is_filtered"`
	InitiatorID     int64  `json:"initiator_id"`
	Comment         string `json:"comment"`
}

type groupInvitationData struct {
	GroupID       int64  `json:"group_id"`
	InvitationSeq int64  `json:"invitation_seq"`
	InitiatorID   int64  `json:"initiator_id"`
	SourceGroupID *int64 `json:"source_group_id,omitempty"`
}

type botOfflineData struct {
	Reason string `json:"reason"`
}

// ── 新增事件载荷 ──────────────────────────────────────────────────────────────

type peerPinChangeData struct {
	MessageScene string `json:"message_scene"`
	PeerID       int64  `json:"peer_id"`
	IsPinned     bool   `json:"is_pinned"`
}

type groupInvitedJoinRequestData struct {
	GroupID         int64 `json:"group_id"`
	NotificationSeq int64 `json:"notification_seq"`
	InitiatorID     int64 `json:"initiator_id"`
	TargetUserID    int64 `json:"target_user_id"`
}

type friendNudgeData struct {
	UserID              int64  `json:"user_id"`
	IsSelfSend          bool   `json:"is_self_send"`
	IsSelfReceive       bool   `json:"is_self_receive"`
	DisplayAction       string `json:"display_action"`
	DisplaySuffix       string `json:"display_suffix"`
	DisplayActionImgURL string `json:"display_action_img_url"`
}

type friendFileUploadData struct {
	UserID   int64  `json:"user_id"`
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"`
	FileHash string `json:"file_hash"`
	IsSelf   bool   `json:"is_self"`
}

type groupEssenceMessageChangeData struct {
	GroupID    int64 `json:"group_id"`
	MessageSeq int64 `json:"message_seq"`
	OperatorID int64 `json:"operator_id"`
	IsSet      bool  `json:"is_set"`
}

type groupNameChangeData struct {
	GroupID      int64  `json:"group_id"`
	NewGroupName string `json:"new_group_name"`
	OperatorID   int64  `json:"operator_id"`
}

type groupNudgeData struct {
	GroupID             int64  `json:"group_id"`
	SenderID            int64  `json:"sender_id"`
	ReceiverID          int64  `json:"receiver_id"`
	DisplayAction       string `json:"display_action"`
	DisplaySuffix       string `json:"display_suffix"`
	DisplayActionImgURL string `json:"display_action_img_url"`
}

type groupFileUploadData struct {
	GroupID  int64  `json:"group_id"`
	UserID   int64  `json:"user_id"`
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"`
}
