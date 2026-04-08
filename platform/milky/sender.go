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
type milkySender struct {
	client *milkyClient
}

func newSender(client *milkyClient) *milkySender {
	return &milkySender{client: client}
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
	if len(segs) == 0 {
		return platform.SendResult{}, errutil.ErrEmptyMessage
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

	return platform.SendResult{
		MessageID: strconv.FormatInt(out.MessageSeq, 10),
		Timestamp: time.Unix(out.Time, 0),
		Platform:  PlatformID,
		Raw:       &SendResult{MessageSeq: out.MessageSeq, SentAt: time.Unix(out.Time, 0)},
	}, nil
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
	chatID := encodeChatID(sceneGroup, func() int64 {
		n, _ := strconv.ParseInt(groupID, 10, 64)
		return n
	}())
	return s.Delete(ctx, chatID, messageID)
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
	_, gid, ok := decodeChatID(chatID)
	if !ok {
		return fmt.Errorf("milky: reaction: invalid chatID %q", chatID)
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
			// 文件附件——Milky 发送消息段中没有直接对应的类型；
			// 尽力而为：跳过（文件上传需单独调用 API）。
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
		s := buildForwardSegment(seg)
		return &s
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
	var apiErr *apiError
	if errors.As(err, &apiErr) {
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
