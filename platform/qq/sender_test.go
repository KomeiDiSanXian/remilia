package qq

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func newTestChat() platform.ChatInfo {
	return platform.ChatInfo{
		ID:      "target_001",
		IsGroup: true,
	}
}

func TestBuildDTOMessage_Card(t *testing.T) {
	msg := platform.TextMessage("")
	msg = ApplyExtra(msg, MessageExtra{
		Card: &dto.Card{
			Type: "tuwen",
			Content: dto.CardContent{
				Title:       "Test Title",
				Description: "Test Description",
				PicURL:      "https://example.com/pic.png",
				URL:         "https://example.com",
			},
		},
	})
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, newTestChat())

	assert.EqualValues(t, 8, dtoMsg.Type, "msg_type should be CardMessage (8)")
	assert.NotNil(t, dtoMsg.Card, "Card should not be nil")
	assert.Equal(t, "tuwen", dtoMsg.Card.Type)
	assert.Equal(t, "Test Title", dtoMsg.Card.Content.Title)
	assert.Equal(t, "https://example.com", dtoMsg.Card.Content.URL)
}

func TestBuildDTOMessage_InputNotify(t *testing.T) {
	msg := platform.TextMessage("")
	msg = ApplyExtra(msg, MessageExtra{
		InputNotify: &dto.InputNotify{
			InputType:   1,
			InputSecond: 30,
		},
	})
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, newTestChat())

	assert.EqualValues(t, 6, dtoMsg.Type, "msg_type should be InputNotifyMsg (6)")
	assert.NotNil(t, dtoMsg.InputNotify, "InputNotify should not be nil")
	assert.Equal(t, 1, dtoMsg.InputNotify.InputType)
	assert.Equal(t, 30, dtoMsg.InputNotify.InputSecond)
}

func TestBuildDTOMessage_MarkdownTemplate(t *testing.T) {
	msg := platform.MarkdownMessage("# Hello")
	msg = ApplyExtra(msg, MessageExtra{
		MarkdownTemplateID: "tmpl_001",
		MarkdownParams: []dto.MarkdownParam{
			{Key: "name", Values: []string{"Alice"}},
		},
	})
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, newTestChat())

	assert.EqualValues(t, 2, dtoMsg.Type, "msg_type should be MarkdownMessage (2)")
	assert.NotNil(t, dtoMsg.Markdown)
	assert.Equal(t, "# Hello", dtoMsg.Markdown.Content)
	assert.Equal(t, "tmpl_001", dtoMsg.Markdown.CustomTemplateID)
	assert.Len(t, dtoMsg.Markdown.Params, 1)
	assert.Equal(t, "name", dtoMsg.Markdown.Params[0].Key)
}

func TestBuildDTOMessage_ArkPriority(t *testing.T) {
	msg := platform.TextMessage("")
	msg = ApplyExtra(msg, MessageExtra{
		Ark: &Ark{TemplateID: 23},
		Card: &dto.Card{
			Type:    "tuwen",
			Content: dto.CardContent{Title: "Card", Description: "desc"},
		},
	})
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, newTestChat())

	assert.EqualValues(t, 3, dtoMsg.Type, "Ark (3) should take priority over Card (8)")
	assert.NotNil(t, dtoMsg.Ark)
	assert.Nil(t, dtoMsg.Card)
}

func TestBuildDTOMessage_IsWakeup(t *testing.T) {
	msg := platform.TextMessage("wakeup msg").WithReply("msg_001")
	msg = ApplyExtra(msg, MessageExtra{
		IsWakeup: true,
		MsgSeq:   42,
	})
	chat := platform.ChatInfo{
		ID:      "user_001",
		IsGroup: false,
	}
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, chat)

	assert.True(t, dtoMsg.IsWakeup, "IsWakeup should be true")
	assert.Equal(t, uint64(42), dtoMsg.MessageSeq)
	// 召回消息应清除 msg_id 和 event_id
	assert.Empty(t, string(dtoMsg.MessageID), "MessageID should be empty for wakeup")
	assert.Empty(t, string(dtoMsg.EventID), "EventID should be empty for wakeup")
}

// TestBuildDTOMessage_PassiveAuthAndQuoteDecoupled 验证被动授权（msg_id）与
// 引用展示（message_reference）解耦：ReplyToID 只映射引用气泡，msg_id 只取
// 事件授权 token（TokenMsgID）。真机验证（2026-09）：该组合在单聊/群聊均正常。
func TestBuildDTOMessage_PassiveAuthAndQuoteDecoupled(t *testing.T) {
	msg := platform.TextMessage("quote me").WithReply("REFIDX_user_msg")
	chat := platform.ChatInfo{
		ID:      "user_001",
		IsGroup: false,
		Tokens:  map[string]string{TokenMsgID: "ROBOT1.0_inbound_event_id"},
	}
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, chat)

	assert.Equal(t, dto.EventID("ROBOT1.0_inbound_event_id"), dtoMsg.MessageID,
		"msg_id 应取事件授权 token，而不是 ReplyToID")
	require.NotNil(t, dtoMsg.MessageReference, "ReplyToID 非空时应设置 MessageReference")
	assert.Equal(t, "REFIDX_user_msg", dtoMsg.MessageReference.MessageID)
	assert.True(t, dtoMsg.MessageReference.IgnoreGetMessageError)
}

// TestBuildDTOMessage_QuoteWithoutAuthToken 验证无事件授权（主动消息）时
// ReplyToID 只产生引用气泡，不应被伪造为 msg_id。
func TestBuildDTOMessage_QuoteWithoutAuthToken(t *testing.T) {
	msg := platform.TextMessage("proactive quote").WithReply("REFIDX_proactive")
	chat := platform.ChatInfo{ID: "user_001", IsGroup: false}
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, chat)

	assert.Empty(t, string(dtoMsg.MessageID), "无授权 token 时不应设置 msg_id")
	require.NotNil(t, dtoMsg.MessageReference)
	assert.Equal(t, "REFIDX_proactive", dtoMsg.MessageReference.MessageID)
}

// TestBuildDTOMessage_PassiveAuthWithoutQuote 验证普通被动回复（有事件授权、
// 无 ReplyToID）不会凭空出现 message_reference。
func TestBuildDTOMessage_PassiveAuthWithoutQuote(t *testing.T) {
	msg := platform.TextMessage("plain passive reply")
	chat := platform.ChatInfo{
		ID:      "group_001",
		IsGroup: true,
		Tokens:  map[string]string{TokenMsgID: "ROBOT1.0_group_event_id"},
	}
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, chat)

	assert.Equal(t, dto.EventID("ROBOT1.0_group_event_id"), dtoMsg.MessageID)
	assert.Nil(t, dtoMsg.MessageReference, "无 ReplyToID 时不应设置 message_reference")
}

// TestBuildDTOMessage_QuoteTriggerDefaultOff 验证引用触发消息默认为关闭：
// 消息未带 QuoteTrigger 时，即使事件携带 TokenQuoteID 也不挂 message_reference
// （是否需要气泡由插件在发送时按条决定，无全局配置）。
func TestBuildDTOMessage_QuoteTriggerDefaultOff(t *testing.T) {
	msg := platform.TextMessage("passive reply")
	chat := platform.ChatInfo{
		ID:      "user_001",
		IsGroup: false,
		Tokens: map[string]string{
			TokenMsgID:   "ROBOT1.0_inbound",
			TokenQuoteID: "REFIDX_trigger_msg",
		},
	}
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, chat)

	assert.Equal(t, dto.EventID("ROBOT1.0_inbound"), dtoMsg.MessageID)
	assert.Nil(t, dtoMsg.MessageReference, "未标记 QuoteTrigger 时不应挂引用")
}

// TestBuildDTOMessage_QuoteTrigger 验证消息带 WithQuoteTrigger 时，被动回复
// 自动引用触发消息自身（message_reference.message_id = 事件 msg_idx REFIDX）。
func TestBuildDTOMessage_QuoteTrigger(t *testing.T) {
	msg := platform.TextMessage("passive reply").WithQuoteTrigger()
	chat := platform.ChatInfo{
		ID:      "group_001",
		IsGroup: true,
		Tokens: map[string]string{
			TokenMsgID:   "ROBOT1.0_group_inbound",
			TokenQuoteID: "REFIDX_trigger_msg",
		},
	}
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, chat)

	require.NotNil(t, dtoMsg.MessageReference, "QuoteTrigger 且事件带 msg_idx 时应挂引用")
	assert.Equal(t, "REFIDX_trigger_msg", dtoMsg.MessageReference.MessageID)
	assert.True(t, dtoMsg.MessageReference.IgnoreGetMessageError)
	// 引用不占用/影响被动授权 msg_id
	assert.Equal(t, dto.EventID("ROBOT1.0_group_inbound"), dtoMsg.MessageID)
}

// TestBuildDTOMessage_QuoteTriggerNoToken 验证 QuoteTrigger 但事件无 msg_idx
// 时静默不挂引用（如主动消息 / 无 message_scene.ext），消息仍正常发送。
func TestBuildDTOMessage_QuoteTriggerNoToken(t *testing.T) {
	msg := platform.TextMessage("reply without quote index").WithQuoteTrigger()
	chat := platform.ChatInfo{
		ID:      "user_001",
		IsGroup: false,
		Tokens:  map[string]string{TokenMsgID: "ROBOT1.0_inbound"},
	}
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, chat)

	assert.Nil(t, dtoMsg.MessageReference, "无 TokenQuoteID 时 QuoteTrigger 静默不生效")
	assert.Equal(t, dto.EventID("ROBOT1.0_inbound"), dtoMsg.MessageID)
}

// TestBuildDTOMessage_ExplicitReplyOverridesQuoteTrigger 验证显式 ReplyToID
// 优先于 QuoteTrigger：携带 reply 段 / WithReply 的消息引用显式目标。
func TestBuildDTOMessage_ExplicitReplyOverridesQuoteTrigger(t *testing.T) {
	msg := platform.TextMessage("explicit quote").
		WithReply("REFIDX_explicit_target").
		WithQuoteTrigger()
	chat := platform.ChatInfo{
		ID:      "group_001",
		IsGroup: true,
		Tokens: map[string]string{
			TokenMsgID:   "ROBOT1.0_inbound",
			TokenQuoteID: "REFIDX_trigger_msg",
		},
	}
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, chat)

	require.NotNil(t, dtoMsg.MessageReference)
	assert.Equal(t, "REFIDX_explicit_target", dtoMsg.MessageReference.MessageID,
		"显式 ReplyToID 应优先于 QuoteTrigger")
}

// TestBuildDTOMessage_QuoteTriggerWakeupSkipped 验证召回消息（IsWakeup）即使
// 标记 QuoteTrigger 也不引用触发消息：召回与来源消息解耦。
func TestBuildDTOMessage_QuoteTriggerWakeupSkipped(t *testing.T) {
	msg := ApplyExtra(platform.TextMessage("wakeup recall").WithQuoteTrigger(),
		MessageExtra{IsWakeup: true})
	chat := platform.ChatInfo{
		ID:      "user_001",
		IsGroup: false,
		Tokens: map[string]string{
			TokenMsgID:   "ROBOT1.0_inbound",
			TokenQuoteID: "REFIDX_trigger_msg",
		},
	}
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, chat)

	assert.True(t, dtoMsg.IsWakeup)
	assert.Empty(t, string(dtoMsg.MessageID))
	assert.Nil(t, dtoMsg.MessageReference, "召回消息不应携带触发消息引用")
}

// TestBuildGuildDTOMessage_QuoteTrigger 验证频道消息带 QuoteTrigger 时引用
// 事件消息 ID（频道 TokenMsgID = payload.ID 即 message id）。
func TestBuildGuildDTOMessage_QuoteTrigger(t *testing.T) {
	msg := platform.TextMessage("channel reply").WithQuoteTrigger()
	chat := platform.ChatInfo{
		ID:       "chan_001",
		ParentID: "guild_001",
		Tokens:   map[string]string{TokenMsgID: "ROBOT1.0_channel_trigger"},
	}
	s := &qqSender{}
	guildMsg := s.buildGuildDTOMessage(msg, chat)

	require.NotNil(t, guildMsg.MessageReference)
	assert.Equal(t, "ROBOT1.0_channel_trigger", guildMsg.MessageReference.MessageID)
}

// TestBuildGuildDTOMessage_QuoteTriggerNoMsgID 验证频道消息 QuoteTrigger 但无
// 事件授权消息 ID 时静默不挂引用。
func TestBuildGuildDTOMessage_QuoteTriggerNoMsgID(t *testing.T) {
	msg := platform.TextMessage("channel proactive").WithQuoteTrigger()
	chat := platform.ChatInfo{ID: "chan_001", ParentID: "guild_001"}
	s := &qqSender{}
	guildMsg := s.buildGuildDTOMessage(msg, chat)

	assert.Nil(t, guildMsg.MessageReference, "无事件消息 ID 时 QuoteTrigger 静默不生效")
}

// TestBuildSendResult_RefIDX 验证普通发送响应中的 ext_info.ref_idx 被保留到
// SendResult.RefIDX（供后续引用机器人自己的消息）。
func TestBuildSendResult_RefIDX(t *testing.T) {
	raw := gjson.Parse(`{"id":"ROBOT1.0_sent","timestamp":1725000000,"ext_info":{"ref_idx":"REFIDX_own_msg"}}`)
	result := buildSendResult(raw)
	qqResult, ok := result.Raw.(*SendResult)
	require.True(t, ok)
	assert.Equal(t, "REFIDX_own_msg", qqResult.RefIDX)
	assert.Equal(t, "ROBOT1.0_sent", qqResult.MessageID)
}

// TestBuildSendResult_NoRefIDX 验证响应缺失 ref_idx 时 RefIDX 保持为空。
func TestBuildSendResult_NoRefIDX(t *testing.T) {
	raw := gjson.Parse(`{"id":"ROBOT1.0_sent"}`)
	qqResult := buildSendResult(raw).Raw.(*SendResult)
	assert.Empty(t, qqResult.RefIDX)
}

// TestBuildSendResultFromUpload_RefIDX 验证富媒体两步发送的发送阶段响应同样
// 携带 RefIDX，且与上传阶段字段合并。
func TestBuildSendResultFromUpload_RefIDX(t *testing.T) {
	upload := gjson.Parse(`{"file_uuid":"uuid_1","file_info":"file_1","ttl":86400}`)
	send := gjson.Parse(`{"id":"ROBOT1.0_media","timestamp":1725000000,"ext_info":{"ref_idx":"REFIDX_own_media"}}`)
	result := buildSendResultFromUpload(upload, send)
	qqResult, ok := result.Raw.(*SendResult)
	require.True(t, ok)
	assert.Equal(t, "REFIDX_own_media", qqResult.RefIDX)
	assert.Equal(t, "ROBOT1.0_media", qqResult.MessageID)
	assert.Equal(t, "file_1", qqResult.FileInfo)
}

func TestBuildGuildDTOMessage_MarkdownTemplate(t *testing.T) {
	msg := platform.MarkdownMessage("# Channel")
	msg = ApplyExtra(msg, MessageExtra{
		MarkdownTemplateID: "tmpl_chan",
	})
	chat := platform.ChatInfo{
		ID:       "chan_001",
		ParentID: "guild_001",
	}
	s := &qqSender{}
	guildMsg := s.buildGuildDTOMessage(msg, chat)

	assert.NotNil(t, guildMsg.Markdown)
	assert.Equal(t, "# Channel", guildMsg.Markdown.Content)
	assert.Equal(t, "tmpl_chan", guildMsg.Markdown.CustomTemplateID)
}

// ── 出站段路径：段 → 便捷字段等价物，at 内联标签保序 ──────────────────

func TestQQSegmentsToFlat_InterleavedAt(t *testing.T) {
	// 分散 at 基准用例：at 内联标签保序交错
	msg := qqSegmentsToFlat(platform.OutboundMessage{Segments: []platform.Segment{
		{Type: platform.SegmentAt, UserID: "openidA"},
		{Type: platform.SegmentText, Text: "一段文本 "},
		{Type: platform.SegmentAt, UserID: "openidB"},
		{Type: platform.SegmentText, Text: "文本..."},
		{Type: platform.SegmentMentionAll},
	}})
	want := `<qqbot-at-user id="openidA" />一段文本 <qqbot-at-user id="openidB" />文本...` + dto.AtAll()
	assert.Equal(t, want, msg.Text)
	assert.Empty(t, msg.Segments)
}

func TestQQSegmentsToFlat_ReplyAndMedia(t *testing.T) {
	msg := qqSegmentsToFlat(platform.OutboundMessage{Segments: []platform.Segment{
		{Type: platform.SegmentReply, ReplyToID: "msg_1"},
		{Type: platform.SegmentText, Text: "hi"},
		{Type: platform.SegmentImage, Attachment: platform.Attachment{URL: "https://ex.com/a.png", Kind: platform.AttachmentKindImage}},
		{Type: platform.SegmentButton},
	}})
	assert.Equal(t, "msg_1", msg.ReplyToID)
	assert.Equal(t, "hi", msg.Text)
	require.Len(t, msg.Attachments, 1)
	assert.Equal(t, "https://ex.com/a.png", msg.Attachments[0].URL)
	assert.Empty(t, msg.Buttons, "按钮不参与段路径（混排受限）")
}

func TestConvertButtons_Extra(t *testing.T) {
	buttons := []platform.Button{
		{
			ID:    "btn_cmd",
			Label: "Run",
			Style: platform.ButtonStylePrimary,
			Extra: map[string]any{ExtraKeyButton: &ButtonExtra{
				Enter:  true,
				Reply:  true,
				Anchor: 0,
			}},
		},
		{
			ID:    "btn_link",
			Label: "Go",
			Style: platform.ButtonStyleLink,
			URL:   "https://example.com",
		},
	}

	kb := convertButtons(buttons)
	assert.NotNil(t, kb)
	assert.NotNil(t, kb.Content)
	assert.Len(t, kb.Content.Rows, 2, "two buttons with Row=0 should each be on their own row")

	// 第一个按钮应有 Enter/Reply
	firstRow := kb.Content.Rows[0]
	assert.Len(t, firstRow.Buttons, 1)
	btn0 := firstRow.Buttons[0]
	assert.Equal(t, "btn_cmd", btn0.ID)
	assert.True(t, btn0.Action.Enter, "ButtonExtra.Enter should be true")
	assert.True(t, btn0.Action.Reply, "ButtonExtra.Reply should be true")
	assert.Equal(t, 0, btn0.Action.Anchor)

	// 第二个按钮无 Extra，不应设置 enter/reply
	secondRow := kb.Content.Rows[1]
	btn1 := secondRow.Buttons[0]
	assert.Equal(t, "btn_link", btn1.ID)
	assert.False(t, btn1.Action.Enter, "link button should not have Enter")
	assert.Equal(t, 0, btn1.Action.Type, "link button action type should be 0 (jump)")
}

// TestConvertButtons_CommandButton 验证 Command 字段映射为指令按钮（type=2）。
func TestConvertButtons_CommandButton(t *testing.T) {
	buttons := []platform.Button{
		{
			ID:      "btn_help",
			Label:   "查看命令列表",
			Command: "/help",
			Style:   platform.ButtonStyleSecondary,
		},
		{
			ID:    "btn_cb",
			Label: "Callback",
			Style: platform.ButtonStylePrimary,
		},
	}
	kb := convertButtons(buttons)
	assert.NotNil(t, kb)
	assert.Len(t, kb.Content.Rows, 2)

	// Command 非空 → type=2（指令按钮），data 为命令文本
	btn0 := kb.Content.Rows[0].Buttons[0]
	assert.Equal(t, 2, btn0.Action.Type, "Command button should map to action type 2 (指令按钮)")
	assert.Equal(t, "/help", btn0.Action.Data)

	// 无 Command → 保持回调按钮（type=1），data 为按钮 ID
	btn1 := kb.Content.Rows[1].Buttons[0]
	assert.Equal(t, 1, btn1.Action.Type, "button without Command should stay callback (type 1)")
	assert.Equal(t, "btn_cb", btn1.Action.Data)
}

func TestConvertButtons_RowGrouping(t *testing.T) {
	buttons := []platform.Button{
		{ID: "a", Label: "A", Row: 1},
		{ID: "b", Label: "B", Row: 1},
		{ID: "c", Label: "C", Row: 0},
		{ID: "d", Label: "D", Row: 2},
	}
	kb := convertButtons(buttons)
	assert.NotNil(t, kb)
	assert.Len(t, kb.Content.Rows, 3)
	// Row 1 应有两个按钮在同一行
	assert.Len(t, kb.Content.Rows[0].Buttons, 2)
	assert.Equal(t, "a", kb.Content.Rows[0].Buttons[0].ID)
	assert.Equal(t, "b", kb.Content.Rows[0].Buttons[1].ID)
	// Row 0 的 c 独占一行
	assert.Len(t, kb.Content.Rows[1].Buttons, 1)
	assert.Equal(t, "c", kb.Content.Rows[1].Buttons[0].ID)
	// Row 2 的 d 独占一行
	assert.Len(t, kb.Content.Rows[2].Buttons, 1)
	assert.Equal(t, "d", kb.Content.Rows[2].Buttons[0].ID)
}

// ────────────────────────────────────────────────────────────────────────────
// 平台可选接口（2026-08 新增能力）
// ────────────────────────────────────────────────────────────────────────────

func TestQQSender_ImplementsOptionalInterfaces(t *testing.T) {
	s := &qqSender{}
	assert.Implements(t, (*platform.GroupManager)(nil), s)
	assert.Implements(t, (*platform.InvitationHandler)(nil), s)
	assert.Implements(t, (*platform.GroupInfoProvider)(nil), s)
}

func TestParseJoinInviteID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantG   string
		wantM   string
		wantJ   string
		wantErr bool
	}{
		{name: "valid", id: "g1:m1:j1", wantG: "g1", wantM: "m1", wantJ: "j1"},
		{name: "empty group", id: ":m1:j1", wantErr: true},
		{name: "empty member", id: "g1::j1", wantErr: true},
		{name: "empty join id", id: "g1:m1:", wantErr: true},
		{name: "too few parts", id: "g1:m1", wantErr: true},
		{name: "too many parts", id: "g1:m1:j1:extra", wantErr: true},
		{name: "empty", id: "", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, m, j, err := parseJoinInviteID(tc.id)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantG, g)
			assert.Equal(t, tc.wantM, m)
			assert.Equal(t, tc.wantJ, j)
		})
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 大文件分片上传辅助函数
// ────────────────────────────────────────────────────────────────────────────

func TestMD5Sum(t *testing.T) {
	assert.Equal(t, "d41d8cd98f00b204e9800998ecf8427e", md5Sum(nil))
	assert.Equal(t, "900150983cd24fb0d6963f7d28e17f72", md5Sum([]byte("abc")))
}

func TestFirstBytes(t *testing.T) {
	data := []byte("hello world")
	assert.Equal(t, "hello", string(firstBytes(data, 5)))
	assert.Equal(t, data, firstBytes(data, 100)) // 超出返回原切片
	assert.Nil(t, firstBytes(nil, 5))
}

func TestTruncateForLog(t *testing.T) {
	assert.Equal(t, "short", truncateForLog([]byte("short")))
	long := make([]byte, 500)
	for i := range long {
		long[i] = 'a'
	}
	out := truncateForLog(long)
	assert.Equal(t, 200+len("..."), len(out))
	assert.Equal(t, "...", out[len(out)-3:])
}

func TestPutPresignedChunk_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		body, _ := io.ReadAll(r.Body)
		assert.Equal(t, "chunk-data", string(body))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := putPresignedChunk(context.Background(), srv.URL, []byte("chunk-data"))
	require.NoError(t, err)
}

func TestPutPresignedChunk_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	err := putPresignedChunk(context.Background(), srv.URL, []byte("chunk-data"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

// fakeChunkedAPI 记录分片上传各阶段请求，返回与真实响应一致的形状。
type fakeChunkedAPI struct {
	openapi.OpenAPI
	prepareResult gjson.Result
	prepareReq    *dto.UploadPrepareRequest
	finishReqs    []*dto.UploadPartFinishRequest
	mergeMedia    *dto.Media
	chatMsg       *dto.Message
}

func (f *fakeChunkedAPI) GroupUploadPrepare(_ context.Context, _ string, req *dto.UploadPrepareRequest) (gjson.Result, error) {
	f.prepareReq = req
	return f.prepareResult, nil
}

func (f *fakeChunkedAPI) GroupUploadPartFinish(_ context.Context, _ string, req *dto.UploadPartFinishRequest) (gjson.Result, error) {
	f.finishReqs = append(f.finishReqs, req)
	return gjson.Result{}, nil
}

func (f *fakeChunkedAPI) GroupRichMedia(_ context.Context, _ string, media *dto.Media) (gjson.Result, error) {
	f.mergeMedia = media
	return gjson.Parse(`{"file_info":"fi_123"}`), nil
}

func (f *fakeChunkedAPI) GroupChat(_ context.Context, _ string, msg *dto.Message) (gjson.Result, error) {
	f.chatMsg = msg
	return gjson.Parse(`{"id":"msg_1"}`), nil
}

// TestSendAttachmentChunked_ParseParts 验证分片上传正确解析 upload_prepare 返回的
// parts 数组（真实响应中 index 从 1 开始），而不是文档之外的 presigned_urls。
func TestSendAttachmentChunked_ParseParts(t *testing.T) {
	const blockSize = 3 * 1024 * 1024 // 3MB 每片
	data := bytes.Repeat([]byte{0xAB}, blockSize*2)

	var mu sync.Mutex
	putChunks := map[string][]byte{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		putChunks[r.URL.Path] = body
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	prepareResp := gjson.Parse(fmt.Sprintf(
		`{"upload_id":"upload_1","block_size":"%d","parts":[`+
			`{"index":1,"presigned_url":%q,"block_size":"%d"},`+
			`{"index":2,"presigned_url":%q,"block_size":"%d"}],`+
			`"upload_config":{"concurrency":1,"retry_timeout":300,"retry_delay":1}}`,
		blockSize, srv.URL+"/p1", blockSize, srv.URL+"/p2", blockSize))

	fake := &fakeChunkedAPI{prepareResult: prepareResp}
	s := &qqSender{api: fake}

	res, err := s.sendAttachmentChunked(context.Background(), newTestChat(), platform.OutboundMessage{}, platform.Attachment{
		Kind: platform.AttachmentKindFile,
		Name: "a.bin",
		Data: data,
	})
	require.NoError(t, err)
	require.NotNil(t, res.Raw, "send result should be populated")

	// prepare 请求按官方文档发送字符串 file_size 与 md5/sha1 校验值
	require.NotNil(t, fake.prepareReq)
	assert.Equal(t, strconv.FormatInt(int64(len(data)), 10), fake.prepareReq.FileSize)
	assert.Equal(t, md5Sum(data), fake.prepareReq.FileMD5)
	assert.Equal(t, sha1Sum(data), fake.prepareReq.FileSHA1)

	// 每个分片都 PUT 到对应预签名 URL，并按数组顺序切片
	require.Len(t, putChunks, 2)
	assert.Equal(t, data[:blockSize], putChunks["/p1"])
	assert.Equal(t, data[blockSize:], putChunks["/p2"])

	// part_finish 使用服务端返回的 index（1 起），而非循环下标
	require.Len(t, fake.finishReqs, 2)
	assert.Equal(t, 1, fake.finishReqs[0].PartIndex)
	assert.Equal(t, 2, fake.finishReqs[1].PartIndex)
	assert.Equal(t, "upload_1", fake.finishReqs[0].UploadID)

	// 合并请求携带 upload_id
	require.NotNil(t, fake.mergeMedia)
	assert.Equal(t, "upload_1", fake.mergeMedia.UploadID)

	// 最终媒体消息携带 file_info
	require.NotNil(t, fake.chatMsg)
	require.NotNil(t, fake.chatMsg.Media)
	assert.Equal(t, "fi_123", fake.chatMsg.Media.FileInfo)
}

// fakeC2CMediaAPI 记录单聊媒体上传/发送，用于验证 msg_type=7 的正文保留逻辑。
type fakeC2CMediaAPI struct {
	openapi.OpenAPI
	chatMsg *dto.Message
}

func (f *fakeC2CMediaAPI) SingleRichMedia(_ context.Context, _ string, _ *dto.Media) (gjson.Result, error) {
	return gjson.Parse(`{"file_info":"fi_c2c"}`), nil
}

func (f *fakeC2CMediaAPI) SingleChat(_ context.Context, _ string, msg *dto.Message) (gjson.Result, error) {
	f.chatMsg = msg
	return gjson.Parse(`{"id":"msg_c2c"}`), nil
}

// TestSendTextImage_C2CKeepsContent 回归真机结论（2026-09，C2C）：msg_type=7 可同时
// 携带 media 与 content 渲染图文同一条消息，单聊发送不再清空 content。
func TestSendTextImage_C2CKeepsContent(t *testing.T) {
	fake := &fakeC2CMediaAPI{}
	s := NewSender(fake)

	_, err := s.Send(context.Background(), platform.SendRequest{
		Target: platform.ChatInfo{ID: "user_openid_001"},
		Message: platform.OutboundMessage{Segments: []platform.Segment{
			{Type: platform.SegmentText, Text: "图文同发正文"},
			{Type: platform.SegmentImage, Attachment: platform.Attachment{
				Kind: platform.AttachmentKindImage,
				URL:  "https://ex.com/a.png",
			}},
		}},
	})
	require.NoError(t, err)

	require.NotNil(t, fake.chatMsg, "应走到单聊发消息接口")
	assert.EqualValues(t, dto.MediaMessage, fake.chatMsg.Type)
	require.NotNil(t, fake.chatMsg.Media)
	assert.Equal(t, "fi_c2c", fake.chatMsg.Media.FileInfo)
	assert.Equal(t, "图文同发正文", fake.chatMsg.Content, "单聊媒体消息必须保留正文（真机验证图文混排）")
	assert.Nil(t, fake.chatMsg.Markdown, "正文已放入 content，不应残留 markdown 载荷")
}

// TestSendTextImage_GroupKeepsContent 群聊与单聊共用 sendMediaMessage：msg_type=7
// 携带 media 与 content 发送，正文同样必须保留（2026-09 C2C 真机验证图文混排，
// 群聊走同一路径）。
func TestSendTextImage_GroupKeepsContent(t *testing.T) {
	fake := &fakeChunkedAPI{}
	s := NewSender(fake)

	_, err := s.Send(context.Background(), platform.SendRequest{
		Target: platform.ChatInfo{ID: "group_openid_001", IsGroup: true},
		Message: platform.OutboundMessage{Segments: []platform.Segment{
			{Type: platform.SegmentText, Text: "群聊图文同发正文"},
			{Type: platform.SegmentImage, Attachment: platform.Attachment{
				Kind: platform.AttachmentKindImage,
				URL:  "https://ex.com/a.png",
			}},
		}},
	})
	require.NoError(t, err)

	require.NotNil(t, fake.chatMsg, "应走到群聊发消息接口")
	assert.EqualValues(t, dto.MediaMessage, fake.chatMsg.Type)
	require.NotNil(t, fake.chatMsg.Media)
	assert.Equal(t, "fi_123", fake.chatMsg.Media.FileInfo)
	assert.Equal(t, "群聊图文同发正文", fake.chatMsg.Content, "群聊媒体消息必须保留正文（与单聊同一 sendMediaMessage 路径）")
	assert.Nil(t, fake.chatMsg.Markdown, "正文已放入 content，不应残留 markdown 载荷")
}

// TestSendImageOnly_MediaMessageContent 覆盖纯图片消息的 content 兜底规则：
// 单聊省略 content（官方示例形态，纯图可发），群聊补空格（文档标注必填）。
func TestSendImageOnly_MediaMessageContent(t *testing.T) {
	t.Run("单聊省略content", func(t *testing.T) {
		fake := &fakeC2CMediaAPI{}
		s := NewSender(fake)
		_, err := s.Send(context.Background(), platform.SendRequest{
			Target: platform.ChatInfo{ID: "user_openid_001"},
			Message: platform.OutboundMessage{Attachments: []platform.Attachment{
				{Kind: platform.AttachmentKindImage, URL: "https://ex.com/a.png"},
			}},
		})
		require.NoError(t, err)
		require.NotNil(t, fake.chatMsg)
		assert.Empty(t, fake.chatMsg.Content, "单聊纯图片消息不携带 content")
	})

	t.Run("群聊空格兜底", func(t *testing.T) {
		fake := &fakeChunkedAPI{}
		s := NewSender(fake)
		_, err := s.Send(context.Background(), platform.SendRequest{
			Target: platform.ChatInfo{ID: "group_openid_001", IsGroup: true},
			Message: platform.OutboundMessage{Attachments: []platform.Attachment{
				{Kind: platform.AttachmentKindImage, URL: "https://ex.com/a.png"},
			}},
		})
		require.NoError(t, err)
		require.NotNil(t, fake.chatMsg)
		assert.Equal(t, " ", fake.chatMsg.Content, "群聊 content 必填，空正文用空格兜底")
	})
}

func TestSendTyping_BuildsInputNotify(t *testing.T) {
	msg := &dto.Message{}
	msg.Type = dto.InputNotifyMsg
	msg.InputNotify = &dto.InputNotify{InputType: 1, InputSecond: 30}
	assert.EqualValues(t, 6, msg.Type)
	assert.Equal(t, 1, msg.InputNotify.InputType)
	assert.Equal(t, 30, msg.InputNotify.InputSecond)
}

// TestNextMsgSeq_MultipleRepliesAllowed 验证同一 msg_id 可无限次回复：
// 被动回复次数/时长限制已移除（平台端校验，官方 SDK/插件同策略），
// msg_seq 仍按 msg_id 递增防重放。
func TestNextMsgSeq_MultipleRepliesAllowed(t *testing.T) {
	s := &qqSender{}
	// 同一 msg_id 连续多次回复：seq 递增，且不报错（无次数上限）
	for i := 1; i <= 20; i++ { // 远超旧上限 5 次
		seq := s.nextMsgSeq("msg_001")
		assert.Equal(t, uint64(i), seq, "seq should increment per reply (iteration %d)", i)
	}
	// 不同 msg_id 各自从 1 开始
	assert.Equal(t, uint64(1), s.nextMsgSeq("msg_002"))
	// 空 msg_id（主动消息）返回 0 不设置 seq
	assert.Equal(t, uint64(0), s.nextMsgSeq(""))
}

// TestNextMsgSeq_ExpiredEntryRecycled 验证过期条目被 sweep 回收后
// seq 从 1 重新开始（sweep 基准 60 分钟 > 平台最长有效期，正常不触发）。
func TestNextMsgSeq_ExpiredEntryRecycled(t *testing.T) {
	s := &qqSender{}
	s.nextMsgSeq("msg_x")
	// 直接把 createdAt 拨回 3 小时前，模拟过期
	v, _ := s.msgSeqMap.Load("msg_x")
	entry := v.(*msgSeqEntry)
	entry.createdAt.Store(time.Now().Add(-3 * time.Hour))
	// 重置惰性清理节流，强制触发 sweep
	s.lastSweep.Store(0)
	s.sweepExpired()
	// sweep 后条目被回收，重新计数
	assert.Equal(t, uint64(1), s.nextMsgSeq("msg_x"))
}

// fakeGroupMembersAPI 返回预设分页的群成员列表响应，并记录每页请求的 cursor。
type fakeGroupMembersAPI struct {
	openapi.OpenAPI
	pages   []gjson.Result
	calls   int
	cursors []string
}

func (f *fakeGroupMembersAPI) GetGroupMemberList(_ context.Context, _ string, cursor string) (gjson.Result, error) {
	if f.calls >= len(f.pages) {
		return gjson.Parse(`{"members":[]}`), nil
	}
	f.cursors = append(f.cursors, cursor)
	res := f.pages[f.calls]
	f.calls++
	return res, nil
}

// TestGetGroupMemberList_CursorPagination 验证群成员列表按 next_cursor 循环分页
// 拉取，并解析 username/member_role/joined_at 等新字段。
func TestGetGroupMemberList_CursorPagination(t *testing.T) {
	api := &fakeGroupMembersAPI{pages: []gjson.Result{
		gjson.Parse(`{"members":[{"member_openid":"u1","username":"甲","member_role":"member","joined_at":"2026-01-02T03:04:05+08:00"},{"member_openid":"u2","username":"乙","member_role":"admin","joined_at":"2026-02-02T03:04:05+08:00"}],"next_cursor":"cur_2"}`),
		gjson.Parse(`{"members":[{"member_openid":"u3","username":"丙","member_role":"owner","bot":true,"joined_at":"2026-03-02T03:04:05+08:00"}],"next_cursor":""}`),
	}}
	s := &qqSender{api: api}

	members, err := s.GetGroupMemberList(context.Background(), "gid_1")
	require.NoError(t, err)
	require.Len(t, members, 3)
	assert.Equal(t, "u1", members[0].UserID)
	assert.Equal(t, "甲", members[0].DisplayName)
	assert.Equal(t, platform.GroupRoleMember, members[0].GroupRole)
	assert.Equal(t, "乙", members[1].DisplayName)
	assert.Equal(t, platform.GroupRoleAdmin, members[1].GroupRole)
	assert.Equal(t, "u3", members[2].UserID)
	assert.Equal(t, "丙", members[2].DisplayName)
	assert.Equal(t, platform.GroupRoleOwner, members[2].GroupRole)
	assert.Equal(t, 2026, members[2].JoinedAt.Year())
	assert.Equal(t, time.March, members[2].JoinedAt.Month())
	assert.Equal(t, 2, api.calls, "应恰好请求两页")
	assert.Equal(t, []string{"", "cur_2"}, api.cursors, "首次请求 cursor 为空串，后续传上一页 next_cursor")
}

// TestGetGroupMemberList_APIError 验证接口错误向上传播。
func TestGetGroupMemberList_APIError(t *testing.T) {
	s := &qqSender{api: nil}
	_, err := s.GetGroupMemberList(context.Background(), "gid_1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "openAPI client is nil")
}

// fakeGroupMemberOpsAPI 覆盖单成员查询与批量移除，供 sender 层测试断言。
type fakeGroupMemberOpsAPI struct {
	openapi.OpenAPI
	memberResult gjson.Result
	memberErr    error
	kickedGroup  string
	kickedReq    *dto.BatchRemoveGroupMembersRequest
}

func (f *fakeGroupMemberOpsAPI) GetGroupMember(_ context.Context, _, _ string) (gjson.Result, error) {
	return f.memberResult, f.memberErr
}

func (f *fakeGroupMemberOpsAPI) BatchRemoveGroupMembers(_ context.Context, groupID string, req *dto.BatchRemoveGroupMembersRequest) (gjson.Result, error) {
	f.kickedGroup = groupID
	f.kickedReq = req
	return gjson.Parse(`{"remove_members_result":"success"}`), nil
}

// TestGetGroupMember 验证单成员查询映射与错误传播。
func TestGetGroupMember(t *testing.T) {
	api := &fakeGroupMemberOpsAPI{
		memberResult: gjson.Parse(`{"member_openid":"u9","username":"管理员","member_role":"admin","joined_at":"2025-08-20T09:15:00+08:00"}`),
	}
	s := &qqSender{api: api}

	member, err := s.GetGroupMember(context.Background(), "gid_1", "u9")
	require.NoError(t, err)
	assert.Equal(t, "u9", member.UserID)
	assert.Equal(t, "管理员", member.DisplayName)
	assert.Equal(t, platform.GroupRoleAdmin, member.GroupRole)
	assert.Equal(t, 2025, member.JoinedAt.Year())
}

func TestGetGroupMember_APIError(t *testing.T) {
	s := &qqSender{api: nil}
	_, err := s.GetGroupMember(context.Background(), "gid_1", "u1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "openAPI client is nil")
}

// TestKickMember 验证 KickMember 走批量移除接口，permanent=true 时同时拉黑。
func TestKickMember(t *testing.T) {
	api := &fakeGroupMemberOpsAPI{}
	s := &qqSender{api: api}

	require.NoError(t, s.KickMember(context.Background(), "gid_1", "u1", false))
	assert.Equal(t, "gid_1", api.kickedGroup)
	require.NotNil(t, api.kickedReq)
	assert.Equal(t, []string{"u1"}, api.kickedReq.MemberOpenIDs)
	assert.False(t, api.kickedReq.AddToMemberBlacklist)

	require.NoError(t, s.KickMember(context.Background(), "gid_1", "u2", true))
	require.NotNil(t, api.kickedReq)
	assert.Equal(t, []string{"u2"}, api.kickedReq.MemberOpenIDs)
	assert.True(t, api.kickedReq.AddToMemberBlacklist, "permanent=true 应同时加入群黑名单")
}

func TestKickMember_APIError(t *testing.T) {
	s := &qqSender{api: nil}
	err := s.KickMember(context.Background(), "gid_1", "u1", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "openAPI client is nil")
}

// TestBuildDTOMessage_ActionButtonAndPromptKeyboard 验证操作按钮与提示键盘透传。
func TestBuildDTOMessage_ActionButtonAndPromptKeyboard(t *testing.T) {
	msg := platform.TextMessage("hello")
	msg = ApplyExtra(msg, MessageExtra{
		ActionButton: &dto.ActionButton{TemplateID: "1", CallbackData: "stop", StopGenerate: true},
		PromptKeyboard: dto.NewPromptKeyboard(
			[]dto.PromptKeyboardButton{{
				RenderData: dto.PromptKeyboardRenderData{Label: "帮助", Style: 2},
				Action:     dto.PromptKeyboardAction{Type: 2},
			}},
		),
	})
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, newTestChat())

	require.NotNil(t, dtoMsg.ActionButton)
	assert.Equal(t, "stop", dtoMsg.ActionButton.CallbackData)
	assert.True(t, dtoMsg.ActionButton.StopGenerate)

	require.NotNil(t, dtoMsg.PromptKeyboard)
	require.Len(t, dtoMsg.PromptKeyboard.Keyboard.Content.Rows, 1)
	require.Len(t, dtoMsg.PromptKeyboard.Keyboard.Content.Rows[0].Buttons, 1)
	btn := dtoMsg.PromptKeyboard.Keyboard.Content.Rows[0].Buttons[0]
	assert.Equal(t, "帮助", btn.RenderData.Label)
	assert.Equal(t, 2, btn.Action.Type)
}
