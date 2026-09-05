package qq_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fwdFlatRecord 是 QQ 官方 API 合并转发（message_type=102）的扁平渲染样本
// （2026-09 报文核验，节选自真实 C2C webhook 报文）。
const fwdFlatRecord = `[月莫法师和蕾米莉亚的聊天记录]
=== 消息 1 ===
[消息内容] /update now
[发送者] 月莫法师

=== 消息 2 ===
[消息内容] 🚀 更新完成，机器人即将重启（约 30 秒）...
[发送者] 蕾米莉亚

=== 消息 3 ===
[发送者] 蕾米莉亚
[附件1] 类型:图片 文件名:AA822D02B16BB0D5A4991A98D591D069.png 尺寸:600x503 大小:30.8KB URL:https://multimedia.nt.qq.com.cn/download?appid=1406&fileid=EhTTrSez3zCNzYQ&rkey=CAMSOLgthq-6lGU&spec=0

=== 消息 4 ===
[消息内容] /mc 103.91.208.156:25565
[发送者] 月莫法师
`

func extractForwardRecord(t *testing.T, seg platform.Segment) *qq.ForwardRecord {
	t.Helper()
	rec, ok := seg.Extra[qq.ExtraKeyForwardNodes].(*qq.ForwardRecord)
	require.True(t, ok, "forward 段应携带 *ForwardRecord，got %T", seg.Extra[qq.ExtraKeyForwardNodes])
	return rec
}

// TestNewEvent_C2CMessageCreate_Forward102 覆盖真实扁平渲染样本的完整解析：
// 结构化 forward 段、子消息文本/附件还原、Content 为空（防记录内命令误触发）。
func TestNewEvent_C2CMessageCreate_Forward102(t *testing.T) {
	payload := makePayload(dto.C2CMessageCreate, map[string]any{
		"id":           "ROBOT1.0_mnqI5",
		"content":      fwdFlatRecord,
		"timestamp":    "2026-09-05T18:16:13+08:00",
		"message_type": 102,
		"author": map[string]any{
			"id":           "0A81EAA929FEE62EB9CB3A40EFF195CC",
			"user_openid":  "0A81EAA929FEE62EB9CB3A40EFF195CC",
			"union_openid": "0A81EAA929FEE62EB9CB3A40EFF195CC",
		},
		"message_scene": map[string]any{
			"source": "default",
			"ext":    []string{"msg_idx=REFIDX_fDcjhHf18VQHp1S3lPcHqPqWx6ikzc8g7eW3OyP54VT4RSflXAViLxWUWMi4Q6HRdumHkCxLO0+/5849nAfemDz6oVAecD1Dx8/ZGM9xZAp7VmKBpdDaNddc3rHzkcgW"},
		},
	})

	event := qq.NewEvent(payload)

	segs := event.Segments()
	require.Len(t, segs, 1, "102 消息应只产生一个 forward 段")
	require.Equal(t, platform.SegmentForward, segs[0].Type)

	rec := extractForwardRecord(t, segs[0])
	assert.Equal(t, "月莫法师和蕾米莉亚的聊天记录", rec.Title)
	require.Len(t, rec.Nodes, 4)

	// 消息 1：文本
	assert.Equal(t, "月莫法师", rec.Nodes[0].Sender.DisplayName)
	require.Len(t, rec.Nodes[0].Segments, 1)
	assert.Equal(t, platform.SegmentText, rec.Nodes[0].Segments[0].Type)
	assert.Equal(t, "/update now", rec.Nodes[0].Segments[0].Text)

	// 消息 2：多行安全（单行文本）
	assert.Equal(t, "🚀 更新完成，机器人即将重启（约 30 秒）...", rec.Nodes[1].Segments[0].Text)

	// 消息 3：仅图片附件（[发送者] 在前，[附件1] 在后）
	assert.Equal(t, "蕾米莉亚", rec.Nodes[2].Sender.DisplayName)
	require.Len(t, rec.Nodes[2].Segments, 1)
	img := rec.Nodes[2].Segments[0]
	assert.Equal(t, platform.SegmentImage, img.Type)
	assert.Equal(t, "AA822D02B16BB0D5A4991A98D591D069.png", img.Attachment.Name)
	assert.Equal(t, 600, img.Attachment.Width)
	assert.Equal(t, 503, img.Attachment.Height)
	assert.Equal(t, 31539, img.Attachment.Size) // 30.8KB ≈ 30.8*1024
	assert.Equal(t, platform.AttachmentKindImage, img.Attachment.Kind)
	// URL 还原为真实 &（\u0026 是日志转义）
	assert.Contains(t, img.Attachment.URL, "appid=1406&fileid=EhTTrSez3zCNzYQ&rkey=CAMSOLgthq-6lGU&spec=0")

	// 消息 4：记录内的命令文本，不进入 Content
	assert.Equal(t, "/mc 103.91.208.156:25565", rec.Nodes[3].Segments[0].Text)

	// 降级摘要键
	assert.Equal(t, "月莫法师和蕾米莉亚的聊天记录", segs[0].Extra[platform.SegmentExtraTitle])
	summary, _ := segs[0].Extra[platform.SegmentExtraSummary].(string)
	assert.Contains(t, summary, "月莫法师: /update now")
	assert.Contains(t, summary, "… 共 4 条消息")

	// Content 为空 → 记录内命令不会被插件匹配；顶层附件列表为空
	assert.Equal(t, "", platform.Content(event))
	assert.Empty(t, platform.Attachments(event))
}

// TestNewEvent_GroupMessageCreate_Forward102 群聊方向同构解析，且消息事件
// 自身字段（发送者/会话/token）不受影响。
func TestNewEvent_GroupMessageCreate_Forward102(t *testing.T) {
	payload := makePayload(dto.GroupMessageCreate, map[string]any{
		"id":           "msg_fwd_group",
		"content":      fwdFlatRecord,
		"group_openid": "group_001",
		"message_type": 102,
		"author": map[string]any{
			"member_openid": "mem001",
			"username":      "月莫法师",
			"member_role":   "member",
		},
		"timestamp": "2026-09-05T18:16:13+08:00",
	})

	event := qq.NewEvent(payload)

	segs := event.Segments()
	require.Len(t, segs, 1)
	require.Equal(t, platform.SegmentForward, segs[0].Type)
	rec := extractForwardRecord(t, segs[0])
	require.Len(t, rec.Nodes, 4)

	assert.Equal(t, "mem001", event.Sender().ID)
	assert.Equal(t, "group_001", event.Chat().ID)
	assert.True(t, event.Chat().IsGroup)
	assert.Equal(t, "msg_fwd_group", event.Chat().Tokens[qq.TokenMsgID])
}

// TestNewEvent_Forward102_PlainTextFallback 解析失败（非记录格式）时退化为
// 普通文本，保证不丢数据。
func TestNewEvent_Forward102_PlainTextFallback(t *testing.T) {
	payload := makePayload(dto.C2CMessageCreate, map[string]any{
		"id":           "msg_fwd_plain",
		"content":      "这不是聊天记录，只是普通文本",
		"message_type": 102,
		"author":       map[string]any{"user_openid": "openid_alice"},
	})

	event := qq.NewEvent(payload)

	segs := event.Segments()
	require.Len(t, segs, 1)
	assert.Equal(t, platform.SegmentText, segs[0].Type)
	assert.Equal(t, "这不是聊天记录，只是普通文本", platform.Content(event))
}

// TestNewEvent_GroupMessageCreate_QuoteForward102 引用聊天记录（103 引用 102）：
// 被引用记录解析为结构化 *ForwardRecord 挂 reply 段 Extra，回复正文正常解析。
func TestNewEvent_GroupMessageCreate_QuoteForward102(t *testing.T) {
	payload := makePayload(dto.GroupMessageCreate, map[string]any{
		"id":           "msg_quote_fwd",
		"content":      "看看这个",
		"group_openid": "group_001",
		"message_type": 103,
		"author":       map[string]any{"member_openid": "mem001", "username": "Alice", "member_role": "member"},
		"timestamp":    "2026-09-05T18:20:00+08:00",
		"message_scene": map[string]any{
			"source": "default",
			"ext":    []string{"ref_msg_idx=REFIDX_fwdrec", "msg_idx=REFIDX_x"},
		},
		"msg_elements": []any{
			map[string]any{
				"msg_idx":      "REFIDX_fwdrec",
				"message_type": 102,
				"content":      fwdFlatRecord,
			},
		},
	})

	event := qq.NewEvent(payload)

	segs := event.Segments()
	require.Len(t, segs, 2, "reply 段 + 正文段")
	require.Equal(t, platform.SegmentReply, segs[0].Type)
	assert.Equal(t, "REFIDX_fwdrec", segs[0].ReplyToID)

	rec, ok := segs[0].Extra[qq.ExtraKeyQuotedForward].(*qq.ForwardRecord)
	require.True(t, ok, "reply 段应携带 *ForwardRecord，got %T", segs[0].Extra[qq.ExtraKeyQuotedForward])
	assert.Equal(t, "月莫法师和蕾米莉亚的聊天记录", rec.Title)
	require.Len(t, rec.Nodes, 4)

	assert.Equal(t, "看看这个", platform.Content(event))
}

// TestNewEvent_GroupMessageCreate_QuoteForward102_EmptyBody 引用聊天记录且
// 回复正文为空：记录不再以扁平文本兜底进正文（防记录内命令误触发）。
func TestNewEvent_GroupMessageCreate_QuoteForward102_EmptyBody(t *testing.T) {
	payload := makePayload(dto.GroupMessageCreate, map[string]any{
		"id":           "msg_quote_fwd_empty",
		"content":      " ",
		"group_openid": "group_001",
		"message_type": 103,
		"author":       map[string]any{"member_openid": "mem001", "username": "Alice", "member_role": "member"},
		"message_scene": map[string]any{
			"source": "default",
			"ext":    []string{"ref_msg_idx=REFIDX_fwdrec"},
		},
		"msg_elements": []any{
			map[string]any{"message_type": 102, "content": fwdFlatRecord},
		},
	})

	event := qq.NewEvent(payload)

	segs := event.Segments()
	require.Len(t, segs, 1, "只有 reply 段，扁平记录不进正文")
	require.Equal(t, platform.SegmentReply, segs[0].Type)
	rec, ok := segs[0].Extra[qq.ExtraKeyQuotedForward].(*qq.ForwardRecord)
	require.True(t, ok)
	require.Len(t, rec.Nodes, 4)
	assert.Equal(t, "", platform.Content(event))
}

// TestNewEvent_GroupMessageCreate_QuoteForward102_ParallelFallback 被引用记录
// 不在 msg_elements 时从 parallel_message.msg_nodes 兜底解析。
func TestNewEvent_GroupMessageCreate_QuoteForward102_ParallelFallback(t *testing.T) {
	payload := makePayload(dto.GroupMessageCreate, map[string]any{
		"id":           "msg_quote_fwd_pm",
		"content":      " ",
		"group_openid": "group_001",
		"message_type": 103,
		"author":       map[string]any{"member_openid": "mem001", "username": "Alice", "member_role": "member"},
		"message_scene": map[string]any{
			"source": "default",
			"ext":    []string{"ref_msg_idx=REFIDX_fwdrec"},
		},
		"msg_elements": []any{
			map[string]any{"message_type": 102, "content": ""},
		},
		"parallel_message": map[string]any{
			"msg_nodes": []any{
				map[string]any{"message_type": 102, "content": fwdFlatRecord},
			},
		},
	})

	event := qq.NewEvent(payload)

	segs := event.Segments()
	require.NotEmpty(t, segs)
	require.Equal(t, platform.SegmentReply, segs[0].Type)
	rec, ok := segs[0].Extra[qq.ExtraKeyQuotedForward].(*qq.ForwardRecord)
	require.True(t, ok, "应从 parallel_message.msg_nodes 兜底解析")
	require.Len(t, rec.Nodes, 4)
}

// TestNewEvent_GroupMessageCreate_QuoteForward102_UnmarkedElement 被引用元素
// 未标 message_type=102 时，内容满足严格记录格式（有标题行）同样接受。
func TestNewEvent_GroupMessageCreate_QuoteForward102_UnmarkedElement(t *testing.T) {
	payload := makePayload(dto.GroupMessageCreate, map[string]any{
		"id":           "msg_quote_fwd_unmarked",
		"content":      " ",
		"group_openid": "group_001",
		"message_type": 103,
		"author":       map[string]any{"member_openid": "mem001", "username": "Alice", "member_role": "member"},
		"message_scene": map[string]any{
			"source": "default",
			"ext":    []string{"ref_msg_idx=REFIDX_fwdrec"},
		},
		"msg_elements": []any{
			map[string]any{"content": fwdFlatRecord},
		},
	})

	event := qq.NewEvent(payload)

	segs := event.Segments()
	require.NotEmpty(t, segs)
	require.Equal(t, platform.SegmentReply, segs[0].Type)
	_, ok := segs[0].Extra[qq.ExtraKeyQuotedForward].(*qq.ForwardRecord)
	require.True(t, ok, "无标记但格式严格的记录应被解析")
}

// quotedRecordNested 是被引用聊天记录（103 引用 102）的真实渲染样本
// （2026-09 报文核验，C2C webhook）：形态 B——无标题行，记录包在单个
// === 消息 1 === 条目内，[关联消息] 以 --- 第N条 --- 列出实际消息；
// 第3条为嵌套合并转发（缩进 +4），第4/8条为嵌套引用，第5/9条为多行正文。
const quotedRecordNested = `=== 消息 1 ===
[消息内容] [月莫法师和蕾米莉亚的聊天记录]
[消息类型] 引用消息
[关联消息]
--- 第1条 ---
    [消息内容] /mc 106.53.117.73:25565
    [发送者] 月莫法师
--- 第2条 ---
    [发送者] 蕾米莉亚
    [附件1] 类型:图片 文件名:299B86AD474E9ACB7B503B855710B831.png 尺寸:600x503 大小:28.1KB URL:https://multimedia.nt.qq.com.cn/download?appid=1406&fileid=EhSxC3m3HNMffsErgFuHOXi6xCiA_Bj64AEg_goo7_e8_aPXlgMyBHByb2RQgLsvWhDxsp1ZpQN6ZYToZ5cOpWZbegKw5YIBAmd6&rkey=CAMSOLgthq-6lGU_o8DFnDevpNsa5VYPaMAg27HKAMXzR3X10ZjAJwKJgbH8Ybgydf1HR2zCoX18Knr_&spec=0
--- 第3条 ---
    [消息内容] [月莫法师和蕾米莉亚的聊天记录]
    [发送者] 月莫法师
    [消息类型] 合并转发消息
    [关联消息]
    --- 第1条 ---
        [消息内容] /update now
        [发送者] 月莫法师
    --- 第2条 ---
        [消息内容] 🔍 正在检查更新...
        [发送者] 蕾米莉亚
    --- 第3条 ---
        [消息内容] ⬇️ 正在下载 v1.53.0 ...
        [发送者] 蕾米莉亚
    --- 第4条 ---
        [消息内容] ✅ 下载完成，sha256 校验通过
        [发送者] 蕾米莉亚
    --- 第5条 ---
        [消息内容] 🔄 正在替换二进制...
        [发送者] 蕾米莉亚
    --- 第6条 ---
        [消息内容] 🚀 更新完成，机器人即将重启（约 30 秒）...
        [发送者] 蕾米莉亚
    --- 第7条 ---
        [消息内容] /mc 103.91.208.156:25565
        [发送者] 月莫法师
    --- 第8条 ---
        [发送者] 蕾米莉亚
        [附件1] 类型:图片 文件名:AA822D02B16BB0D5A4991A98D591D069.png 尺寸:600x503 大小:30.8KB URL:https://multimedia.nt.qq.com.cn/download?appid=1406&fileid=EhTTrSez3zCNzYQ_Cz-ntFjKPw02UBjN9gEg_goo3rTi5ZrXlgMyBHByb2RQgLsvWhAs148yQUhMwRGyMMpRSFzlegK5U4IBAmd6&rkey=CAESOE4_cASDm1t1spInyorQArkPxBAOksLEcd6jDYmzWZLi7hSehm8Pxz-PU90RzDMWjFvwN5aIoFM1&spec=0
    --- 第9条 ---
        [消息内容] /mc 106.53.117.73:25565
        [发送者] 月莫法师
    --- 第10条 ---
        [发送者] 蕾米莉亚
        [附件1] 类型:图片 文件名:299B86AD474E9ACB7B503B855710B831.png 尺寸:600x503 大小:28.1KB URL:https://multimedia.nt.qq.com.cn/download?appid=1406&fileid=EhSxC3m3HNMffsErgFuHOXi6xCiA_Bj64AEg_goo_r_i5ZrXlgMyBHByb2RQgLsvWhBXx_UsVXJUtkvY_nXi4FgyegKjZ4IBAmd6&rkey=CAISONPsN0nSR8aLnLxMKGHsWEsCqpj7pcQFm5m3gVN0IX7E-GeIy-DjI9NzBcmRmRz2eoXqqEjz_uaI&spec=0
--- 第4条 ---
    [消息内容] 你能看到这是什么吗
    [发送者] 月莫法师
    [关联消息]
    --- 第1条 ---
        [消息内容] [聊天记录]
        [消息类型] 引用消息
--- 第5条 ---
    [消息内容] 抱歉呢，亲爱的客人。我这边并没有收到任何图片——你所发送的似乎只是空白的消息记录，没有附带图片或可读取的内容。
    如果你是想让我辨认某张图片，可以先将图片上传或提供公开可访问的图片链接，我就能替你仔细看看了。若是指我之前与你聊过的某段内容，也烦请说得更具体些，好让我回忆起来。
    那么，你究竟想让蕾米莉亚看些什么呢？🌟
    [发送者] 蕾米莉亚
--- 第6条 ---
    [消息内容] /ai reset
    [发送者] 月莫法师
--- 第7条 ---
    [消息内容] ✅ 对话历史已清空，开始全新的对话吧！
    [发送者] 蕾米莉亚
--- 第8条 ---
    [消息内容] 能读到这份转发的聊天记录消息吗
    [发送者] 月莫法师
    [关联消息]
    --- 第1条 ---
        [消息内容] [聊天记录]
        [消息类型] 引用消息
--- 第9条 ---
    [消息内容] 啊啦，我仔细看了你转发的这份"聊天记录"，不过那里只有"[聊天记录]"这个占位符，内容并没有真正传达到我这边呢。
    想必是复制粘贴时把具体文字漏掉了，或者转发的内容是以图片/附件形式发送的。能否把具体的文字内容粘贴给我？或者告诉我这张图片的链接，我也可以试着查看。
    作为红魔馆的大小姐，我很乐意听听你想让我读的东西。✨
    [发送者] 蕾米莉亚
--- 第10条 ---
    [消息内容] /ai reset
    [发送者] 月莫法师
--- 第11条 ---
    [消息内容] ✅ 对话历史已清空，开始全新的对话吧！
    [发送者] 蕾米莉亚
`

// TestNewEvent_C2CMessageCreate_QuoteForwardNested 覆盖真实报文（2026-09）：
// 103 引用 102、被引用记录含嵌套转发与嵌套引用。msg_elements[0] 无
// message_type 字段，须靠内容形态识别；包裹结构（形态 B）自动解包。
func TestNewEvent_C2CMessageCreate_QuoteForwardNested(t *testing.T) {
	payload := makePayload(dto.C2CMessageCreate, map[string]any{
		"id":           "ROBOT1.0_mnqI5_mgnoCKX9",
		"content":      "能读到这个聊天记录吗？",
		"timestamp":    "2026-09-05T18:57:52+08:00",
		"message_type": 103,
		"author": map[string]any{
			"id":           "0A81EAA929FEE62EB9CB3A40EFF195CC",
			"user_openid":  "0A81EAA929FEE62EB9CB3A40EFF195CC",
			"union_openid": "0A81EAA929FEE62EB9CB3A40EFF195CC",
		},
		"message_scene": map[string]any{
			"source": "default",
			"ext": []string{
				"msg_idx=REFIDX_voni2t0sgKQoYlTgZcome/qWx6ikzc8g7eW3OyP54VT4RSflXAViLxWUWMi4Q6HRdumHkCxLO0+/5849nAfemDz6oVAecD1Dx8/ZGM9xZAp7VmKBpdDaNddc3rHzkcgW",
				"ref_msg_idx=TMP_93b5da4d-84f7-4c6c-823e-83430c0d2c99",
			},
		},
		"parallel_message": map[string]any{
			"msg_nodes": []any{
				map[string]any{"message_type": 0, "content": "[聊天记录]"},
			},
		},
		"msg_elements": []any{
			map[string]any{"content": quotedRecordNested},
		},
	})

	event := qq.NewEvent(payload)

	segs := event.Segments()
	require.Len(t, segs, 2, "reply 段 + 正文段")
	require.Equal(t, platform.SegmentReply, segs[0].Type)
	assert.Equal(t, "TMP_93b5da4d-84f7-4c6c-823e-83430c0d2c99", segs[0].ReplyToID)

	rec, ok := segs[0].Extra[qq.ExtraKeyQuotedForward].(*qq.ForwardRecord)
	require.True(t, ok, "reply 段应携带 *ForwardRecord，got %T", segs[0].Extra[qq.ExtraKeyQuotedForward])

	// 形态 B 解包：Title 来自包裹条目的 [消息内容]，Nodes 为 [关联消息] 列表
	assert.Equal(t, "月莫法师和蕾米莉亚的聊天记录", rec.Title)
	require.Len(t, rec.Nodes, 11)

	// 第1条：文本
	assert.Equal(t, "月莫法师", rec.Nodes[0].Sender.DisplayName)
	require.Len(t, rec.Nodes[0].Segments, 1)
	assert.Equal(t, "/mc 106.53.117.73:25565", rec.Nodes[0].Segments[0].Text)

	// 第2条：仅图片（[发送者] 在前，[附件1] 在后，无 [消息内容]）
	require.Len(t, rec.Nodes[1].Segments, 1)
	img := rec.Nodes[1].Segments[0]
	assert.Equal(t, platform.SegmentImage, img.Type)
	assert.Equal(t, "299B86AD474E9ACB7B503B855710B831.png", img.Attachment.Name)
	assert.Equal(t, 28774, img.Attachment.Size) // 28.1KB
	assert.Contains(t, img.Attachment.URL, "fileid=EhSxC3m3HNMffsErgFuHOXi6xCiA_Bj64AEg_goo7_e8")
	assert.Contains(t, img.Attachment.URL, "rkey=CAMSOLgthq-6lGU_o8DFnDevpNsa5VYPaMAg27HKAMXzR3X10ZjAJwKJgbH8Ybgydf1HR2zCoX18Knr_&spec=0")

	// 第3条：嵌套合并转发（[消息类型] 合并转发消息 + [关联消息] 10 条）
	nested := rec.Nodes[2]
	assert.Equal(t, qq.ForwardKindRecord, nested.Kind)
	require.Len(t, nested.Segments, 1)
	assert.Equal(t, "[月莫法师和蕾米莉亚的聊天记录]", nested.Segments[0].Text)
	require.Len(t, nested.Related, 10)
	assert.Equal(t, "/update now", nested.Related[0].Segments[0].Text)
	nestedImg := nested.Related[7].Segments[0]
	assert.Equal(t, platform.SegmentImage, nestedImg.Type)
	assert.Equal(t, "AA822D02B16BB0D5A4991A98D591D069.png", nestedImg.Attachment.Name)
	assert.Equal(t, 31539, nestedImg.Attachment.Size) // 30.8KB
	assert.Contains(t, nestedImg.Attachment.URL, "rkey=CAESOE4_cASDm1t1spInyorQArkPxBAOksLEcd6jDYmzWZLi7hSehm8Pxz-PU90RzDMWjFvwN5aIoFM1&spec=0")

	// 第4条：嵌套引用（[关联消息] 仅 1 条，内容为占位符；
	// 实测该条目自身无 [消息类型] 字段，类型标记只在子条目上）
	quote := rec.Nodes[3]
	assert.Equal(t, platform.ForwardNodeKind(""), quote.Kind)
	require.Len(t, quote.Related, 1)
	assert.Equal(t, "[聊天记录]", quote.Related[0].Segments[0].Text)
	assert.Equal(t, qq.ForwardKindQuote, quote.Related[0].Kind)

	// 第5条：多行正文完整保留
	multi := rec.Nodes[4].Segments[0].Text
	assert.Contains(t, multi, "抱歉呢，亲爱的客人。")
	assert.Contains(t, multi, "如果你是想让我辨认某张图片")
	assert.Contains(t, multi, "那么，你究竟想让蕾米莉亚看些什么呢？🌟")

	// 第9条：多行正文 + 引号文本
	assert.Contains(t, rec.Nodes[8].Segments[0].Text, `那里只有"[聊天记录]"这个占位符`)

	// 末条
	assert.Equal(t, "✅ 对话历史已清空，开始全新的对话吧！", rec.Nodes[10].Segments[0].Text)

	// 回复正文不受记录影响
	assert.Equal(t, "能读到这个聊天记录吗？", platform.Content(event))
}

// TestNewEvent_Forward102_QuoteInsideBlock 直发 102 的块内嵌套引用：
// [关联消息] 子条目之后，同缩进的 === 块应作为兄弟块继续，不被吞入子树。
func TestNewEvent_Forward102_QuoteInsideBlock(t *testing.T) {
	content := `[记录标题]
=== 消息 1 ===
[消息内容] hello
[发送者] A

=== 消息 2 ===
[消息类型] 引用消息
[关联消息]
--- 第1条 ---
    [消息内容] 被引用内容
    [发送者] B

=== 消息 3 ===
[消息内容] world
[发送者] C
`
	payload := makePayload(dto.GroupMessageCreate, map[string]any{
		"id":           "msg_fwd_quote_inside",
		"content":      content,
		"group_openid": "group_001",
		"message_type": 102,
		"author":       map[string]any{"member_openid": "mem001", "username": "Alice", "member_role": "member"},
	})

	event := qq.NewEvent(payload)

	segs := event.Segments()
	require.Len(t, segs, 1)
	require.Equal(t, platform.SegmentForward, segs[0].Type)
	rec, ok := segs[0].Extra[qq.ExtraKeyForwardNodes].(*qq.ForwardRecord)
	require.True(t, ok)
	require.Len(t, rec.Nodes, 3, "同缩进的 === 块不应被 [关联消息] 子树吞并")

	assert.Equal(t, platform.ForwardNodeKind(""), rec.Nodes[0].Kind)
	assert.Equal(t, qq.ForwardKindQuote, rec.Nodes[1].Kind)
	require.Len(t, rec.Nodes[1].Related, 1)
	assert.Equal(t, "被引用内容", rec.Nodes[1].Related[0].Segments[0].Text)
	assert.Equal(t, "world", rec.Nodes[2].Segments[0].Text)
	assert.Equal(t, "C", rec.Nodes[2].Sender.DisplayName)
}
