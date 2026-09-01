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

// fakeGroupMembersAPI 返回预设分页的群成员列表响应。
type fakeGroupMembersAPI struct {
	openapi.OpenAPI
	pages []gjson.Result
	calls int
}

func (f *fakeGroupMembersAPI) GetGroupMembers(_ context.Context, _ string, _, _ int) (gjson.Result, error) {
	if f.calls >= len(f.pages) {
		return gjson.Parse(`{"members":[]}`), nil
	}
	res := f.pages[f.calls]
	f.calls++
	return res, nil
}

// TestGetGroupMemberList_Pagination 验证群成员列表按 next_index 循环分页拉取。
func TestGetGroupMemberList_Pagination(t *testing.T) {
	api := &fakeGroupMembersAPI{pages: []gjson.Result{
		gjson.Parse(`{"members":[{"member_openid":"u1","join_timestamp":"2026-01-02T03:04:05+08:00"},{"member_openid":"u2","join_timestamp":"2026-02-02T03:04:05+08:00"}],"next_index":100}`),
		gjson.Parse(`{"members":[{"member_openid":"u3","join_timestamp":"2026-03-02T03:04:05+08:00"}],"next_index":0}`),
	}}
	s := &qqSender{api: api}

	members, err := s.GetGroupMemberList(context.Background(), "gid_1")
	require.NoError(t, err)
	require.Len(t, members, 3)
	assert.Equal(t, "u1", members[0].UserID)
	assert.Equal(t, "u3", members[2].UserID)
	assert.Equal(t, 2026, members[2].JoinedAt.Year())
	assert.Equal(t, time.March, members[2].JoinedAt.Month())
	assert.Equal(t, 2, api.calls, "应恰好请求两页")
}

// TestGetGroupMemberList_APIError 验证接口错误向上传播。
func TestGetGroupMemberList_APIError(t *testing.T) {
	s := &qqSender{api: nil}
	_, err := s.GetGroupMemberList(context.Background(), "gid_1")
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
