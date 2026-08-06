package milky

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// ────────────────────────────────────────────────────────────────────────────
// Chat ID 辅助函数
// ────────────────────────────────────────────────────────────────────────────
//
// Milky 通过 (message_scene, peer_id) 路由消息。由于 platform.Event.Chat()
// 返回的 ChatInfo.ID 是普通字符串，而部分操作（如 Delete）仅能获取到 chatID
// 而没有额外上下文，因此将 scene 编码进 ID 中：
//
//   - "group:123456789"   — 群消息   (IsGroup=true)
//   - "friend:987654321"  — 好友消息  (IsGroup=false)
//   - "temp:555555555"    — 临时会话  (IsGroup=false)

const (
	sceneGroup  = "group"
	sceneFriend = "friend"
	sceneTemp   = "temp"
)

func encodeChatID(scene string, peerID int64) string {
	return scene + ":" + strconv.FormatInt(peerID, 10)
}

// decodeChatID 解析由 encodeChatID 生成的 chatID。
// 返回 (scene, peerID, ok)，若格式无法识别则 ok=false。
func decodeChatID(chatID string) (scene string, peerID int64, ok bool) {
	before, after, ok0 := strings.Cut(chatID, ":")
	if !ok0 {
		// 旧格式 / 纯数字——视为未知 scene
		n, err := strconv.ParseInt(chatID, 10, 64)
		if err != nil {
			return "", 0, false
		}
		return "", n, true
	}
	n, err := strconv.ParseInt(after, 10, 64)
	if err != nil {
		return "", 0, false
	}
	return before, n, true
}

// ────────────────────────────────────────────────────────────────────────────
// milkyEvent
// ────────────────────────────────────────────────────────────────────────────

// milkyEvent 将解析后的 Milky 事件封装为 platform.Event。
//
// 实现了以下接口：
//   - platform.Event（核心接口）
//   - platform.RawEvent  （RawType / RawPayload）
//   - platform.ReplyEvent（ReplyToID）
//   - platform.MentionsEvent（Mentions）
type milkyEvent struct {
	kind        platform.EventKind
	id          string // message_seq 的十进制字符串，或非消息事件的复合 ID
	senderInfo  platform.UserInfo
	chat        platform.ChatInfo
	content     string
	timestamp   time.Time
	attachments []platform.Attachment
	rawType     string // 线上传输的 event_type 字符串
	rawPayload  any    // 解析后的载荷结构体

	// 可选扩展字段
	replyToID string
	mentions  []platform.UserInfo
}

// ── platform.Event ──────────────────────────────────────────────────────────

func (e *milkyEvent) Platform() string                   { return PlatformID }
func (e *milkyEvent) Kind() platform.EventKind           { return e.kind }
func (e *milkyEvent) ID() string                         { return e.id }
func (e *milkyEvent) Sender() platform.UserInfo          { return e.senderInfo }
func (e *milkyEvent) Chat() platform.ChatInfo            { return e.chat }
func (e *milkyEvent) Content() string                    { return e.content }
func (e *milkyEvent) Timestamp() time.Time               { return e.timestamp }
func (e *milkyEvent) Attachments() []platform.Attachment { return e.attachments }

// ── platform.RawEvent ───────────────────────────────────────────────────────

func (e *milkyEvent) RawType() string { return e.rawType }
func (e *milkyEvent) RawPayload() any { return e.rawPayload }

// ── platform.ReplyEvent ─────────────────────────────────────────────────────

func (e *milkyEvent) ReplyToID() string { return e.replyToID }

// ── platform.MentionsEvent ──────────────────────────────────────────────────

func (e *milkyEvent) Mentions() []platform.UserInfo { return e.mentions }

// ────────────────────────────────────────────────────────────────────────────
// 事件解析
// ────────────────────────────────────────────────────────────────────────────

// parseRawEvent 将 Milky WebSocket 原始消息转换为 platform.Event。
func parseRawEvent(data []byte) (platform.Event, error) {
	var env rawEvent
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("milky: parse event envelope: %w", err)
	}

	ts := time.Unix(env.Time, 0)
	e := &milkyEvent{
		rawType:   env.EventType,
		timestamp: ts,
	}

	switch env.EventType {
	case "message_receive":
		return parseMessageEvent(e, env.Data)
	case "message_recall":
		return parseMessageRecallEvent(e, env.Data)
	case "group_member_increase":
		return parseGroupMemberIncreaseEvent(e, env.Data)
	case "group_member_decrease":
		return parseGroupMemberDecreaseEvent(e, env.Data)
	case "group_admin_change":
		return parseGroupAdminChangeEvent(e, env.Data)
	case "group_mute":
		return parseGroupMuteEvent(e, env.Data)
	case "group_whole_mute":
		return parseGroupWholeMuteEvent(e, env.Data)
	case "group_message_reaction":
		return parseGroupMessageReactionEvent(e, env.Data)
	case "friend_request":
		return parseFriendRequestEvent(e, env.Data)
	case "group_join_request":
		return parseGroupJoinRequestEvent(e, env.Data)
	case "group_invitation":
		return parseGroupInvitationEvent(e, env.Data)
	case "bot_offline":
		return parseBotOfflineEvent(e, env.Data)
	case "peer_pin_change":
		return parsePeerPinChangeEvent(e, env.Data)
	case "group_invited_join_request":
		return parseGroupInvitedJoinRequestEvent(e, env.Data)
	case "friend_nudge":
		return parseFriendNudgeEvent(e, env.Data)
	case "friend_file_upload":
		return parseFriendFileUploadEvent(e, env.Data)
	case "group_essence_message_change":
		return parseGroupEssenceMessageChangeEvent(e, env.Data)
	case "group_name_change":
		return parseGroupNameChangeEvent(e, env.Data)
	case "group_nudge":
		return parseGroupNudgeEvent(e, env.Data)
	case "group_file_upload":
		return parseGroupFileUploadEvent(e, env.Data)
	default:
		// 未知事件类型——返回携带原始载荷的最小化 NOTICE 事件
		e.kind = platform.EventKindNotice
		e.id = fmt.Sprintf("%s:%d", env.EventType, env.Time)
		e.rawPayload = env.Data
		return e, nil
	}
}

// ── message_receive ──────────────────────────────────────────────────────────

func parseMessageEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var msg incomingMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("milky: parse message_receive: %w", err)
	}

	e.id = strconv.FormatInt(msg.MessageSeq, 10)
	e.timestamp = time.Unix(msg.Time, 0)
	e.rawPayload = &msg

	// 会话路由
	chatID := encodeChatID(msg.MessageScene, msg.PeerID)
	isGroup := msg.MessageScene == sceneGroup

	chat := platform.ChatInfo{
		ID:      chatID,
		IsGroup: isGroup,
	}
	if isGroup && msg.Group != nil {
		chat.Name = msg.Group.GroupName
	}
	e.chat = chat

	// 发送者信息
	e.senderInfo = senderFromMessage(&msg)

	// 事件类型
	switch msg.MessageScene {
	case sceneGroup:
		e.kind = platform.EventKindGroupMessage
	case sceneFriend:
		e.kind = platform.EventKindPrivateMessage
	case sceneTemp:
		e.kind = platform.EventKindPrivateMessage
	}

	// 解析消息段
	var (
		textParts []string
		mentions  []platform.UserInfo
		atts      []platform.Attachment
		replyID   string
	)
	for _, seg := range msg.Segments {
		switch seg.Type {
		case "text":
			textParts = append(textParts, seg.Data.Text)
		case "mention":
			uid := strconv.FormatInt(seg.Data.UserID, 10)
			mentions = append(mentions, platform.UserInfo{
				ID:          uid,
				DisplayName: seg.Data.Name,
			})
		case "mention_all":
			// @全体成员——无具体用户信息
		case "reply":
			replyID = strconv.FormatInt(seg.Data.MessageSeq, 10)
		case "face":
			atts = append(atts, platform.Attachment{
				Extra: map[string]any{ExtraKeyFace: &FaceSegmentMeta{
					FaceID:  seg.Data.FaceID,
					IsLarge: seg.Data.IsLarge,
				}},
			})
		case "image":
			att := platform.Attachment{
				URL:      seg.Data.TempURL,
				MimeType: "image/jpeg",
				Width:    seg.Data.Width,
				Height:   seg.Data.Height,
			}
			subType := "normal"
			if seg.Data.SubType == "sticker" {
				subType = "sticker"
			}
			att.Extra = map[string]any{ExtraKeyImage: &ImageSegmentMeta{SubType: subType, ResourceID: seg.Data.ResourceID}}
			atts = append(atts, att)
		case "record":
			atts = append(atts, platform.Attachment{
				URL:      seg.Data.TempURL,
				MimeType: "audio/mpeg",
				Extra:    map[string]any{ExtraKeyRecord: &RecordSegmentMeta{ResourceID: seg.Data.ResourceID, Duration: seg.Data.Duration}},
			})
		case "video":
			atts = append(atts, platform.Attachment{
				URL:      seg.Data.TempURL,
				MimeType: "video/mp4",
				Width:    seg.Data.Width,
				Height:   seg.Data.Height,
				Extra:    map[string]any{ExtraKeyVideo: &VideoSegmentMeta{ResourceID: seg.Data.ResourceID, Duration: seg.Data.Duration}},
			})
		case "file":
			atts = append(atts, platform.Attachment{
				Name: seg.Data.FileName,
				Size: int(seg.Data.FileSize),
				Extra: map[string]any{ExtraKeyFile: &FileSegmentMeta{
					FileID:   seg.Data.FileID,
					FileName: seg.Data.FileName,
					FileSize: seg.Data.FileSize,
					FileHash: seg.Data.FileHash,
				}},
			})
		case "market_face":
			atts = append(atts, platform.Attachment{
				URL: seg.Data.EmojiURL,
				Extra: map[string]any{ExtraKeyMarketFace: &MarketFaceSegmentMeta{
					EmojiPackageID: seg.Data.EmojiPackageID,
					EmojiID:        seg.Data.EmojiID,
					Key:            seg.Data.EmojiKey,
					Summary:        seg.Data.Summary,
					URL:            seg.Data.EmojiURL,
				}},
			})
		case "light_app":
			atts = append(atts, platform.Attachment{
				Extra: map[string]any{ExtraKeyLightApp: &LightAppSegmentMeta{
					AppName:     seg.Data.AppName,
					JSONPayload: seg.Data.JSONPayload,
				}},
			})
		case "xml":
			atts = append(atts, platform.Attachment{
				Extra: map[string]any{ExtraKeyXML: &XMLSegmentMeta{
					ServiceID:  seg.Data.ServiceID,
					XMLPayload: seg.Data.XMLPayload,
				}},
			})
		}
	}

	e.content = strings.Join(textParts, "")
	e.mentions = mentions
	e.attachments = atts
	e.replyToID = replyID
	return e, nil
}

// ── message_recall ───────────────────────────────────────────────────────────

func parseMessageRecallEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d messageRecallData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse message_recall: %w", err)
	}
	e.kind = platform.EventKindMessageDelete
	e.id = strconv.FormatInt(d.MessageSeq, 10)
	e.rawPayload = &d
	e.chat = platform.ChatInfo{
		ID:      encodeChatID(d.MessageScene, d.PeerID),
		IsGroup: d.MessageScene == sceneGroup,
	}
	e.senderInfo = platform.UserInfo{ID: strconv.FormatInt(d.OperatorID, 10)}
	return e, nil
}

// ── 群成员增加/减少 ─────────────────────────────────────────────────────────

func parseGroupMemberIncreaseEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d groupMemberIncreaseData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse group_member_increase: %w", err)
	}
	e.kind = platform.EventKindMemberJoin
	e.id = fmt.Sprintf("member_join:%d:%d:%d", d.GroupID, d.UserID, e.timestamp.Unix())
	e.rawPayload = &d
	e.chat = platform.ChatInfo{
		ID:      encodeChatID(sceneGroup, d.GroupID),
		IsGroup: true,
	}
	e.senderInfo = platform.UserInfo{ID: strconv.FormatInt(d.UserID, 10)}
	return e, nil
}

func parseGroupMemberDecreaseEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d groupMemberDecreaseData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse group_member_decrease: %w", err)
	}
	e.kind = platform.EventKindMemberLeave
	e.id = fmt.Sprintf("member_leave:%d:%d:%d", d.GroupID, d.UserID, e.timestamp.Unix())
	e.rawPayload = &d
	e.chat = platform.ChatInfo{
		ID:      encodeChatID(sceneGroup, d.GroupID),
		IsGroup: true,
	}
	e.senderInfo = platform.UserInfo{ID: strconv.FormatInt(d.UserID, 10)}
	return e, nil
}

// ── 群管理员变更 ──────────────────────────────────────────────────────────────

func parseGroupAdminChangeEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d groupAdminChangeData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse group_admin_change: %w", err)
	}
	e.kind = platform.EventKindMemberUpdate
	e.id = fmt.Sprintf("admin_change:%d:%d:%d", d.GroupID, d.UserID, e.timestamp.Unix())
	e.rawPayload = &d
	e.chat = platform.ChatInfo{
		ID:      encodeChatID(sceneGroup, d.GroupID),
		IsGroup: true,
	}
	e.senderInfo = platform.UserInfo{ID: strconv.FormatInt(d.UserID, 10)}
	return e, nil
}

// ── 群禁言 ────────────────────────────────────────────────────────────────────

func parseGroupMuteEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d groupMuteData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse group_mute: %w", err)
	}
	e.kind = platform.EventKindNotice
	e.id = fmt.Sprintf("group_mute:%d:%d:%d", d.GroupID, d.UserID, e.timestamp.Unix())
	e.rawPayload = &d
	e.chat = platform.ChatInfo{
		ID:      encodeChatID(sceneGroup, d.GroupID),
		IsGroup: true,
	}
	e.senderInfo = platform.UserInfo{ID: strconv.FormatInt(d.UserID, 10)}
	return e, nil
}

func parseGroupWholeMuteEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d groupWholeMuteData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse group_whole_mute: %w", err)
	}
	e.kind = platform.EventKindNotice
	e.id = fmt.Sprintf("group_whole_mute:%d:%d", d.GroupID, e.timestamp.Unix())
	e.rawPayload = &d
	e.chat = platform.ChatInfo{
		ID:      encodeChatID(sceneGroup, d.GroupID),
		IsGroup: true,
	}
	e.senderInfo = platform.UserInfo{ID: strconv.FormatInt(d.OperatorID, 10)}
	return e, nil
}

// ── 群表情回应 ────────────────────────────────────────────────────────────────

func parseGroupMessageReactionEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d groupMessageReactionData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse group_message_reaction: %w", err)
	}
	e.kind = platform.EventKindReaction
	e.id = fmt.Sprintf("reaction:%d:%d:%d:%s", d.GroupID, d.UserID, d.MessageSeq, d.FaceID)
	e.rawPayload = &d
	e.chat = platform.ChatInfo{
		ID:      encodeChatID(sceneGroup, d.GroupID),
		IsGroup: true,
	}
	e.senderInfo = platform.UserInfo{ID: strconv.FormatInt(d.UserID, 10)}
	return e, nil
}

// ── 好友请求 ──────────────────────────────────────────────────────────────────

func parseFriendRequestEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d friendRequestData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse friend_request: %w", err)
	}
	e.kind = platform.EventKindRequest
	e.id = fmt.Sprintf("friend_req:%d:%d", d.InitiatorID, e.timestamp.Unix())
	e.rawPayload = &d
	e.senderInfo = platform.UserInfo{ID: strconv.FormatInt(d.InitiatorID, 10)}
	e.chat = platform.ChatInfo{ID: encodeChatID(sceneFriend, d.InitiatorID)}
	return e, nil
}

// ── 入群申请 ──────────────────────────────────────────────────────────────────

func parseGroupJoinRequestEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d groupJoinRequestData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse group_join_request: %w", err)
	}
	e.kind = platform.EventKindRequest
	e.id = fmt.Sprintf("group_req:%d:%d:%d", d.GroupID, d.InitiatorID, d.NotificationSeq)
	e.rawPayload = &d
	e.senderInfo = platform.UserInfo{ID: strconv.FormatInt(d.InitiatorID, 10)}
	e.chat = platform.ChatInfo{
		ID:      encodeChatID(sceneGroup, d.GroupID),
		IsGroup: true,
	}
	return e, nil
}

// ── 群邀请 ───────────────────────────────────────────────────────────────────

func parseGroupInvitationEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d groupInvitationData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse group_invitation: %w", err)
	}
	e.kind = platform.EventKindRequest
	e.id = fmt.Sprintf("group_inv:%d:%d", d.GroupID, d.InvitationSeq)
	e.rawPayload = &d
	e.senderInfo = platform.UserInfo{ID: strconv.FormatInt(d.InitiatorID, 10)}
	e.chat = platform.ChatInfo{
		ID:      encodeChatID(sceneGroup, d.GroupID),
		IsGroup: true,
	}
	return e, nil
}

// ── 机器人下线 ───────────────────────────────────────────────────────────────

func parseBotOfflineEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d botOfflineData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse bot_offline: %w", err)
	}
	e.kind = platform.EventKindSystem
	e.id = fmt.Sprintf("bot_offline:%d", e.timestamp.Unix())
	e.rawPayload = &d
	e.content = d.Reason
	return e, nil
}

// ── peer_pin_change ──────────────────────────────────────────────────────────

func parsePeerPinChangeEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d peerPinChangeData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse peer_pin_change: %w", err)
	}
	e.kind = platform.EventKindNotice
	e.id = fmt.Sprintf("peer_pin_change:%s:%d:%d", d.MessageScene, d.PeerID, e.timestamp.Unix())
	e.rawPayload = &d
	isGroup := d.MessageScene == sceneGroup
	e.chat = platform.ChatInfo{
		ID:      encodeChatID(d.MessageScene, d.PeerID),
		IsGroup: isGroup,
	}
	return e, nil
}

// ── group_invited_join_request ───────────────────────────────────────────────

// parseGroupInvitedJoinRequestEvent 处理群管理员邀请他人加群的请求事件。
// 与 group_join_request（用户主动申请入群）不同，
// 此事件由其他成员邀请某人加群后触发，需要管理员审批。
func parseGroupInvitedJoinRequestEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d groupInvitedJoinRequestData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse group_invited_join_request: %w", err)
	}
	e.kind = platform.EventKindRequest
	e.id = fmt.Sprintf("invited_join_req:%d:%d:%d", d.GroupID, d.InitiatorID, d.NotificationSeq)
	e.rawPayload = &d
	e.senderInfo = platform.UserInfo{ID: strconv.FormatInt(d.InitiatorID, 10)}
	e.chat = platform.ChatInfo{
		ID:      encodeChatID(sceneGroup, d.GroupID),
		IsGroup: true,
	}
	return e, nil
}

// ── friend_nudge ─────────────────────────────────────────────────────────────

func parseFriendNudgeEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d friendNudgeData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse friend_nudge: %w", err)
	}
	e.kind = platform.EventKindNotice
	e.id = fmt.Sprintf("friend_nudge:%d:%d", d.UserID, e.timestamp.Unix())
	e.rawPayload = &d
	e.senderInfo = platform.UserInfo{ID: strconv.FormatInt(d.UserID, 10)}
	e.chat = platform.ChatInfo{ID: encodeChatID(sceneFriend, d.UserID)}
	e.content = d.DisplayAction + d.DisplaySuffix
	return e, nil
}

// ── friend_file_upload ───────────────────────────────────────────────────────

func parseFriendFileUploadEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d friendFileUploadData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse friend_file_upload: %w", err)
	}
	e.kind = platform.EventKindNotice
	e.id = fmt.Sprintf("friend_file:%d:%s", d.UserID, d.FileID)
	e.rawPayload = &d
	e.senderInfo = platform.UserInfo{ID: strconv.FormatInt(d.UserID, 10)}
	e.chat = platform.ChatInfo{ID: encodeChatID(sceneFriend, d.UserID)}
	e.attachments = []platform.Attachment{{
		Name: d.FileName,
		Size: int(d.FileSize),
		Extra: map[string]any{ExtraKeyFile: &FileSegmentMeta{
			FileID:   d.FileID,
			FileName: d.FileName,
			FileSize: d.FileSize,
		}},
	}}
	return e, nil
}

// ── group_essence_message_change ─────────────────────────────────────────────

func parseGroupEssenceMessageChangeEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d groupEssenceMessageChangeData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse group_essence_message_change: %w", err)
	}
	e.kind = platform.EventKindNotice
	e.id = fmt.Sprintf("essence_change:%d:%d:%d", d.GroupID, d.MessageSeq, e.timestamp.Unix())
	e.rawPayload = &d
	e.senderInfo = platform.UserInfo{ID: strconv.FormatInt(d.OperatorID, 10)}
	e.chat = platform.ChatInfo{
		ID:      encodeChatID(sceneGroup, d.GroupID),
		IsGroup: true,
	}
	return e, nil
}

// ── group_name_change ────────────────────────────────────────────────────────

func parseGroupNameChangeEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d groupNameChangeData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse group_name_change: %w", err)
	}
	e.kind = platform.EventKindNotice
	e.id = fmt.Sprintf("group_name_change:%d:%d", d.GroupID, e.timestamp.Unix())
	e.rawPayload = &d
	e.senderInfo = platform.UserInfo{ID: strconv.FormatInt(d.OperatorID, 10)}
	e.chat = platform.ChatInfo{
		ID:      encodeChatID(sceneGroup, d.GroupID),
		IsGroup: true,
		Name:    d.NewGroupName,
	}
	e.content = d.NewGroupName
	return e, nil
}

// ── group_nudge ──────────────────────────────────────────────────────────────

func parseGroupNudgeEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d groupNudgeData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse group_nudge: %w", err)
	}
	e.kind = platform.EventKindNotice
	e.id = fmt.Sprintf("group_nudge:%d:%d:%d:%d", d.GroupID, d.SenderID, d.ReceiverID, e.timestamp.Unix())
	e.rawPayload = &d
	e.senderInfo = platform.UserInfo{ID: strconv.FormatInt(d.SenderID, 10)}
	e.chat = platform.ChatInfo{
		ID:      encodeChatID(sceneGroup, d.GroupID),
		IsGroup: true,
	}
	e.content = d.DisplayAction + d.DisplaySuffix
	return e, nil
}

// ── group_file_upload ────────────────────────────────────────────────────────

func parseGroupFileUploadEvent(e *milkyEvent, data []byte) (platform.Event, error) {
	var d groupFileUploadData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("milky: parse group_file_upload: %w", err)
	}
	e.kind = platform.EventKindNotice
	e.id = fmt.Sprintf("group_file:%d:%s", d.GroupID, d.FileID)
	e.rawPayload = &d
	e.senderInfo = platform.UserInfo{ID: strconv.FormatInt(d.UserID, 10)}
	e.chat = platform.ChatInfo{
		ID:      encodeChatID(sceneGroup, d.GroupID),
		IsGroup: true,
	}
	e.attachments = []platform.Attachment{{
		Name: d.FileName,
		Size: int(d.FileSize),
		Extra: map[string]any{ExtraKeyFile: &FileSegmentMeta{
			FileID:   d.FileID,
			FileName: d.FileName,
			FileSize: d.FileSize,
		}},
	}}
	return e, nil
}

// ────────────────────────────────────────────────────────────────────────────
// 辅助函数
// ────────────────────────────────────────────────────────────────────────────

func senderFromMessage(msg *incomingMessage) platform.UserInfo {
	info := platform.UserInfo{
		ID: strconv.FormatInt(msg.SenderID, 10),
	}

	switch msg.MessageScene {
	case sceneFriend:
		if msg.Friend != nil {
			info.DisplayName = msg.Friend.Nickname
			if msg.Friend.Remark != "" {
				info.DisplayName = msg.Friend.Remark
			}
		}
	case sceneGroup:
		if msg.GroupMember != nil {
			info.DisplayName = msg.GroupMember.Nickname
			if msg.GroupMember.Card != "" {
				info.DisplayName = msg.GroupMember.Card
			}
			switch msg.GroupMember.Role {
			case "owner":
				info.GroupRole = platform.GroupRoleOwner
			case "admin":
				info.GroupRole = platform.GroupRoleAdmin
			default:
				info.GroupRole = platform.GroupRoleMember
			}
		}
	}
	return info
}
