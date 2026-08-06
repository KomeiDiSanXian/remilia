// Package qq 是 QQ 官方机器人平台的 platform.Adapter 实现。
//
// 本包将 QQ 官方数据结构（dto.Payload、dto.C2CMessageCreateEvent 等）
// 包装为 platform.Event，使框架核心不再直接依赖 QQ SDK 类型。
package qq

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/tidwall/gjson"
)

// PlatformID 是 QQ 平台适配器的平台标识符，值为 "qq"。
const PlatformID = "qq"

// qqEvent 将 QQ 的 *dto.Payload 解析后的字段存储为平台无关类型。
//
// D5：不再持有 *dto.Payload 引用，populate 完成后立即将 payload 释放回对象池，
// 避免事件对象长期持有池资源，降低 GC 压力。
type qqEvent struct {
	kind      platform.EventKind
	sender    platform.UserInfo
	chat      platform.ChatInfo
	segments  []platform.Segment
	timestamp time.Time
	id        string // 对应 payload.ID，populate 后独立持有
	rawType   string // 对应 payload.Type，populate 后独立持有
	// replyToID 事件级上下文回复目标（interaction 被操作消息 / reaction 被表态消息等非内容场景）；
	// 内容型回复（消息 message_reference / 引用消息）以段中的 SegmentReply 为准。
	replyToID string
	// mentions @ 用户列表（仅 GROUP_MESSAGE_CREATE 含 mentions 字段）
	mentions []platform.UserInfo
}

// NewEvent 从 QQ payload 创建 platform.Event。
//
// D5：提取所有字段后立即调用 dto.ReleasePayload，将 payload 还给对象池。
// 调用方在 NewEvent 返回后不得再访问 payload。
func NewEvent(payload *dto.Payload) platform.Event {
	e := &qqEvent{}
	if payload == nil {
		e.kind = platform.EventKindUnknown
		return e
	}
	// 在 populate 前提取不依赖 Detail 的字段
	e.id = string(payload.ID)
	e.rawType = payload.Type
	e.populateFrom(payload.Type, payload.Detail)
	// 所有字段已拷贝到 e，立即释放 payload 回对象池
	dto.ReleasePayload(payload)
	return e
}

// populateFrom 根据事件类型解析 detail 字节并填充 e 的字段。
func (e *qqEvent) populateFrom(evType string, detail json.RawMessage) {
	switch evType {
	case dto.C2CMessageCreate:
		e.kind = platform.EventKindPrivateMessage
		e.populateC2C(detail)
		// TokenMsgID 已在 populateC2C 中写入 ChatInfo.Tokens
	case dto.GroupAtMessageCreate, dto.GroupMessageCreate:
		e.kind = platform.EventKindGroupMessage
		e.populateGroupAt(detail)
		// TokenMsgID 已在 populateGroupAt 中写入 ChatInfo.Tokens
	case dto.Ready, dto.Resumed:
		e.kind = platform.EventKindSystem
	case dto.GroupAddRobot:
		e.kind = platform.EventKindBotAdded
		e.populateNoticeGroup(detail)
		e.chat.Tokens = map[string]string{TokenEventID: e.id} // 支持 event_id 被动回复
	case dto.GroupDelRobot:
		e.kind = platform.EventKindBotRemoved
		e.populateNoticeGroup(detail)
		// GroupDelRobot 不支持被动回复
	case dto.GroupMsgReject:
		e.kind = platform.EventKindMsgPermissionChange
		e.populateNoticeGroup(detail)
		// GroupMsgReject 不支持被动回复
	case dto.GroupMsgReceive:
		e.kind = platform.EventKindMsgPermissionChange
		e.populateNoticeGroup(detail)
		e.chat.Tokens = map[string]string{TokenEventID: e.id} // 支持 event_id 被动回复
	case dto.FriendAdd:
		e.kind = platform.EventKindFriendAdded
		e.populateNoticeUser(detail)
		e.chat.Tokens = map[string]string{TokenEventID: e.id} // 支持 event_id 被动回复
	case dto.FriendDel:
		e.kind = platform.EventKindFriendRemoved
		e.populateNoticeUser(detail)
		// FriendDel 不支持被动回复
	case dto.C2CMsgReject:
		e.kind = platform.EventKindMsgPermissionChange
		e.populateNoticeUser(detail)
		// C2CMsgReject 不支持被动回复
	case dto.C2CMsgReceive:
		e.kind = platform.EventKindMsgPermissionChange
		e.populateNoticeUser(detail)
		e.chat.Tokens = map[string]string{TokenEventID: e.id} // 支持 event_id 被动回复
	case dto.AtMessageCreate, dto.MessageCreate, dto.DirectMessageCreate:
		e.kind = platform.EventKindGuildMessage
		e.populateGuildMessage(evType, detail)
		// TokenMsgID 已在 populateGuildMessage 中写入 ChatInfo.Tokens
	case dto.InteractionCreate:
		e.kind = platform.EventKindInteraction
		// 被动回复 event_id 须取"事件最外层的 id"（即 payload.ID），
		// 群消息发送接口文档明确：event_id 从事件最外层的 id 获取。
		// 须在 populateInteraction 前保存 payload.ID，因为 populateInteraction
		// 会将 e.id 覆盖为 interaction body id（即 RespondInteraction 所需的
		// interaction_id），且会重新赋值 e.chat（覆盖任何此前对 chat 的设置）。
		savedEventID := e.id
		e.populateInteraction(detail)
		e.chat.Tokens = map[string]string{TokenEventID: savedEventID}
	// ── 表情表态事件 ────────────────────────────────────────────────────────
	case dto.MessageReactionAdd, dto.MessageReactionRemove:
		e.kind = platform.EventKindReaction
		e.populateMessageReaction(detail)
	// ── 频道（Guild）事件 ──────────────────────────────────────────────────
	case dto.GuildCreate:
		e.kind = platform.EventKindBotAdded // 机器人加入频道
		e.populateGuildEvent(detail)
	case dto.GuildUpdate:
		e.kind = platform.EventKindGuildChange
		e.populateGuildEvent(detail)
	case dto.GuildDelete:
		e.kind = platform.EventKindBotRemoved // 机器人退出频道
		e.populateGuildEvent(detail)
	// ── 频道成员事件 ────────────────────────────────────────────────────────
	case dto.GuildMemberAdd:
		e.kind = platform.EventKindMemberJoin
		e.populateGuildMemberEvent(detail)
	case dto.GuildMemberUpdate:
		e.kind = platform.EventKindMemberUpdate
		e.populateGuildMemberEvent(detail)
	case dto.GuildMemberRemove:
		e.kind = platform.EventKindMemberLeave
		e.populateGuildMemberEvent(detail)
	// ── 群聊成员事件 ────────────────────────────────────────────────────────
	case dto.GroupMemberAdd:
		e.kind = platform.EventKindMemberJoin
		e.populateNoticeGroup(detail)
	case dto.GroupMemberRemove:
		e.kind = platform.EventKindMemberLeave
		e.populateNoticeGroup(detail)
	// ── 子频道事件 ──────────────────────────────────────────────────────────
	case dto.ChannelCreate, dto.ChannelUpdate, dto.ChannelDelete:
		e.kind = platform.EventKindChannelChange
		e.populateChannelEvent(detail)
	// ── 消息撤回事件 ────────────────────────────────────────────────────────
	case dto.MessageDeleteEvent:
		e.kind = platform.EventKindMessageDelete
		e.populateMessageDelete(detail)
	// ── 消息审核事件 ────────────────────────────────────────────────────────
	case dto.MessageAudit:
		e.kind = platform.EventKindMessageAudit
		e.populateMessageAudit(detail)
	default:
		e.kind = platform.EventKindUnknown
	}
}

func (e *qqEvent) populateC2C(detail json.RawMessage) {
	if detail == nil {
		return
	}
	// 一次线性扫描提取所有字段（O(n) 而非 O(k×n)）
	results := gjson.GetManyBytes(detail,
		"id",                 // [0] 消息 ID（用于 msg_id 被动回复授权）
		"content",            // [1]
		"author.user_openid", // [2]
		"author.id",          // [3]
		"timestamp",          // [4]
		"attachments",        // [5]
		"message_type",       // [6] 消息内容类型
		"ark_data",           // [7] 结构化卡片数据（message_type=3）
	)
	userOpenID := results[2].String()
	e.sender = platform.UserInfo{
		ID:          userOpenID,
		DisplayName: results[3].String(),
	}
	e.chat = platform.ChatInfo{
		ID:      userOpenID,
		IsGroup: false,
		Tokens:  map[string]string{TokenMsgID: results[0].String()}, // msg_id 被动回复
	}
	if ts := results[4].String(); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.timestamp = t
		}
	}

	// message_type=3 结构化卡片：content 为空，卡片数据在 ark_data
	if int(results[6].Int()) == 3 {
		if raw := results[7].Raw; raw != "" {
			e.segments = append(e.segments, arkDataSegment(raw))
			return
		}
	}
	e.segments = textAndMediaSegments(results[1].String(), parseAttachments(results[5]))
}

func (e *qqEvent) populateGroupAt(detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
		"id",                        // [0] 消息 ID（用于 msg_id 被动回复授权）
		"content",                   // [1]
		"author.member_openid",      // [2]
		"author.username",           // [3] 发送者昵称
		"group_openid",              // [4]
		"timestamp",                 // [5]
		"attachments",               // [6]
		"message_type",              // [7] 消息内容类型
		"author.member_role",        // [8] 群角色
		"author.union_openid",       // [9] 跨应用统一 OpenID
		"author.union_user_account", // [10] 跨应用统一账号
		"msg_elements",              // [11] 消息元素列表（引用消息）
		"ark_data",                  // [12] 结构化卡片数据（message_type=3）
		"message_scene.ext",         // [13] 场景扩展（含 ref_msg_idx=REFIDX_... 引用标识）
		"parallel_message",          // [14] 并行消息/转发节点（msg_nodes）
	)
	content := results[1].String()
	msgType := int(results[7].Int())
	msgElements := results[11]

	// message_type=3 结构化卡片：content 为空，卡片数据在 ark_data
	if msgType == 3 {
		if raw := results[12].Raw; raw != "" {
			e.segments = append(e.segments, arkDataSegment(raw))
		}
	}

	// message_type=103 引用消息（实测结构，2026-08 报文核验）：
	//   - 引用目标标识在外层 message_scene.ext 的 ref_msg_idx=REFIDX_xxx（非 msg_elements）
	//   - msg_elements[0] 为被引用消息：content（文本）+ attachments（媒体）
	//   - 外层 content 是回复者正文（含 <@id> 占位符，非空格时同样解析）
	//   - parallel_message.msg_nodes 为并行消息/转发形态
	if msgType == 103 {
		if refIdx := quoteReplyID(results[13]); refIdx != "" {
			e.segments = append(e.segments, platform.Segment{
				Type:      platform.SegmentReply,
				ReplyToID: refIdx,
				Extra:     map[string]any{"raw_quote": msgElements.Raw}, // 同平台转发还原
			})
		}
		if pm := results[14]; pm.IsObject() && pm.Get("msg_nodes").IsArray() {
			e.segments = append(e.segments, platform.Segment{
				Type:  platform.SegmentForward,
				Extra: map[string]any{"parallel_message": pm.Raw},
			})
		}
		if content == "" || content == " " {
			// 正文为空时用被引用消息文本兜底（纯文本引用场景）
			content = extractQuoteContent(msgElements)
		}
	}

	memberOpenID := results[2].String()
	e.sender = platform.UserInfo{
		ID:          memberOpenID,
		DisplayName: results[3].String(),
		GroupRole:   parseQQGroupRole(results[8].String()),
	}

	e.chat = platform.ChatInfo{
		ID:      results[4].String(),
		IsGroup: true,
		Tokens:  map[string]string{TokenMsgID: results[0].String()}, // msg_id 被动回复
	}
	if ts := results[5].String(); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.timestamp = t
		}
	}

	// 解析 mentions 数组（payload 级结构化 @，含 bot/is_you 标记）
	mentionByID := make(map[string]platform.UserInfo, 2)
	mentionsResult := gjson.GetBytes(detail, "mentions")
	if mentionsResult.IsArray() {
		arr := mentionsResult.Array()
		e.mentions = make([]platform.UserInfo, 0, len(arr))
		for _, m := range arr {
			mentionedID := m.Get("id").String()
			if mentionedID == "" {
				continue
			}
			u := platform.UserInfo{
				ID:          mentionedID,
				DisplayName: m.Get("username").String(),
				IsBot:       m.Get("bot").Bool(),
				IsSelf:      m.Get("is_you").Bool(),
			}
			mentionByID[mentionedID] = u
			e.mentions = append(e.mentions, u)
		}
	}

	// 段：文本/at 按 <@id> 占位符位置内联交错（分散 at 基准用例）→ 附件。
	e.segments = append(e.segments, splitMentionedContent(content, mentionByID)...)
	e.segments = append(e.segments, mediaSegments(parseAttachments(results[6]))...)
}

// splitMentionedContent 按 <@id> 占位符把正文切成 text/at 交错段。
//
// 占位符即位置信息：@ 出现在正文中的实际位置（QQ 群消息 mentions 格式）。
// 无法匹配 mentions 数组的占位符按原样作为文本保留。
func splitMentionedContent(content string, mentionByID map[string]platform.UserInfo) []platform.Segment {
	if content == "" {
		return nil
	}
	var out []platform.Segment
	pos := 0
	for _, loc := range qqMentionTokenRe.FindAllStringIndex(content, -1) {
		id := content[loc[0]+2 : loc[1]-1] // <@id> → id
		if loc[0] > pos {
			out = append(out, platform.Segment{Type: platform.SegmentText, Text: content[pos:loc[0]]})
		}
		if u, ok := mentionByID[id]; ok {
			out = append(out, platform.Segment{
				Type:   platform.SegmentAt,
				UserID: u.ID,
				Text:   u.DisplayName,
				Extra:  map[string]any{platform.SegmentExtraIsSelf: u.IsSelf},
			})
		} else {
			out = append(out, platform.Segment{Type: platform.SegmentText, Text: content[loc[0]:loc[1]]})
		}
		pos = loc[1]
	}
	if pos < len(content) {
		out = append(out, platform.Segment{Type: platform.SegmentText, Text: content[pos:]})
	}
	return out
}

// extractQuoteContent 从 msg_elements 中提取被引用消息的文本内容。
// 纯文本引用场景（正文为空）时用作正文兜底。
func extractQuoteContent(r gjson.Result) string {
	if !r.IsArray() {
		return ""
	}
	arr := r.Array()
	if len(arr) == 0 {
		return ""
	}
	return arr[0].Get("content").String()
}

// quoteReplyID 从 message_scene.ext 中提取引用目标标识（ref_msg_idx=REFIDX_xxx）。
//
// 实测（2026-08 报文核验）：引用消息的引用目标 ID 不在 msg_elements 内，
// 而在外层 message_scene.ext 数组中，格式为 "ref_msg_idx=REFIDX_..."。
func quoteReplyID(ext gjson.Result) string {
	if !ext.IsArray() {
		return ""
	}
	const prefix = "ref_msg_idx="
	for _, e := range ext.Array() {
		if s := e.String(); strings.HasPrefix(s, prefix) {
			return strings.TrimPrefix(s, prefix)
		}
	}
	return ""
}

func (e *qqEvent) populateGuildMessage(evType string, detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
		"content",
		"author.id",
		"author.username",
		"channel_id",
		"guild_id",
		"channel_name",
		"timestamp",
		"attachments",
		"message_reference.message_id",
	)
	content := results[0].String()
	e.sender = platform.UserInfo{
		ID:          results[1].String(),
		DisplayName: results[2].String(),
	}
	channelID := results[3].String()
	guildID := results[4].String()

	isDM := evType == dto.DirectMessageCreate
	if isDM {
		// 频道私信：以 guild_id 作为发送目标（POST /dms/{guild_id}/messages），
		// channel_id 仅作辅助信息，不用于发送。
		e.chat = platform.ChatInfo{
			ID:       guildID, // 私信会话 ID（用于 DMChat）
			ParentID: guildID,
			Name:     results[5].String(),
			IsGroup:  false,
			IsDM:     true,
			Tokens:   map[string]string{TokenMsgID: e.id}, // payload.ID 即 message id，用于 msg_id 被动回复
		}
	} else {
		chatID := channelID
		if chatID == "" {
			chatID = guildID
		}
		e.chat = platform.ChatInfo{
			ID:       chatID,
			ParentID: guildID,
			Name:     results[5].String(),
			IsGroup:  true,
			Tokens:   map[string]string{TokenMsgID: e.id}, // payload.ID 即 message id，用于 msg_id 被动回复
		}
	}

	if ts := results[6].String(); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.timestamp = t
		}
	}
	// 段：message_reference 回复 → 正文文本 → 附件
	if refID := results[8].String(); refID != "" {
		e.segments = append(e.segments, platform.Segment{Type: platform.SegmentReply, ReplyToID: refID})
	}
	e.segments = append(e.segments, textAndMediaSegments(content, parseAttachments(results[7]))...)
}

// populateGuildEvent 解析频道事件（机器人加入/退出/更新频道）。
func (e *qqEvent) populateGuildEvent(detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
		"id",
		"name",
		"owner_id",
		"joined_at",
	)
	guildID := results[0].String()
	e.chat = platform.ChatInfo{
		ID:      guildID,
		Name:    results[1].String(),
		IsGroup: true,
	}
	e.sender = platform.UserInfo{
		ID: results[2].String(),
	}
	if ts := results[3].String(); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.timestamp = t
		}
	}
}

// populateGuildMemberEvent 解析频道成员事件（加入/更新/移除成员）。
func (e *qqEvent) populateGuildMemberEvent(detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
		"guild_id",
		"user.id",
		"user.username",
		"nick",
		"joined_at",
		"op_user_id",
	)
	e.chat = platform.ChatInfo{
		ID:      results[0].String(),
		IsGroup: true,
	}
	e.sender = platform.UserInfo{
		ID:          results[1].String(),
		DisplayName: results[2].String(),
	}
	if ts := results[4].String(); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.timestamp = t
		}
	}
}

// populateChannelEvent 解析子频道事件（创建/更新/删除子频道）。
func (e *qqEvent) populateChannelEvent(detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
		"id",
		"guild_id",
		"name",
		"op_user_id",
	)
	e.chat = platform.ChatInfo{
		ID:       results[0].String(),
		ParentID: results[1].String(),
		Name:     results[2].String(),
		IsGroup:  true,
	}
	e.sender = platform.UserInfo{
		ID: results[3].String(),
	}
}

// populateMessageDelete 解析频道消息撤回事件。
func (e *qqEvent) populateMessageDelete(detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
		"message.id",
		"message.channel_id",
		"message.guild_id",
		"op_user.id",
	)
	e.id = results[0].String()
	e.chat = platform.ChatInfo{
		ID:       results[1].String(),
		ParentID: results[2].String(),
		IsGroup:  true,
	}
	e.sender = platform.UserInfo{
		ID: results[3].String(),
	}
}

// populateMessageAudit 解析消息审核事件（MESSAGE_AUDIT）。
func (e *qqEvent) populateMessageAudit(detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
		"audit_id",     // [0]
		"message_id",   // [1]
		"guild_id",     // [2]
		"channel_id",   // [3]
		"audit_result", // [4]
		"create_time",  // [5]
	)
	e.id = results[1].String() // 被审核的消息 ID
	e.chat = platform.ChatInfo{
		ID:       results[3].String(),
		ParentID: results[2].String(),
		IsGroup:  true,
	}
	e.segments = textSegments(results[0].String()) // audit_id 作为 content 供 handler 匹配
	if ts := results[5].String(); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.timestamp = t
		}
	}
}

// parseAttachments 将 gjson 数组结果转换为平台无关的 Attachment 切片。
//
// 使用 r.Array() 预获取元素数量，一次性分配输出切片，避免 append 扩容。
// QQ 语音附件的专属字段（voice_wav_url、asr_refer_text）通过 Extra map
// 以 ExtraKeyVoice → *VoiceAttachmentMeta 携带，不污染平台无关字段。
// parseQQGroupRole 将 QQ 群角色字符串映射为 platform.GroupRole。
func parseQQGroupRole(role string) platform.GroupRole {
	switch role {
	case "owner":
		return platform.GroupRoleOwner
	case "admin":
		return platform.GroupRoleAdmin
	case "member":
		return platform.GroupRoleMember
	default:
		return platform.GroupRoleUnknown
	}
}

// qqMentionTokenRe 匹配 QQ 群消息正文中的 @ 占位符 <@member_openid>。
var qqMentionTokenRe = regexp.MustCompile(`<@([^>]+)>`)

// arkDataSegment 将结构化卡片（message_type=3）的 ark_data 原始 JSON 包装为段。
//
// 卡片载荷为平台原生 KV 结构（字段名官方文档未全量公开），整体保留在
// Extra[ExtraKeyArkData]，供同平台转发还原；跨平台转发按未知段剥离。
func arkDataSegment(raw string) platform.Segment {
	return platform.Segment{
		Type:  platform.SegmentUnknown,
		Extra: map[string]any{"type": "ark_data", ExtraKeyArkData: raw},
	}
}

// textSegments 将纯文本包装为单条 text 段。
func textSegments(text string) []platform.Segment {
	if text == "" {
		return nil
	}
	return []platform.Segment{{Type: platform.SegmentText, Text: text}}
}

// mediaSegments 将附件列表按 MIME 分类映射为媒体段（保序）。
func mediaSegments(atts []platform.Attachment) []platform.Segment {
	var segs []platform.Segment
	for _, att := range atts {
		var t platform.SegmentType
		mime := strings.ToLower(att.MimeType)
		switch {
		case strings.HasPrefix(mime, "image/"):
			t = platform.SegmentImage
		case strings.HasPrefix(mime, "audio/"):
			t = platform.SegmentAudio
		case strings.HasPrefix(mime, "video/"):
			t = platform.SegmentVideo
		default:
			t = platform.SegmentFile
		}
		segs = append(segs, platform.Segment{Type: t, Attachment: att})
	}
	return segs
}

// textAndMediaSegments 拼接文本段与媒体段（正文在前）。
func textAndMediaSegments(text string, atts []platform.Attachment) []platform.Segment {
	segs := textSegments(text)
	return append(segs, mediaSegments(atts)...)
}

func parseAttachments(r gjson.Result) []platform.Attachment {
	if !r.IsArray() || len(r.Raw) == 0 {
		return nil
	}
	arr := r.Array()
	if len(arr) == 0 {
		return nil
	}
	out := make([]platform.Attachment, 0, len(arr))
	for _, v := range arr {
		att := platform.Attachment{
			URL:      v.Get("url").String(),
			MimeType: v.Get("content_type").String(),
			Name:     v.Get("filename").String(),
			Size:     int(v.Get("size").Int()),
			Width:    int(v.Get("width").Int()),
			Height:   int(v.Get("height").Int()),
		}
		// QQ 专属语音元数据：通过 Extra 携带，不污染平台无关字段
		wavURL := v.Get("voice_wav_url").String()
		asrText := v.Get("asr_refer_text").String()
		if wavURL != "" || asrText != "" {
			att.Extra = map[string]any{ExtraKeyVoice: &VoiceAttachmentMeta{WavURL: wavURL, AsrText: asrText}}
		}
		if att.URL != "" || att.Name != "" {
			out = append(out, att)
		}
	}
	return out
}

// populateInteraction 解析 INTERACTION_CREATE 事件（按钮回调 / 快捷菜单）。
//
// scene 字段决定填充策略：
//   - c2c：UserOpenID → sender & chat
//   - group：GroupMemberOpenID → sender，GroupOpenID → chat
//   - guild：resolved.user_id → sender，ChannelID + GuildID → chat
//
// e.id 被覆盖为交互事件体的 id（即 RespondInteraction 所需的 interactionID）。
// e.content 根据交互类型填充：
//   - type=11（消息按钮）：设为 data.resolved.button_data（按钮 action.data 值）
//   - type=12（单聊快捷菜单）：设为 data.resolved.feature_id（菜单按钮 ID）
//
// e.replyToID 被设为 data.resolved.message_id（仅频道场景，被操作的消息 ID）。
func (e *qqEvent) populateInteraction(detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
		"id",                        // [0]  interaction body id（用于 RespondInteraction）
		"scene",                     // [1]  事件场景：c2c、group、guild
		"chat_type",                 // [2]  0=频道，1=群聊，2=单聊
		"user_openid",               // [3]  单聊触发用户 openid
		"group_openid",              // [4]  群聊 openid
		"group_member_openid",       // [5]  群成员 openid
		"guild_id",                  // [6]  频道 openid
		"channel_id",                // [7]  文字子频道 openid
		"data.resolved.button_data", // [8]  消息按钮 action.data（type=11）
		"data.resolved.user_id",     // [9]  操作用户 userid（仅频道）
		"timestamp",                 // [10] 触发时间（RFC3339）
		"type",                      // [11] 交互类型：11=消息按钮，12=单聊快捷菜单
		"data.resolved.feature_id",  // [12] 快捷菜单按钮 ID（type=12）
		"data.resolved.message_id",  // [13] 被操作消息 ID（仅频道场景）
	)
	// 覆盖 e.id 为 interaction body 中的 id（用于 RespondInteraction）
	if id := results[0].String(); id != "" {
		e.id = id
	}
	scene := results[1].String()
	switch scene {
	case "c2c":
		uid := results[3].String()
		e.sender = platform.UserInfo{ID: uid}
		e.chat = platform.ChatInfo{ID: uid, IsGroup: false}
	case "group":
		e.sender = platform.UserInfo{ID: results[5].String()}
		e.chat = platform.ChatInfo{ID: results[4].String(), IsGroup: true}
	case "guild":
		e.sender = platform.UserInfo{ID: results[9].String()}
		e.chat = platform.ChatInfo{
			ID:       results[7].String(),
			ParentID: results[6].String(),
			IsGroup:  true,
		}
	default:
		// 未知 scene：尝试根据 chat_type 填充
		chatType := int(results[2].Int())
		switch chatType {
		case 2: // 单聊
			uid := results[3].String()
			e.sender = platform.UserInfo{ID: uid}
			e.chat = platform.ChatInfo{ID: uid, IsGroup: false}
		case 1: // 群聊
			e.sender = platform.UserInfo{ID: results[5].String()}
			e.chat = platform.ChatInfo{ID: results[4].String(), IsGroup: true}
		case 0: // 频道
			e.sender = platform.UserInfo{ID: results[9].String()}
			e.chat = platform.ChatInfo{
				ID:       results[7].String(),
				ParentID: results[6].String(),
				IsGroup:  true,
			}
		}
	}
	// content 根据交互类型填充：
	//   type=12（单聊快捷菜单）→ feature_id（菜单按钮 ID，管理端配置）
	//   type=11 或其他（消息按钮）→ button_data（按钮 action.data 值）
	interactionType := int(results[11].Int())
	if interactionType == 12 {
		e.segments = textSegments(results[12].String()) // feature_id
	} else {
		e.segments = textSegments(results[8].String()) // button_data
	}
	// replyToID 设为被操作消息 ID（仅频道场景下存在，其他场景为空）
	e.replyToID = results[13].String()
	if ts := results[10].String(); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.timestamp = t
		}
	}
}

// populateMessageReaction 解析表情表态事件（MESSAGE_REACTION_ADD / MESSAGE_REACTION_REMOVE）。
//
// e.content 被设置为表情 ID，便于 handler 判断具体表情。
// e.replyToID 被设置为被表态的消息 ID（target.id），便于 handler 知道是哪条消息被表态。
func (e *qqEvent) populateMessageReaction(detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
		"user_id",    // [0] 发表表态的用户 ID
		"channel_id", // [1] 子频道 ID
		"guild_id",   // [2] 频道 ID
		"emoji.id",   // [3] 表情 ID
		"target.id",  // [4] 被表态的消息 ID（target.type=0 即消息）
	)
	e.sender = platform.UserInfo{ID: results[0].String()}
	e.chat = platform.ChatInfo{
		ID:       results[1].String(),
		ParentID: results[2].String(),
		IsGroup:  true,
	}
	// content 设为 emoji.id，方便 handler 判断是哪个表情
	e.segments = textSegments(results[3].String())
	// replyToID 设为被表态的消息 ID，方便 handler 知道哪条消息被表态
	e.replyToID = results[4].String()
}

func (e *qqEvent) populateNoticeGroup(detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
		"group_openid",
		"op_member_openid",
		"timestamp",
	)
	e.chat = platform.ChatInfo{
		ID:      results[0].String(),
		IsGroup: true,
	}
	e.sender = platform.UserInfo{
		ID: results[1].String(),
	}
	if ts := results[2].Int(); ts != 0 {
		e.timestamp = time.Unix(ts, 0)
	}
}

func (e *qqEvent) populateNoticeUser(detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
		"openid",
		"timestamp",
	)
	openID := results[0].String()
	e.sender = platform.UserInfo{ID: openID}
	e.chat = platform.ChatInfo{ID: openID, IsGroup: false}
	if ts := results[1].Int(); ts != 0 {
		e.timestamp = time.Unix(ts, 0)
	}
}

// Platform 返回平台标识符 "qq"。
func (e *qqEvent) Platform() string { return PlatformID }

// ID 返回事件/消息的唯一标识符。
func (e *qqEvent) ID() string { return e.id }

// Kind 返回事件类型。
func (e *qqEvent) Kind() platform.EventKind { return e.kind }

// RawType 返回 QQ 平台原始事件类型字符串（如 "C2C_MESSAGE_CREATE"）。
func (e *qqEvent) RawType() string { return e.rawType }

// Sender 返回消息发送者信息。
func (e *qqEvent) Sender() platform.UserInfo { return e.sender }

// Chat 返回消息所在会话信息。
func (e *qqEvent) Chat() platform.ChatInfo { return e.chat }

// Segments 返回保序统一消息段（唯一真相源）。
func (e *qqEvent) Segments() []platform.Segment { return e.segments }

// Content 返回段派生文本（at/quote 等剥离，见 platform.SegmentsContent）。
//
// 保持 qq 历史 TrimSpace 语义：段内首尾空白不进入 Content。
func (e *qqEvent) Content() string {
	return strings.TrimSpace(platform.SegmentsContent(e.segments))
}

// Attachments 返回段派生附件列表（image/audio/video/file）。
func (e *qqEvent) Attachments() []platform.Attachment {
	return platform.SegmentsAttachments(e.segments)
}

// Timestamp 返回消息发送时间。
func (e *qqEvent) Timestamp() time.Time { return e.timestamp }

// ReplyToID 实现 platform.ReplyEvent，返回被回复消息的平台原生 ID。
//
// 派生顺序：段内 SegmentReply（消息引用/引用消息）→ 事件级上下文字段
// （interaction 被操作消息 / reaction 被表态消息）。
func (e *qqEvent) ReplyToID() string {
	if id := platform.SegmentsReplyToID(e.segments); id != "" {
		return id
	}
	return e.replyToID
}

// Mentions 实现 platform.MentionsEvent，返回 @ 用户列表。
//
// 仅 GROUP_MESSAGE_CREATE 事件携带 mentions 字段，其他事件返回 nil。
func (e *qqEvent) Mentions() []platform.UserInfo { return e.mentions }

// RawPayload 返回 nil。
//
// D5：payload 在 populate 完成后已立即释放回对象池，不再长期持有引用。
// 需要访问 QQ 平台特定字段的代码应在 handler 内通过其他上下文获取。
func (e *qqEvent) RawPayload() any { return nil }
