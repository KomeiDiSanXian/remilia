// attachments_test.go — 历史消息只存引用，请求构建时按引用补水。
//
// 这三条契约决定了"会话内存与历史里有多少图无关"是否成立：
//   - 缓存命中时补水零 I/O；
//   - 缓存失效后靠 AttachmentID 从本机附件存储取回（重启恢复的关键）；
//   - 引用彻底失效时降级为文本占位，而不是悄悄丢掉片段。
package ai

import (
	"context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInboundImageReachesCurrentTurnRequest 覆盖"刚入站就带图"的完整链路：
//
//	buildUserMessage —— 下载后字节进缓存，消息里只放引用
//	AppendMessage    —— 历史只存引用
//	PrepareRequestMessages —— 当前轮附件始终保留
//	hydrateAttachmentParts —— 命中缓存，把字节装回请求副本
//
// 这是"图片不进会话"最容易出问题的场景（当前轮自己把自己饿死），
// 用真实 buildUserMessage + 真实请求管线钉住。
func TestInboundImageReachesCurrentTurnRequest(t *testing.T) {
	const url = "https://example.invalid/inbound.png"

	evt := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "这是什么",
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1", DisplayName: "小明"}),
		platform.WithSyntheticAttachments(platform.Attachment{
			Kind: platform.AttachmentKindImage, URL: url, MimeType: "image/png",
		}),
	)
	ctx := eventctx.NewContextFromEvent(evt, nil)

	sess := &session.Session{ID: "g1:u1"}
	// 模拟入站下载已完成（downloadAttachment 的真实副作用：字节写入会话缓存）。
	sess.SetCachedContent(url, []byte("inbound-bytes"), "image/png", "")

	p := &Plugin{cfg: &config.Config{VisionEnabled: true, MaxImagesPerMessage: 4, ImageCacheMaxMB: 64}}
	p.sm = session.NewSessionManager(16, 20, time.Hour, nil)

	msg := p.buildUserMessage(ctx, "这是什么", sess)
	require.Len(t, msg.ContentParts, 2, "text + image reference")
	require.Empty(t, msg.ContentParts[1].Data, "inbound message must carry a reference, not bytes")
	p.sm.AppendMessage(sess, msg)

	// 请求构建：当前轮裁剪保留 → 按引用补水。
	msgs, _, _ := runtime.PrepareRequestMessages(sess.SnapshotMessages(), runtime.Retention{
		MaxTurns: 5, Window: 10 * time.Minute, MaxPerRequest: 8,
	})
	filled, unavailable := p.hydrateAttachmentParts(context.Background(), msgs, sess)

	require.Equal(t, 1, filled, "current turn image must be hydrated from the session cache")
	require.Zero(t, unavailable)

	var got []byte
	for _, part := range msgs[0].ContentParts {
		if part.Type == protocol.ContentPartImage {
			got = part.Data
		}
	}
	assert.Equal(t, "inbound-bytes", string(got), "request copy must carry the bytes")
	assert.Empty(t, sess.SnapshotMessages()[0].ContentParts[1].Data, "session history stays byte-free")
}

func TestHydrateAttachmentPartsFromCache(t *testing.T) {
	const url = "https://example.invalid/a.png"
	stored := protocol.Message{
		Role: protocol.RoleUser,
		ContentParts: []protocol.ContentPart{
			{Type: protocol.ContentPartText, Text: "看图"},
			{Type: protocol.ContentPartImage, SourceURL: url, MimeType: "image/png"},
		},
	}
	sess := &session.Session{ID: "s1", Messages: []protocol.Message{stored}}
	sess.SetCachedContent(url, []byte("cached-bytes"), "image/png", "")

	// 请求副本 = 快照；hydrate 只改副本。
	msgs := sess.SnapshotMessages()
	p := &Plugin{cfg: &config.Config{}}
	filled, unavailable := p.hydrateAttachmentParts(context.Background(), msgs, sess)

	require.Equal(t, 1, filled)
	assert.Zero(t, unavailable)
	part := msgs[0].ContentParts[1]
	assert.Equal(t, []byte("cached-bytes"), part.Data, "bytes must be loaded into the request copy")
	assert.Equal(t, url, part.SourceURL, "reference must be preserved")

	// 会话本身不持有字节：hydrate 只作用于请求副本，不写回历史。
	assert.Empty(t, sess.SnapshotMessages()[0].ContentParts[1].Data,
		"session history must stay byte-free")
}

func TestHydrateAttachmentPartsFromStorage(t *testing.T) {
	l, store := newStoreTestLogger(t, t.TempDir()+"/att")
	insertStoredImage(t, l, store, "g1", "in-1")

	rows, err := l.AttachmentsByEventID("in-1")
	require.NoError(t, err)
	require.Len(t, rows, 1)

	// 模拟"重启后缓存已空，只剩持久化的引用"。
	sess := &session.Session{ID: "s1", ChatID: "g1"}
	msgs := []protocol.Message{{
		Role: protocol.RoleUser,
		ContentParts: []protocol.ContentPart{{
			Type:         protocol.ContentPartImage,
			SourceURL:    rows[0].URL,
			AttachmentID: rows[0].ID,
			MimeType:     rows[0].MimeType,
		}},
	}}

	p := &Plugin{cfg: &config.Config{MaxAttachmentSize: 1 << 20}, history: l}
	filled, unavailable := p.hydrateAttachmentParts(context.Background(), msgs, sess)

	require.Equal(t, 1, filled)
	assert.Zero(t, unavailable)
	assert.Equal(t, "fake-image-bytes-in-1", string(msgs[0].ContentParts[0].Data))
	assert.Equal(t, int64(rows[0].ID), msgs[0].ContentParts[0].AttachmentID)

	// 取回的字节回填会话缓存，同一回合的后续工具轮直接命中。
	cached := sess.CachedContent(rows[0].URL)
	require.NotNil(t, cached)
	assert.Equal(t, "fake-image-bytes-in-1", string(cached.Data))
}

func TestHydrateAttachmentPartsUnavailable(t *testing.T) {
	sess := &session.Session{ID: "s1"}
	msgs := []protocol.Message{{
		Role: protocol.RoleUser,
		ContentParts: []protocol.ContentPart{
			{Type: protocol.ContentPartText, Text: "这张图"},
			{Type: protocol.ContentPartImage}, // 无引用，也无缓存：不可恢复
			{Type: protocol.ContentPartAudio},
		},
	}}

	p := &Plugin{cfg: &config.Config{}}
	filled, unavailable := p.hydrateAttachmentParts(context.Background(), msgs, sess)

	assert.Zero(t, filled)
	require.Equal(t, 2, unavailable)

	parts := msgs[0].ContentParts
	require.Len(t, parts, 3)
	assert.Equal(t, protocol.ContentPartText, parts[1].Type)
	assert.Equal(t, unavailableImagePlaceholder, parts[1].Text)
	assert.Equal(t, unavailableAudioPlaceholder, parts[2].Text)
}
