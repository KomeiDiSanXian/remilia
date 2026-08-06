package milky

import (
	stdctx "context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ────────────────────────────────────────────────────────────────────────────
// milkySender
// ────────────────────────────────────────────────────────────────────────────

// milkySender 实现了以下接口：
//   - platform.Sender
//   - platform.MessageDeleter
//   - platform.GroupManager
//   - platform.AutoModerator
//   - platform.InvitationHandler
//   - platform.ReactionSender
//   - platform.APIProvider（PlatformAPI 返回 *Adapter，暴露 Milky 全部 API 动作）
type milkySender struct {
	client  *milkyClient
	adapter *Adapter
}

func newSender(adapter *Adapter) *milkySender {
	return &milkySender{client: adapter.client, adapter: adapter}
}

// PlatformAPI 实现 platform.APIProvider，返回 Milky 适配器本身，
// 调用方可断言 *milky.Adapter 访问全部 Milky API 动作
// （get_login_info、send_group_message、群管理、文件上传等）。
func (s *milkySender) PlatformAPI() any { return s.adapter }

// 编译期接口实现检查。
var _ platform.APIProvider = (*milkySender)(nil)
var _ platform.GroupSettings = (*milkySender)(nil)
var _ platform.MessageHistoryProvider = (*milkySender)(nil)
var _ platform.AnnouncementManager = (*milkySender)(nil)

// ────────────────────────────────────────────────────────────────────────────
// platform.GroupSettings（委托 Adapter 的 Milky API）
// ────────────────────────────────────────────────────────────────────────────

// SetGroupName 实现 platform.GroupSettings，修改群名称。
func (s *milkySender) SetGroupName(ctx stdctx.Context, groupID, name string) error {
	gid, err := parseUin(groupID, "groupID")
	if err != nil {
		return err
	}
	return s.adapter.SetGroupName(ctx, gid, name)
}

// SetGroupCard 实现 platform.GroupSettings，设置群成员名片。
func (s *milkySender) SetGroupCard(ctx stdctx.Context, groupID, userID, card string) error {
	gid, err := parseUin(groupID, "groupID")
	if err != nil {
		return err
	}
	uid, err := parseUin(userID, "userID")
	if err != nil {
		return err
	}
	return s.adapter.SetGroupMemberCard(ctx, gid, uid, card)
}

// SetGroupSpecialTitle 实现 platform.GroupSettings，设置群成员专属头衔。
func (s *milkySender) SetGroupSpecialTitle(ctx stdctx.Context, groupID, userID, title string) error {
	gid, err := parseUin(groupID, "groupID")
	if err != nil {
		return err
	}
	uid, err := parseUin(userID, "userID")
	if err != nil {
		return err
	}
	return s.adapter.SetGroupMemberSpecialTitle(ctx, gid, uid, title)
}

// LeaveGroup 实现 platform.GroupSettings，退出群组。
func (s *milkySender) LeaveGroup(ctx stdctx.Context, groupID string, dismiss bool) error {
	gid, err := parseUin(groupID, "groupID")
	if err != nil {
		return err
	}
	// Milky 无解散群接口，dismiss 参数被忽略。
	_ = dismiss
	return s.adapter.QuitGroup(ctx, gid)
}

// ────────────────────────────────────────────────────────────────────────────
// platform.MessageHistoryProvider（委托 Adapter 的 Milky API）
// ────────────────────────────────────────────────────────────────────────────

// GetGroupHistory 实现 platform.MessageHistoryProvider，获取群历史消息。
func (s *milkySender) GetGroupHistory(ctx stdctx.Context, chatID string, limit int) ([]platform.Message, error) {
	peerID, err := parseUin(chatID, "groupID")
	if err != nil {
		return nil, err
	}
	msgs, _, err := s.adapter.GetHistoryMessages(ctx, sceneGroup, peerID, nil, limit)
	if err != nil {
		return nil, err
	}
	return toPlatformMessages(msgs, sceneGroup), nil
}

// GetFriendHistory 实现 platform.MessageHistoryProvider，获取好友历史消息。
func (s *milkySender) GetFriendHistory(ctx stdctx.Context, chatID string, limit int) ([]platform.Message, error) {
	peerID, err := parseUin(chatID, "userID")
	if err != nil {
		return nil, err
	}
	msgs, _, err := s.adapter.GetHistoryMessages(ctx, sceneFriend, peerID, nil, limit)
	if err != nil {
		return nil, err
	}
	return toPlatformMessages(msgs, sceneFriend), nil
}

// toPlatformMessages 将 Milky Message 列表转换为平台无关消息快照。
func toPlatformMessages(msgs []Message, scene string) []platform.Message {
	out := make([]platform.Message, 0, len(msgs))
	for _, m := range msgs {
		segs := incomingSegmentsToPlatform(toIncomingSegments(m.Segments))
		out = append(out, platform.Message{
			ID:          strconv.FormatInt(m.MessageSeq, 10),
			Platform:    PlatformID,
			Sender:      platform.UserInfo{ID: strconv.FormatInt(m.SenderID, 10)},
			Chat:        platform.ChatInfo{ID: encodeChatID(scene, m.PeerID), IsGroup: scene == sceneGroup},
			Segments:    segs,
			Content:     platform.SegmentsContent(segs),
			Attachments: platform.SegmentsAttachments(segs),
			Mentions:    platform.SegmentsMentions(segs, ""),
			ReplyToID:   platform.SegmentsReplyToID(segs),
			Timestamp:   time.Unix(m.Time, 0),
		})
	}
	return out
}

// ────────────────────────────────────────────────────────────────────────────
// platform.AnnouncementManager（委托 Adapter 的 Milky API）
// ────────────────────────────────────────────────────────────────────────────

// SendAnnouncement 实现 platform.AnnouncementManager，发布群公告。
func (s *milkySender) SendAnnouncement(ctx stdctx.Context, groupID, content, imageURL string) error {
	gid, err := parseUin(groupID, "groupID")
	if err != nil {
		return err
	}
	return s.adapter.SendGroupAnnouncement(ctx, gid, content, imageURL)
}

// GetAnnouncements 实现 platform.AnnouncementManager，获取群公告列表。
func (s *milkySender) GetAnnouncements(ctx stdctx.Context, groupID string) ([]platform.Announcement, error) {
	gid, err := parseUin(groupID, "groupID")
	if err != nil {
		return nil, err
	}
	anns, err := s.adapter.GetGroupAnnouncements(ctx, gid)
	if err != nil {
		return nil, err
	}
	out := make([]platform.Announcement, 0, len(anns))
	for _, a := range anns {
		ann := platform.Announcement{
			ID:          a.AnnouncementID,
			Content:     a.Content,
			PublisherID: strconv.FormatInt(a.UserID, 10),
			Timestamp:   a.Time,
		}
		if a.ImageURL != nil {
			ann.ImageURL = *a.ImageURL
		}
		out = append(out, ann)
	}
	return out, nil
}

// ────────────────────────────────────────────────────────────────────────────
// platform.Sender
// ────────────────────────────────────────────────────────────────────────────

// Send 将 OutboundMessage 转换为 Milky 消息段并进行发送。
//
// 路由规则：
//   - chat.IsGroup=true  → send_group_message（发送群消息）
//   - chat.IsGroup=false → send_private_message（发送私聊消息）
func (s *milkySender) Send(ctx stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
	if err := req.Validate(); err != nil {
		return platform.SendResult{}, err
	}

	scene, peerID, ok := decodeChatID(req.Target.ID)
	if !ok {
		return platform.SendResult{}, fmt.Errorf("%w: invalid milky chatID %q", errutil.ErrNoChatInfo, req.Target.ID)
	}
	// 如果 MessageExtra 中指定了 scene，则以其为准
	if extra := extractExtra(req.Message); extra.Scene != "" {
		scene = extra.Scene
	}
	// 兜底：若 chatID 中未编码 scene，则根据 IsGroup 推断
	if scene == "" {
		if req.Target.IsGroup {
			scene = sceneGroup
		} else {
			scene = sceneFriend
		}
	}

	segs := buildOutgoingSegments(req.Message)

	// 文件附件走独立的上传接口。
	//
	// Milky 的消息段里没有"文件"类型，文件必须通过 upload_private_file /
	// upload_group_file 单独发送。此前这类附件在 buildOutgoingSegments 的
	// default 分支被**静默丢弃**：Send 照常返回成功，用户只收到正文、
	// 收不到文件，调用方也无从察觉——而 Capabilities().FileUpload 声明为 true。
	fileAtts := fileAttachments(req.Message.Attachments)

	if len(segs) == 0 && len(fileAtts) == 0 {
		return platform.SendResult{}, errutil.ErrEmptyMessage
	}

	// 纯文件消息（无正文）：只做上传，不发空消息。
	if len(segs) == 0 {
		if err := s.uploadFiles(ctx, scene, peerID, fileAtts); err != nil {
			return platform.SendResult{}, wrapSendError(err, req.Target.ID, scene)
		}
		return platform.SendResult{Platform: PlatformID}, nil
	}

	var out sendMessageOutput
	var err error

	if scene == sceneGroup {
		err = s.client.call(ctx, "send_group_message", &sendGroupMessageInput{
			GroupID: peerID,
			Message: segs,
		}, &out)
	} else {
		err = s.client.call(ctx, "send_private_message", &sendPrivateMessageInput{
			UserID:  peerID,
			Message: segs,
		}, &out)
	}
	if err != nil {
		return platform.SendResult{}, wrapSendError(err, req.Target.ID, scene)
	}

	// 正文发送成功后再上传文件；上传失败必须上报，不能像此前那样静默吞掉。
	if len(fileAtts) > 0 {
		if upErr := s.uploadFiles(ctx, scene, peerID, fileAtts); upErr != nil {
			return platform.SendResult{}, wrapSendError(upErr, req.Target.ID, scene)
		}
	}

	return platform.SendResult{
		MessageID: strconv.FormatInt(out.MessageSeq, 10),
		Timestamp: time.Unix(out.Time, 0),
		Platform:  PlatformID,
		Raw:       &SendResult{MessageSeq: out.MessageSeq, SentAt: time.Unix(out.Time, 0)},
	}, nil
}

// fileAttachments 筛出需要走上传接口的文件类附件。
func fileAttachments(atts []platform.Attachment) []platform.Attachment {
	var out []platform.Attachment
	for _, att := range atts {
		if att.Kind == platform.AttachmentKindFile {
			out = append(out, att)
		}
	}
	return out
}

// uploadFiles 依次上传文件附件到指定会话。
func (s *milkySender) uploadFiles(ctx stdctx.Context, scene string, peerID int64, atts []platform.Attachment) error {
	for i, att := range atts {
		uri := att.URL
		if uri == "" && len(att.Data) > 0 {
			uri = "base64://" + base64.StdEncoding.EncodeToString(att.Data)
		}
		if uri == "" {
			return fmt.Errorf("milky: 附件 #%d 既无 URL 也无 Data，无法上传", i)
		}
		name := att.Name
		if name == "" {
			name = "file"
		}

		var out uploadFileOutput
		var err error
		if scene == sceneGroup {
			err = s.client.call(ctx, "upload_group_file", &uploadGroupFileInput{
				GroupID:  peerID,
				FileURI:  uri,
				FileName: name,
			}, &out)
		} else {
			err = s.client.call(ctx, "upload_private_file", &uploadPrivateFileInput{
				UserID:   peerID,
				FileURI:  uri,
				FileName: name,
			}, &out)
		}
		if err != nil {
			return fmt.Errorf("milky: 上传附件 %q 失败: %w", name, err)
		}
	}
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// platform.MessageDeleter
// ────────────────────────────────────────────────────────────────────────────

// Delete 通过 message_seq 撤回一条消息。
//
// chatID 须为 "scene:peer_id" 格式（由 milkyEvent.Chat().ID 设置）。
// messageID 为十进制 message_seq 字符串（由 SendResult.MessageID 返回）。
func (s *milkySender) Delete(ctx stdctx.Context, chatID, messageID string) error {
	scene, peerID, ok := decodeChatID(chatID)
	if !ok {
		return fmt.Errorf("milky: Delete: invalid chatID %q", chatID)
	}
	seq, err := strconv.ParseInt(messageID, 10, 64)
	if err != nil {
		return fmt.Errorf("milky: Delete: invalid messageID %q: %w", messageID, err)
	}

	if scene == sceneGroup {
		return s.client.call(ctx, "recall_group_message", &recallGroupMessageInput{
			GroupID:    peerID,
			MessageSeq: seq,
		}, nil)
	}
	return s.client.call(ctx, "recall_private_message", &recallPrivateMessageInput{
		UserID:     peerID,
		MessageSeq: seq,
	}, nil)
}

// ────────────────────────────────────────────────────────────────────────────
// platform.GroupManager
// ────────────────────────────────────────────────────────────────────────────

// KickMember 将用户从群中移除。
// permanent=true 同时拒绝该用户今后的入群申请。
func (s *milkySender) KickMember(ctx stdctx.Context, groupID, userID string, permanent bool) error {
	gid, err := parseUin(groupID, "groupID")
	if err != nil {
		return err
	}
	uid, err := parseUin(userID, "userID")
	if err != nil {
		return err
	}
	return s.client.call(ctx, "kick_group_member", &kickGroupMemberInput{
		GroupID:          gid,
		UserID:           uid,
		RejectAddRequest: permanent,
	}, nil)
}

// BanMember 禁言或解禁群成员。
// duration=0 时解禁；大于 0 时设置禁言秒数。
func (s *milkySender) BanMember(ctx stdctx.Context, groupID, userID string, duration time.Duration) error {
	gid, err := parseUin(groupID, "groupID")
	if err != nil {
		return err
	}
	uid, err := parseUin(userID, "userID")
	if err != nil {
		return err
	}
	secs := int64(duration.Seconds())
	if duration < 0 {
		secs = 0
	}
	return s.client.call(ctx, "set_group_member_mute", &setGroupMemberMuteInput{
		GroupID:  gid,
		UserID:   uid,
		Duration: secs,
	}, nil)
}

// SetAdmin 授予或撤销群成员的管理员权限。
func (s *milkySender) SetAdmin(ctx stdctx.Context, groupID, userID string, isAdmin bool) error {
	gid, err := parseUin(groupID, "groupID")
	if err != nil {
		return err
	}
	uid, err := parseUin(userID, "userID")
	if err != nil {
		return err
	}
	return s.client.call(ctx, "set_group_member_admin", &setGroupMemberAdminInput{
		GroupID: gid,
		UserID:  uid,
		IsSet:   isAdmin,
	}, nil)
}

// ────────────────────────────────────────────────────────────────────────────
// platform.AutoModerator
// ────────────────────────────────────────────────────────────────────────────

// DeleteMemberMessage 撤回群成员的消息。功能等同于群场景下的 Delete，
// 作为自动审核的便捷方法提供。
func (s *milkySender) DeleteMemberMessage(ctx stdctx.Context, groupID, messageID string) error {
	// 必须走 parseUin：调用方（core/context 的 TryDeleteMemberMessage）传入的
	// 是 event.Chat().ID，形态为 "group:123456789" 而非裸 QQ 号。
	// 此前直接 strconv.ParseInt 且丢弃 error，解析失败得到 0，于是向
	// group_id=0 发起撤回——自动审核静默失效，而调用方通常忽略返回值，
	// 完全无从察觉。本文件其余 GroupManager 方法都已使用 parseUin。
	gid, err := parseUin(groupID, "groupID")
	if err != nil {
		return err
	}
	return s.Delete(ctx, encodeChatID(sceneGroup, gid), messageID)
}

// MuteAll 开启或关闭全群禁言。
func (s *milkySender) MuteAll(ctx stdctx.Context, groupID string, mute bool) error {
	gid, err := parseUin(groupID, "groupID")
	if err != nil {
		return err
	}
	return s.client.call(ctx, "set_group_whole_mute", &setGroupWholeMuteInput{
		GroupID: gid,
		IsMute:  mute,
	}, nil)
}

// ────────────────────────────────────────────────────────────────────────────
// platform.InvitationHandler
// ────────────────────────────────────────────────────────────────────────────

// AcceptGroupInvite 接受邀请机器人加入某群的邀请。
//
// inviteID 须为 "group_id:invitation_seq" 格式，
// 由事件解析器存储在 ChatInfo.Tokens[TokenInvitationSeq] 中。
func (s *milkySender) AcceptGroupInvite(ctx stdctx.Context, inviteID string) error {
	gid, seq, err := parseInviteID(inviteID)
	if err != nil {
		return err
	}
	return s.client.call(ctx, "accept_group_invitation", &acceptGroupInvitationInput{
		GroupID:       gid,
		InvitationSeq: seq,
	}, nil)
}

// RejectGroupInvite 拒绝邀请机器人加入某群的邀请。
//
// 注意：Milky 协议的 reject_group_invitation API 不支持拒绝理由，reason 参数将被忽略。
func (s *milkySender) RejectGroupInvite(ctx stdctx.Context, inviteID, reason string) error {
	gid, seq, err := parseInviteID(inviteID)
	if err != nil {
		return err
	}
	_ = reason // Milky reject_group_invitation 协议不支持拒绝理由字段
	return s.client.call(ctx, "reject_group_invitation", &rejectGroupInvitationInput{
		GroupID:       gid,
		InvitationSeq: seq,
	}, nil)
}

// AcceptFriendRequest 同意好友请求。
//
// requestID 须为发起者的 UID 字符串（由事件解析器存储）。
func (s *milkySender) AcceptFriendRequest(ctx stdctx.Context, requestID string) error {
	return s.client.call(ctx, "accept_friend_request", &acceptFriendRequestInput{
		InitiatorUID: requestID,
		IsFiltered:   false,
	}, nil)
}

// RejectFriendRequest 拒绝好友请求。
func (s *milkySender) RejectFriendRequest(ctx stdctx.Context, requestID, reason string) error {
	return s.client.call(ctx, "reject_friend_request", &rejectFriendRequestInput{
		InitiatorUID: requestID,
		IsFiltered:   false,
		Reason:       reason,
	}, nil)
}

// ────────────────────────────────────────────────────────────────────────────
// platform.ReactionSender
// ────────────────────────────────────────────────────────────────────────────

// AddReaction 为群消息添加表情回应。
//
// chatID 须为群 chatID；messageID 为十进制 message_seq。
// Milky 仅支持群消息的表情回应。
func (s *milkySender) AddReaction(ctx stdctx.Context, chatID, messageID string, emoji platform.Emoji) error {
	return s.sendReaction(ctx, chatID, messageID, emoji, true)
}

// RemoveReaction 取消群消息的表情回应。
func (s *milkySender) RemoveReaction(ctx stdctx.Context, chatID, messageID string, emoji platform.Emoji) error {
	return s.sendReaction(ctx, chatID, messageID, emoji, false)
}

func (s *milkySender) sendReaction(ctx stdctx.Context, chatID, messageID string, emoji platform.Emoji, add bool) error {
	scene, gid, ok := decodeChatID(chatID)
	if !ok {
		return fmt.Errorf("milky: reaction: invalid chatID %q", chatID)
	}
	// 必须校验 scene：Milky 仅支持群消息表情回应。此前丢弃 scene 后，
	// 私聊场景的 "friend:12345" 会把好友 QQ 号当作 group_id 用，
	// 若机器人恰好在该号码对应的群里，就会把表情回应写到毫不相干的群消息上。
	if scene != sceneGroup {
		return fmt.Errorf("milky: reaction: 仅支持群消息，chatID=%q", chatID)
	}
	seq, err := strconv.ParseInt(messageID, 10, 64)
	if err != nil {
		return fmt.Errorf("milky: reaction: invalid messageID %q", messageID)
	}

	reaction := emoji.ID
	if reaction == "" {
		reaction = emoji.Value
	}
	reactionType := "face"
	if emoji.Kind == platform.EmojiKindUnicode {
		reactionType = "emoji"
		reaction = emoji.Value
	}

	return s.client.call(ctx, "send_group_message_reaction", &sendGroupMessageReactionInput{
		GroupID:      gid,
		MessageSeq:   seq,
		Reaction:     reaction,
		ReactionType: reactionType,
		IsAdd:        add,
	}, nil)
}

// ────────────────────────────────────────────────────────────────────────────
// 消息段构建器
// ────────────────────────────────────────────────────────────────────────────

// buildOutgoingSegments 将 platform.OutboundMessage 转换为 Milky 发送消息段列表。
func buildOutgoingSegments(msg platform.OutboundMessage) []outgoingSegment {
	// 出站段优先路径：按段保序，保留文本夹 at 的交错位置
	if len(msg.Segments) > 0 {
		return buildOutgoingFromSegments(msg.Segments)
	}

	var segs []outgoingSegment

	// 引用回复（必须排在最前面）
	if msg.ReplyToID != "" {
		seq, err := strconv.ParseInt(msg.ReplyToID, 10, 64)
		if err == nil {
			segs = append(segs, outgoingSegment{
				Type: "reply",
				Data: outgoingSegData{MessageSeq: seq},
			})
		}
	}

	// @ 提及
	for _, uid := range msg.Mentions {
		uin, err := strconv.ParseInt(uid, 10, 64)
		if err == nil {
			segs = append(segs, outgoingSegment{
				Type: "mention",
				Data: outgoingSegData{UserID: uin},
			})
		}
	}

	// 文本内容
	text := msg.Text
	if text == "" && msg.Markdown != "" {
		// Milky 没有原生 Markdown 类型，回退为纯文本。
		text = msg.Markdown
	}
	if text != "" {
		segs = append(segs, outgoingSegment{
			Type: "text",
			Data: outgoingSegData{Text: text},
		})
	}

	// 附件
	for _, att := range msg.Attachments {
		uri := att.URL
		if uri == "" && len(att.Data) > 0 {
			uri = "base64://" + base64.StdEncoding.EncodeToString(att.Data)
		}
		if uri == "" {
			continue
		}
		switch att.Kind {
		case platform.AttachmentKindImage:
			segs = append(segs, outgoingSegment{
				Type: "image",
				Data: outgoingSegData{URI: uri, SubType: "normal"},
			})
		case platform.AttachmentKindAudio:
			segs = append(segs, outgoingSegment{
				Type: "record",
				Data: outgoingSegData{URI: uri},
			})
		case platform.AttachmentKindVideo:
			segs = append(segs, outgoingSegment{
				Type: "video",
				Data: outgoingSegData{URI: uri},
			})
		default:
			// 文件类附件在这里刻意跳过：Milky 的消息段没有"文件"类型，
			// 它们由 milkySender.Send 通过 upload_*_file 接口单独上传。
		}
	}

	// Milky 特有消息段（来自 MessageExtra.Segments，追加在标准内容之后）
	extra := extractExtra(msg)
	for _, s := range extra.Segments {
		if seg := convertOutgoingSegment(s); seg != nil {
			segs = append(segs, *seg)
		}
	}

	return segs
}

// buildOutgoingFromSegments 将统一出站段转换为 Milky 发送消息段（保序）。
//
// 与 buildOutgoingSegments 的便捷字段路径互补；文件段与便捷路径一致刻意跳过
// （由 milkySender.Send 通过 upload_*_file 单独上传）；button/unknown 无发送
// 能力 → 跳过；forward 无通用载荷 → 跳过（同平台还原走 MessageExtra 路径）。
func buildOutgoingFromSegments(segs []platform.Segment) []outgoingSegment {
	var out []outgoingSegment
	for _, s := range segs {
		switch s.Type {
		case platform.SegmentText:
			out = append(out, outgoingSegment{Type: "text", Data: outgoingSegData{Text: s.Text}})
		case platform.SegmentAt:
			uin, err := strconv.ParseInt(s.UserID, 10, 64)
			if err != nil {
				continue
			}
			out = append(out, outgoingSegment{Type: "mention", Data: outgoingSegData{UserID: uin}})
		case platform.SegmentMentionAll:
			out = append(out, outgoingSegment{Type: "mention_all", Data: outgoingSegData{}})
		case platform.SegmentFace:
			out = append(out, outgoingSegment{Type: "face", Data: outgoingSegData{FaceID: s.FaceID}})
		case platform.SegmentReply:
			seq, err := strconv.ParseInt(s.ReplyToID, 10, 64)
			if err != nil {
				continue
			}
			out = append(out, outgoingSegment{Type: "reply", Data: outgoingSegData{MessageSeq: seq}})
		case platform.SegmentImage:
			out = append(out, outgoingSegment{Type: "image", Data: outgoingSegData{URI: mediaURI(s.Attachment), SubType: "normal"}})
		case platform.SegmentAudio:
			out = append(out, outgoingSegment{Type: "record", Data: outgoingSegData{URI: mediaURI(s.Attachment)}})
		case platform.SegmentVideo:
			out = append(out, outgoingSegment{Type: "video", Data: outgoingSegData{URI: mediaURI(s.Attachment)}})
		default:
			// file/forward/button/unknown：跳过（见函数注释）
		}
	}
	return out
}

// mediaURI 提取统一附件段的发送 URI（URL 优先，二进制 Data 转 base64）。
func mediaURI(att platform.Attachment) string {
	if att.URL != "" {
		return att.URL
	}
	if len(att.Data) > 0 {
		return "base64://" + base64.StdEncoding.EncodeToString(att.Data)
	}
	return ""
}

// convertOutgoingSegment 将 Milky 特有的 OutgoingSegment 接口值转换为内部 outgoingSegment。
// 若类型未知则返回 nil。
func convertOutgoingSegment(s OutgoingSegment) *outgoingSegment {
	switch seg := s.(type) {
	case *TextSegment:
		return &outgoingSegment{
			Type: "text",
			Data: outgoingSegData{Text: seg.Text},
		}
	case *MentionSegment:
		return &outgoingSegment{
			Type: "mention",
			Data: outgoingSegData{UserID: seg.UserID},
		}
	case *ReplySegment:
		return &outgoingSegment{
			Type: "reply",
			Data: outgoingSegData{MessageSeq: seg.MessageSeq},
		}
	case *ImageSegment:
		subType := seg.SubType
		if subType == "" {
			subType = "normal"
		}
		return &outgoingSegment{
			Type: "image",
			Data: outgoingSegData{URI: seg.URI, SubType: subType, Summary: seg.Summary},
		}
	case *RecordSegment:
		return &outgoingSegment{
			Type: "record",
			Data: outgoingSegData{URI: seg.URI, ThumbURI: seg.ThumbURI},
		}
	case *VideoSegment:
		return &outgoingSegment{
			Type: "video",
			Data: outgoingSegData{URI: seg.URI, ThumbURI: seg.ThumbURI},
		}
	case *FaceSegment:
		return &outgoingSegment{
			Type: "face",
			Data: outgoingSegData{FaceID: seg.FaceID, IsLarge: seg.IsLarge},
		}
	case *MentionAllSegment:
		return &outgoingSegment{Type: "mention_all", Data: outgoingSegData{}}
	case *LightAppSegment:
		return &outgoingSegment{
			Type: "light_app",
			Data: outgoingSegData{JSONPayload: seg.JSONPayload},
		}
	case *ForwardSegment:
		return new(buildForwardSegment(seg))
	}
	return nil
}

// buildForwardSegment 将 ForwardSegment 转换为内部 outgoingSegment（type="forward"）。
func buildForwardSegment(seg *ForwardSegment) outgoingSegment {
	msgs := make([]outgoingForwardedMessage, len(seg.Messages))
	for i, entry := range seg.Messages {
		var fwdSegs []outgoingSegment
		if len(entry.Segments) > 0 {
			// 使用复杂消息段
			for _, s := range entry.Segments {
				if converted := convertOutgoingSegment(s); converted != nil {
					fwdSegs = append(fwdSegs, *converted)
				}
			}
		} else {
			// 回退到纯文本
			fwdSegs = []outgoingSegment{{
				Type: "text",
				Data: outgoingSegData{Text: entry.Text},
			}}
		}
		msgs[i] = outgoingForwardedMessage{
			UserID:     entry.UserID,
			SenderName: entry.SenderName,
			Segments:   fwdSegs,
		}
	}
	return outgoingSegment{
		Type: "forward",
		Data: outgoingSegData{
			ForwardMessages: msgs,
			Title:           seg.Title,
			Preview:         seg.Preview,
			Summary:         seg.Summary,
			Prompt:          seg.Prompt,
		},
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 辅助函数
// ────────────────────────────────────────────────────────────────────────────

// parseUin 将字符串 UIN（QQ 号）解析为 int64。
func parseUin(s, field string) (int64, error) {
	// chatID 可能为 "scene:peer_id" 格式——提取 peer_id 部分
	if idx := strings.Index(s, ":"); idx >= 0 {
		s = s[idx+1:]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("milky: invalid %s %q: %w", field, s, err)
	}
	return n, nil
}

// parseInviteID 解析 "group_id:invitation_seq" 格式的邀请 ID。
func parseInviteID(id string) (groupID, invitationSeq int64, err error) {
	parts := strings.SplitN(id, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("milky: invalid invite ID %q (expected group_id:invitation_seq)", id)
	}
	gid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("milky: invalid invite ID group part %q: %w", parts[0], err)
	}
	seq, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("milky: invalid invite ID seq part %q: %w", parts[1], err)
	}
	return gid, seq, nil
}

// wrapSendError 将 Milky API 错误包装为 platform.SendError。
func wrapSendError(err error, chatID, scene string) error {
	if err == nil {
		return nil
	}
	if apiErr, ok := errors.AsType[*apiError](err); ok {
		code := platform.SendErrPlatform
		switch apiErr.Retcode {
		case -403:
			code = platform.SendErrPermDenied
		case -400:
			code = platform.SendErrInvalidTarget
		}
		return platform.NewSendError(code, PlatformID, chatID, apiErr.Message, 0, apiErr)
	}
	return platform.NewSendError(platform.SendErrNetworkError, PlatformID, chatID, err.Error(), 0, err)
}
